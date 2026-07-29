package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInitializeDoesNotAdvertiseUnsupportedToolListChanges(t *testing.T) {
	s := stampServer(t)
	response := s.handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	var got struct {
		Result struct {
			Capabilities struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.Capabilities.Tools.ListChanged {
		t.Fatal("initialize advertised tools.listChanged without an in-process mutable catalog")
	}
}

func TestSSENeverInventsCatalogChangeOnStartup(t *testing.T) {
	h, _ := newHTTPServer(t, "read-only")

	for stream := 1; stream <= 2; stream++ {
		body := recordSSE(t, h)
		if strings.Contains(body, `"method":"notifications/tools/list_changed"`) {
			t.Fatalf("SSE stream %d fabricated a tool-list change: %q", stream, body)
		}
		if !strings.Contains(body, ": mcp-devbox stream open") {
			t.Fatalf("SSE stream %d did not open normally: %q", stream, body)
		}
	}
}

func recordSSE(t *testing.T, h http.Handler) string {
	t.Helper()
	sessionID := initializeHandlerSession(t, h, "Bearer "+testToken)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, DefaultMCPPath, nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Mcp-Session-Id", sessionID)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(recorder, req)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not stop after cancellation")
	}
	return recorder.Body.String()
}
