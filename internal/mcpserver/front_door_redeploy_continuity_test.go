package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/frontdoor"
)

type continuityRPCTracker struct {
	mu    sync.Mutex
	calls map[string]int
}

func (t *continuityRPCTracker) record(body []byte) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(body, &request) != nil || request.Method != "tools/call" || len(request.ID) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls == nil {
		t.calls = map[string]int{}
	}
	t.calls[string(request.ID)]++
}

func (t *continuityRPCTracker) count(id int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls[fmt.Sprint(id)]
}

type countedBackendInstance struct {
	next    http.Handler
	tracker *continuityRPCTracker
	calls   atomic.Int64
}

func (h *countedBackendInstance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == DefaultMCPPath {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.tracker.record(body)
		h.calls.Add(1)
	}
	h.next.ServeHTTP(w, r)
}

func continuityToolCall(client *http.Client, baseURL, sessionID string, id int) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": "system_runtime_info", "arguments": map[string]any{}},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+DefaultMCPPath, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Session-Id", sessionID)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tool call %d returned HTTP %d: %s", id, response.StatusCode, body)
	}
	if response.Header.Get("Location") != "" {
		return fmt.Errorf("tool call %d redirected", id)
	}
	if response.Header.Get("X-MCP-Front-Door-Commit") == "" {
		return fmt.Errorf("tool call %d omitted front-door identity", id)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || (len(envelope.Error) > 0 && string(envelope.Error) != "null") || envelope.Result.IsError || len(envelope.Result.Content) != 1 {
		return fmt.Errorf("tool call %d returned an invalid MCP result: %s", id, body)
	}
	if !strings.Contains(envelope.Result.Content[0].Text, "catalog_hash") {
		return fmt.Errorf("tool call %d did not execute system_runtime_info", id)
	}
	return nil
}

func openContinuitySSE(t *testing.T, client *http.Client, baseURL, sessionID string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+DefaultMCPPath, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Mcp-Session-Id", sessionID)
	response, err := client.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" || !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		_ = response.Body.Close()
		cancel()
		t.Fatalf("SSE status=%d location=%q content-type=%q", response.StatusCode, response.Header.Get("Location"), response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != ": mcp-devbox stream open\n" {
		_ = response.Body.Close()
		cancel()
		t.Fatalf("SSE open line=%q err=%v", line, err)
	}
	if line, err = reader.ReadString('\n'); err != nil || line != "\n" {
		_ = response.Body.Close()
		cancel()
		t.Fatalf("SSE separator=%q err=%v", line, err)
	}
	closed := make(chan error, 1)
	go func() {
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				closed <- err
				return
			}
		}
	}()
	return func() {
		cancel()
		_ = response.Body.Close()
	}, closed
}

func TestFrontDoorBackendReplacementPreservesSessionSSEAndTools(t *testing.T) {
	previousCommit, previousBuiltAt := buildinfo.Commit, buildinfo.BuiltAt
	buildinfo.Commit = strings.Repeat("d", 40)
	buildinfo.BuiltAt = "2026-07-31T00:00:00Z"
	defer func() {
		buildinfo.Commit = previousCommit
		buildinfo.BuiltAt = previousBuiltAt
	}()

	storeA, storeB := openReplacementSessionStores(t)
	serverA, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverB, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverA.WithHTTPSessionStore(storeA)
	serverB.WithHTTPSessionStore(storeB)
	infoA, infoB := serverA.mustRuntimeInfo(), serverB.mustRuntimeInfo()
	if infoA.ProtocolVersion != infoB.ProtocolVersion || infoA.CatalogHash != infoB.CatalogHash || infoA.Commit != infoB.Commit {
		t.Fatalf("replacement identity differs before activation: A=%+v B=%+v", infoA, infoB)
	}

	lifecycleA, lifecycleB := newHTTPServerLifecycle(), newHTTPServerLifecycle()
	handlerA := serverA.httpHandlerWithRuntime(testToken, nil, HTTPOptions{}, lifecycleA, storeA, newHTTPTransportTelemetry())
	handlerB := serverB.httpHandlerWithRuntime(testToken, nil, HTTPOptions{}, lifecycleB, storeB, newHTTPTransportTelemetry())
	if handlerReadyStatus(t, handlerB) != http.StatusOK {
		t.Fatal("candidate backend was not ready before activation")
	}
	version := httptest.NewRecorder()
	handlerB.ServeHTTP(version, httptest.NewRequest(http.MethodGet, "/version", nil))
	if version.Code != http.StatusOK || !strings.Contains(version.Body.String(), infoB.Commit) || !strings.Contains(version.Body.String(), infoB.ProtocolVersion) || !strings.Contains(version.Body.String(), infoB.CatalogHash) {
		t.Fatalf("candidate identity was incomplete before activation: status=%d body=%s", version.Code, version.Body.String())
	}

	tracker := &continuityRPCTracker{}
	instanceA := &countedBackendInstance{next: handlerA, tracker: tracker}
	instanceB := &countedBackendInstance{next: handlerB, tracker: tracker}
	backendSwitch := newSwitchingHTTPHandler(instanceA)
	backend := httptest.NewServer(backendSwitch)
	defer backend.Close()

	frontCommit := strings.Repeat("f", 40)
	front, err := frontdoor.New(frontdoor.Config{
		BackendURL: backend.URL, ExpectedProtocol: infoA.ProtocolVersion,
		ExpectedCatalogHash: infoA.CatalogHash, FrontDoorCommit: frontCommit,
		ProbeInterval: 250 * time.Millisecond, ProbeTimeout: time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := front.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(front.Handler())
	defer endpoint.Close()
	client := endpoint.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	sessionID := initializeRemote(t, client, endpoint.URL)
	if err := continuityToolCall(client, endpoint.URL, sessionID, 100); err != nil {
		t.Fatal(err)
	}
	closeSSE, sseClosed := openContinuitySSE(t, client, endpoint.URL, sessionID)
	defer closeSSE()

	errorsDuring := make(chan error, 32)
	callsDone := make(chan struct{})
	go func() {
		defer close(callsDone)
		for id := 101; id <= 124; id++ {
			if err := continuityToolCall(client, endpoint.URL, sessionID, id); err != nil {
				errorsDuring <- err
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for instanceA.calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	backendSwitch.Replace(instanceB)
	if err := front.Probe(context.Background()); err != nil {
		t.Fatalf("front door rejected ready replacement: %v", err)
	}
	<-callsDone
	close(errorsDuring)
	for err := range errorsDuring {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	if instanceA.calls.Load() == 0 || instanceB.calls.Load() == 0 {
		t.Fatalf("replacement traffic did not traverse both instances: A=%d B=%d", instanceA.calls.Load(), instanceB.calls.Load())
	}
	select {
	case err := <-sseClosed:
		t.Fatalf("SSE reset during backend replacement: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if got := initializeRemoteWithPrior(t, client, endpoint.URL, sessionID); got != sessionID {
		t.Fatalf("replacement rejected durable session: old=%s new=%s", sessionID, got)
	}
	if err := continuityToolCall(client, endpoint.URL, sessionID, 125); err != nil {
		t.Fatal(err)
	}
	for id := 100; id <= 125; id++ {
		if count := tracker.count(id); count != 1 {
			t.Fatalf("JSON-RPC id %d was forwarded %d times", id, count)
		}
	}

	closeSSE()
	lifecycleA.BeginDrain()
	select {
	case err := <-sseClosed:
		if err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "closed") {
			t.Logf("SSE closed after deliberate retirement: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SSE did not close after deliberate client cancellation")
	}
}
