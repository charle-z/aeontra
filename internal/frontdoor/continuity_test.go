package frontdoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testRuntimeResponse(request *http.Request) *http.Response {
	body, _ := json.Marshal(map[string]any{
		"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
		"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

func TestFrontDoorNeverRetriesDispatchedPOST(t *testing.T) {
	var posts atomic.Int64
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/readyz":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ready\n")), Request: request}, nil
		case "/version":
			return testRuntimeResponse(request), nil
		case "/mcp":
			posts.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, errors.New("unexpected path")
		}
	})
	door, err := New(Config{
		BackendURL: "http://127.0.0.1:8765", ExpectedProtocol: "2024-11-05",
		ExpectedCatalogHash: testCatalogHash, AdmissionTimeout: time.Second,
		Client: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
	response := httptest.NewRecorder()
	door.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("POST was dispatched %d times", got)
	}
}

func TestFrontDoorAdmissionCancellationDoesNotForward(t *testing.T) {
	var posts atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz", "/version":
			http.Error(w, "offline", http.StatusServiceUnavailable)
		case "/mcp":
			posts.Add(1)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		AdmissionTimeout: time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = door.Probe(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, request)
		done <- response
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case response := <-done:
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled admission did not return")
	}
	if posts.Load() != 0 {
		t.Fatal("cancelled request reached the backend")
	}
}

func TestFrontDoorRejectsNonCommentSSEPayload(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, ": mcp-devbox stream open\n\ndata: forbidden\n\n")
		}
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		AdmissionTimeout: time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://front.example/mcp", nil)
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	door.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "data: forbidden") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if door.sseReconnectFails.Load() != 1 {
		t.Fatalf("reconnect failures=%d", door.sseReconnectFails.Load())
	}
}
