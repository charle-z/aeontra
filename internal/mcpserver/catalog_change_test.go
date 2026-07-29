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

func TestInitializeAdvertisesToolListChanges(t *testing.T) {
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
	if !got.Result.Capabilities.Tools.ListChanged {
		t.Fatal("initialize must advertise tools.listChanged=true")
	}
}

func TestSSEEmitsCatalogChangeNotificationOnce(t *testing.T) {
	h, _ := newHTTPServer(t, "read-only")

	first := recordSSE(t, h)
	if !strings.Contains(first, `"method":"notifications/tools/list_changed"`) {
		t.Fatalf("first SSE stream lacks tool-list change notification: %q", first)
	}

	second := recordSSE(t, h)
	if strings.Contains(second, `"method":"notifications/tools/list_changed"`) {
		t.Fatalf("catalog notification was duplicated across SSE streams: %q", second)
	}
}

func recordSSE(t *testing.T, h http.Handler) string {
	t.Helper()
	sessionID := initializeHandlerSession(t, h, "Bearer "+testToken)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
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
