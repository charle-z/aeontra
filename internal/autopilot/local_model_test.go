package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalModelIsLoopbackClosedAndBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/next-action" {
			t.Fatal(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"status","arguments":{"workspace_id":"ws_0123456789abcdef0123456789abcdef"}}`))
	}))
	defer server.Close()
	model := LocalHTTPModel{Endpoint: server.URL + "/v1/next-action"}
	response, err := model.NextAction(context.Background(), LocalAgentRequest{WorkspaceID: "ws_0123456789abcdef0123456789abcdef"})
	if err != nil || response.Action != ActionStatus {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	for _, endpoint := range []string{"https://127.0.0.1:1/v1/next-action", "http://example.com:80/v1/next-action", "http://127.0.0.1:1/other"} {
		if _, err := (LocalHTTPModel{Endpoint: endpoint}).NextAction(context.Background(), LocalAgentRequest{}); err == nil || strings.Contains(err.Error(), "example.com") {
			t.Fatalf("unsafe endpoint error=%v", err)
		}
	}
}
