package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubWorkflowDispatchAcceptsNoContentResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/demo/actions/workflows/ci.yml/dispatches" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Ref    string            `json:"ref"`
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref != "main" || body.Inputs["mode"] != "safe" {
			t.Fatalf("unexpected body: %#v err=%v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	response, err := client.dispatchWorkflow(context.Background(), "demo", "ci.yml", "main", map[string]string{"mode": "safe"})
	if err != nil || response.WorkflowRunID != 0 || response.HTMLURL != "" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
