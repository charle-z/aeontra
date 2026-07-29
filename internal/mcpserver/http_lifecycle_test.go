package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestHTTPReadinessDropsBeforeDrainWhileLivenessRemains(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	lifecycle := newHTTPServerLifecycle()
	handler := server.httpHandlerWithLifecycle(testToken, nil, HTTPOptions{}, lifecycle)

	if response := do(t, handler, http.MethodGet, "/readyz", "", ""); response.Code != http.StatusOK {
		t.Fatalf("ready before drain=%d body=%s", response.Code, response.Body.String())
	}
	if response := do(t, handler, http.MethodGet, "/healthz", "", ""); response.Code != http.StatusOK {
		t.Fatalf("liveness before drain=%d body=%s", response.Code, response.Body.String())
	}

	lifecycle.BeginDrain()

	if lifecycle.Ready() || !lifecycle.Draining() {
		t.Fatalf("lifecycle after BeginDrain: ready=%v draining=%v", lifecycle.Ready(), lifecycle.Draining())
	}
	if response := do(t, handler, http.MethodGet, "/readyz", "", ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready during drain=%d body=%s", response.Code, response.Body.String())
	}
	if response := do(t, handler, http.MethodGet, "/healthz", "", ""); response.Code != http.StatusOK {
		t.Fatalf("liveness during drain=%d body=%s", response.Code, response.Body.String())
	}

	initialize := do(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, rpcBody(t, 1, "initialize", nil))
	if initialize.Code != http.StatusServiceUnavailable {
		t.Fatalf("initialize during drain=%d body=%s", initialize.Code, initialize.Body.String())
	}
	if initialize.Header().Get("Retry-After") == "" || initialize.Header().Get("Mcp-Session-Id") != "" {
		t.Fatalf("unsafe initialize drain headers=%v", initialize.Header())
	}

	if response := do(t, handler, http.MethodPost, DefaultMCPPath, "", rpcBody(t, 2, "tools/list", nil)); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /mcp during drain=%d", response.Code)
	}
	if response := do(t, handler, http.MethodGet, "/console/status", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /console/status during drain=%d", response.Code)
	}
}

func TestHTTPSSEClosesWhenDrainBegins(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	lifecycle := newHTTPServerLifecycle()
	handler := server.httpHandlerWithLifecycle(testToken, nil, HTTPOptions{}, lifecycle)
	request := httptest.NewRequest(http.MethodGet, DefaultMCPPath, nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()
	lifecycle.BeginDrain()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE stream did not close after drain began")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("SSE status=%d body=%s", response.Code, response.Body.String())
	}
}

func addBlockingDrainTool(server *Server, name string, started chan<- struct{}, release <-chan struct{}) {
	var once sync.Once
	server.table[name] = toolEntry{
		def: toolDef{
			Name:        name,
			Description: "Test-only bounded drain probe.",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
			Version:     "1",
		},
		handler: func(json.RawMessage) (string, error) {
			once.Do(func() { close(started) })
			<-release
			return "completed", nil
		},
	}
	server.order = append(server.order, name)
}

type lifecycleServer struct {
	baseURL   string
	cancel    context.CancelFunc
	done      <-chan error
	client    *http.Client
	lifecycle *httpServerLifecycle
}

func startLifecycleServer(t *testing.T, server *Server, shutdownTimeout time.Duration, invalidate func()) lifecycleServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := newHTTPServerLifecycle()
	httpServer := &http.Server{
		Handler:           server.httpHandlerWithLifecycle(testToken, nil, HTTPOptions{}, lifecycle),
		ReadHeaderTimeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveHTTPListener(ctx, httpServer, listener, lifecycle, shutdownTimeout, invalidate)
	}()
	return lifecycleServer{
		baseURL:   "http://" + listener.Addr().String(),
		cancel:    cancel,
		done:      done,
		client:    &http.Client{Timeout: 2 * time.Second},
		lifecycle: lifecycle,
	}
}

type asyncHTTPResult struct {
	status int
	err    error
}

func callToolAsync(client *http.Client, baseURL, sessionID, tool string) <-chan asyncHTTPResult {
	result := make(chan asyncHTTPResult, 1)
	go func() {
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      tool,
				"arguments": map[string]any{},
			},
		})
		if err != nil {
			result <- asyncHTTPResult{err: err}
			return
		}
		request, err := http.NewRequest(http.MethodPost, baseURL+DefaultMCPPath, strings.NewReader(string(body)))
		if err != nil {
			result <- asyncHTTPResult{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Content-Type", "application/json")
		if sessionID != "" {
			request.Header.Set("Mcp-Session-Id", sessionID)
		}
		response, err := client.Do(request)
		if err != nil {
			result <- asyncHTTPResult{err: err}
			return
		}
		defer response.Body.Close()
		result <- asyncHTTPResult{status: response.StatusCode}
	}()
	return result
}

func waitForDrain(t *testing.T, lifecycle *httpServerLifecycle) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !lifecycle.Draining() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !lifecycle.Draining() || lifecycle.Ready() {
		t.Fatalf("lifecycle did not enter drain: ready=%v draining=%v", lifecycle.Ready(), lifecycle.Draining())
	}
}

func TestHTTPDrainAllowsActiveRequestToCompleteAndInvalidatesSessionMetadata(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	started := make(chan struct{})
	release := make(chan struct{})
	const toolName = "drain_completion_probe"
	addBlockingDrainTool(server, toolName, started, release)
	var invalidated atomic.Bool
	running := startLifecycleServer(t, server, time.Second, func() {
		invalidated.Store(true)
		server.clients.Reset()
	})

	sessionID := initializeRemote(t, running.client, running.baseURL)
	if got := server.clients.Snapshot(sessionID); got.ObservedAt == "" {
		t.Fatal("initialize did not record process-local session metadata")
	}
	requestDone := callToolAsync(running.client, running.baseURL, sessionID, toolName)
	<-started

	running.cancel()
	waitForDrain(t, running.lifecycle)
	if invalidated.Load() {
		t.Fatal("session metadata was invalidated before the active request completed")
	}
	if got := server.clients.Snapshot(sessionID); got.ObservedAt == "" {
		t.Fatal("active request lost its process-local session metadata during drain")
	}
	close(release)

	if result := <-requestDone; result.err != nil || result.status != http.StatusOK {
		t.Fatalf("active request result=%+v", result)
	}
	if err := <-running.done; err != nil {
		t.Fatalf("bounded graceful drain failed: %v", err)
	}
	if !invalidated.Load() {
		t.Fatal("session metadata invalidation did not run after active work completed")
	}
	if got := server.clients.Snapshot(sessionID); got.ObservedAt != "" {
		t.Fatalf("session metadata survived completed drain: %+v", got)
	}
}

func TestHTTPDrainForcesTerminationAtDeadline(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	started := make(chan struct{})
	release := make(chan struct{})
	const toolName = "drain_deadline_probe"
	addBlockingDrainTool(server, toolName, started, release)
	var invalidated atomic.Bool
	running := startLifecycleServer(t, server, 40*time.Millisecond, func() { invalidated.Store(true) })
	requestDone := callToolAsync(running.client, running.baseURL, "", toolName)
	<-started

	began := time.Now()
	running.cancel()
	err := <-running.done
	elapsed := time.Since(began)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline drain error=%v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("deadline drain took %s", elapsed)
	}
	waitForDrain(t, running.lifecycle)
	if !invalidated.Load() {
		t.Fatal("invalidation did not run before forced close")
	}

	close(release)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("forced-close request goroutine did not exit")
	}
}
