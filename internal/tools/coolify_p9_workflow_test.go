package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestCoolifySetEnvBrainRootVerifiesStorageBeforeEnvWrite(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	var requests []string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", []string{p9BrainAppUUID}, func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch len(requests) {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/"+p9BrainAppUUID+"/storages" {
				t.Fatalf("first request must list fixed Brain storages: %s %s", r.Method, r.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
				{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
			]`))}, nil
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/"+p9BrainAppUUID+"/envs" {
				t.Fatalf("second request must list envs after storage verification: %s %s", r.Method, r.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		case 3:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/applications/"+p9BrainAppUUID+"/envs" {
				t.Fatalf("third request must create missing env: %s %s", r.Method, r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["key"] != "MCP_DEVBOX_BRAIN_ROOT" || body["value"] != p9BrainMountPath {
				t.Fatalf("unexpected Brain env payload: %#v", body)
			}
			return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"uuid":"env-1"}`))}, nil
		default:
			t.Fatalf("unexpected request %d: %s %s", len(requests), r.Method, r.URL.Path)
			return nil, nil
		}
	}))

	out, err := svc.CoolifySetEnv(p9BrainAppUUID, map[string]string{"MCP_DEVBOX_BRAIN_ROOT": p9BrainMountPath}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests=%v", requests)
	}
	if !strings.Contains(out, "storage verified") || !strings.Contains(out, "MCP_DEVBOX_BRAIN_ROOT -> created") {
		t.Fatalf("unexpected safe output: %q", out)
	}
}

func TestCoolifySetEnvBrainRootRejectsWrongAppOrPathWithoutHTTP(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  string
		path string
	}{
		{name: "wrong-app", app: "otherapp", path: p9BrainMountPath},
		{name: "wrong-path", app: p9BrainAppUUID, path: "/wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTestService(t, config.ModeAllow)
			called := false
			svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(*http.Request) (*http.Response, error) {
				called = true
				return nil, nil
			}))
			if _, err := svc.CoolifySetEnv(tc.app, map[string]string{"MCP_DEVBOX_BRAIN_ROOT": tc.path}, false); err == nil {
				t.Fatal("unsafe Brain env configuration must be rejected")
			}
			if called {
				t.Fatal("unsafe Brain env configuration must make no HTTP request")
			}
		})
	}
}

func TestCoolifySetEnvBrainStorageConflictStopsBeforeEnvWrite(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	var requests int
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", []string{p9BrainAppUUID}, func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/"+p9BrainAppUUID+"/storages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
			{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/wrong"}
		]`))}, nil
	}))

	if _, err := svc.CoolifySetEnv(p9BrainAppUUID, map[string]string{"MCP_DEVBOX_BRAIN_ROOT": p9BrainMountPath}, false); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("storage conflict must stop Brain env write: %v", err)
	}
	if requests != 1 {
		t.Fatalf("conflict must stop after storage list, requests=%d", requests)
	}
}
