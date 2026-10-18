package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

var validationRunnerTestMounts = []string{
	"type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
	"type=bind,source=/var/lib/docker/volumes/repos/_data,target=/repos",
	"type=volume,source=mcp-devbox-pnpm-store,target=/pnpm-store",
}

func TestPlatformValidationRunnerCreateIsPrivatePlannedAndConfiguresEnv(t *testing.T) {
	created := 0
	envs := 0
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/public":
			created++
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"runner1","name":"mcp-devbox-validation-runner-managed"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/applications/runner1/envs"):
			envs++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	svc := configuredPlatformService(t, config.ModeAsk, ts.URL)
	svc.WithCoolify(svc.coolify.WithBuilderRuntime("destination1", validationRunnerTestMounts))
	preview, err := svc.PlatformValidationRunnerCreatePreview("")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || !strings.Contains(preview, "domain: none") || !strings.Contains(preview, "ports_mappings: none") {
		t.Fatalf("unsafe preview created=%d:\n%s", created, preview)
	}
	plan := field(preview, "plan_id")
	out, err := svc.PlatformValidationRunnerCreate(plan, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || created != 0 {
		t.Fatalf("approval gate out=%q err=%v created=%d", out, err, created)
	}
	out, err = svc.PlatformValidationRunnerCreate(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || envs != 7 || !strings.Contains(out, "application_uuid: runner1") || !strings.Contains(out, "deployed: false") {
		t.Fatalf("unexpected result created=%d envs=%d out=%s", created, envs, out)
	}
	for key, want := range map[string]any{
		"git_branch":             "main",
		"destination_uuid":       "destination1",
		"dockerfile_location":    "/Dockerfile.validation-runner",
		"ports_exposes":          "8787",
		"ports_mappings":         "",
		"autogenerate_domain":    false,
		"is_auto_deploy_enabled": false,
		"instant_deploy":         false,
		"health_check_path":      "/healthz",
	} {
		if payload[key] != want {
			t.Fatalf("payload[%s]=%#v want %#v; full=%#v", key, payload[key], want, payload)
		}
	}
	options, _ := payload["custom_docker_run_options"].(string)
	for _, mount := range validationRunnerTestMounts {
		if !strings.Contains(options, "--mount "+mount) {
			t.Fatalf("missing mount %q in %q", mount, options)
		}
	}
}

func TestPlatformValidationRunnerCreateRequiresExactRuntimeConfig(t *testing.T) {
	svc := configuredPlatformService(t, config.ModeAllow, "https://coolify.test")
	svc.WithCoolify(svc.coolify.WithBuilderRuntime("destination1", validationRunnerTestMounts[:2]))
	if _, err := svc.PlatformValidationRunnerCreatePreview(""); err == nil || !strings.Contains(err.Error(), "exactly the three") {
		t.Fatalf("incomplete mount config accepted: %v", err)
	}
}
