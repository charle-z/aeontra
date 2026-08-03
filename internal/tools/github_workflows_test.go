package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubWorkflowDispatchIsOwnerBoundPlannedApprovedAndRevalidated(t *testing.T) {
	refSHA := strings.Repeat("a", 40)
	dispatches := 0
	workflowReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + refSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/actions/workflows/edge-release.yml":
			workflowReads++
			_, _ = w.Write([]byte(`{"id":42,"name":"Signed Edge release","path":".github/workflows/edge-release.yml","state":"active","html_url":"https://github.com/acme/mcp-devbox/actions/workflows/edge-release.yml"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/mcp-devbox/actions/workflows/edge-release.yml/dispatches":
			var body struct {
				Ref    string            `json:"ref"`
				Inputs map[string]string `json:"inputs"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref != "main" || body.Inputs["release"] != "p15.0.24" || len(body.Inputs) != 1 {
				t.Fatalf("unexpected dispatch body: %#v err=%v", body, err)
			}
			dispatches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"workflow_run_id":99,"run_url":"https://api.github.com/repos/acme/mcp-devbox/actions/runs/99","html_url":"https://github.com/acme/mcp-devbox/actions/runs/99"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourceWorkflowDispatchPreview("mcp-devbox", "edge-release.yml", "main", map[string]string{"release": "p15.0.24"})
	if err != nil || dispatches != 0 || workflowReads != 1 || !strings.Contains(preview, refSHA) || !strings.Contains(preview, "release=p15.0.24") {
		t.Fatalf("preview=%q err=%v dispatches=%d reads=%d", preview, err, dispatches, workflowReads)
	}
	planID := field(preview, "plan_id")
	approval, err := svc.SourceWorkflowDispatch(planID, false)
	if err != nil || !strings.Contains(approval, "APPROVAL REQUIRED") || dispatches != 0 {
		t.Fatalf("approval=%q err=%v dispatches=%d", approval, err, dispatches)
	}
	result, err := svc.SourceWorkflowDispatch(planID, true)
	if err != nil || dispatches != 1 || workflowReads != 2 || !strings.Contains(result, "workflow_run_id: 99") || !strings.Contains(result, "dispatched: true") {
		t.Fatalf("result=%q err=%v dispatches=%d reads=%d", result, err, dispatches, workflowReads)
	}
	if strings.Contains(result, "token") || strings.Contains(preview, "token") {
		t.Fatalf("workflow output exposed credential material: preview=%q result=%q", preview, result)
	}
}

func TestGitHubWorkflowDispatchRejectsChangedRef(t *testing.T) {
	refSHA := strings.Repeat("a", 40)
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/main"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + refSHA + `"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/actions/workflows/edge-release.yml"):
			_, _ = w.Write([]byte(`{"id":42,"name":"Signed Edge release","path":".github/workflows/edge-release.yml","state":"active"}`))
		case r.Method == http.MethodPost:
			dispatched = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourceWorkflowDispatchPreview("mcp-devbox", "edge-release.yml", "main", map[string]string{"release": "p15.0.24"})
	if err != nil {
		t.Fatal(err)
	}
	refSHA = strings.Repeat("b", 40)
	if _, err := svc.SourceWorkflowDispatch(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "ref changed") || dispatched {
		t.Fatalf("expected changed-ref failure, got err=%v dispatched=%t", err, dispatched)
	}
}

func TestGitHubWorkflowDispatchRejectsUnsafeInputs(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient("https://api.github.invalid", "token", "acme", "org", "private"))

	tooMany := map[string]string{}
	for i := 0; i < maxGitHubWorkflowInputs+1; i++ {
		tooMany[string(rune('a'+i))] = "safe"
	}
	cases := []struct {
		name     string
		workflow string
		ref      string
		inputs   map[string]string
	}{
		{name: "workflow path", workflow: "../edge-release.yml", ref: "main"},
		{name: "unsafe ref", workflow: "edge-release.yml", ref: "main..old"},
		{name: "too many inputs", workflow: "edge-release.yml", ref: "main", inputs: tooMany},
		{name: "whitespace normalization", workflow: "edge-release.yml", ref: "main", inputs: map[string]string{"release": " p15.0.24"}},
		{name: "secret-like input", workflow: "edge-release.yml", ref: "main", inputs: map[string]string{"release": "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890123456"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := svc.SourceWorkflowDispatchPreview("mcp-devbox", test.workflow, test.ref, test.inputs); err == nil {
				t.Fatal("unsafe workflow dispatch input was accepted")
			}
		})
	}
}
