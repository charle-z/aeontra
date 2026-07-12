package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestCoolifyDeployEncodesExplicitForceMode(t *testing.T) {
	tests := []struct {
		name  string
		force bool
		want  string
	}{
		{name: "normal", force: false, want: "false"},
		{name: "without cache", force: true, want: "true"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotForce, gotUUID string
			client := fakeCoolify(t, "https://coolify.example.com", "token", nil, func(request *http.Request) (*http.Response, error) {
				gotForce = request.URL.Query().Get("force")
				gotUUID = request.URL.Query().Get("uuid")
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"deployment_uuid":"dep1"}`))}, nil
			})

			status, _, err := client.deploy(t.Context(), "app1", test.force)
			if err != nil {
				t.Fatal(err)
			}
			if status != http.StatusOK || gotUUID != "app1" || gotForce != test.want {
				t.Fatalf("status=%d uuid=%q force=%q, want force=%q", status, gotUUID, gotForce, test.want)
			}
		})
	}
}

func TestPlatformDeployWithoutCacheUsesSeparateReviewedPlan(t *testing.T) {
	deploys := 0
	var gotForce string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/deploy" {
			deploys++
			gotForce = request.URL.Query().Get("force")
			_, _ = response.Write([]byte(`{"deployment_uuid":"dep-force","status":"queued"}`))
			return
		}
		_, _ = response.Write([]byte(`{"uuid":"app1","name":"demo","status":"running","git_repository":"acme/demo","git_branch":"main","git_commit_sha":"abc123"}`))
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAsk, server.URL)
	preview, err := service.PlatformDeployWithoutCachePreview("app1")
	if err != nil {
		t.Fatal(err)
	}
	if deploys != 0 || !strings.Contains(preview, "without reusable build cache") || !strings.Contains(preview, "force: true") {
		t.Fatalf("unsafe or unclear preview deploys=%d:\n%s", deploys, preview)
	}
	planID := field(preview, "plan_id")

	approval, err := service.PlatformDeployWithoutCache(planID, false)
	if err != nil {
		t.Fatal(err)
	}
	if deploys != 0 || !strings.Contains(approval, "APPROVAL REQUIRED") {
		t.Fatalf("approval gate failed deploys=%d out=%q", deploys, approval)
	}

	result, err := service.PlatformDeployWithoutCache(planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if deploys != 1 || gotForce != "true" || !strings.Contains(result, "deployment_id: dep-force") {
		t.Fatalf("force deployment failed deploys=%d force=%q result=%q", deploys, gotForce, result)
	}

	if _, err := service.PlatformDeploy(planID, true); err == nil || !strings.Contains(err.Error(), "operation mismatch") {
		t.Fatalf("force plan must not execute through normal deploy: %v", err)
	}
}

func TestPlatformDeployWithoutCacheRevalidatesApplicationState(t *testing.T) {
	branch := "main"
	deploys := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/deploy" {
			deploys++
			_, _ = response.Write([]byte(`{"deployment_uuid":"dep-force"}`))
			return
		}
		_, _ = response.Write([]byte(`{"uuid":"app1","name":"demo","git_repository":"acme/demo","git_branch":"` + branch + `","git_commit_sha":"abc123"}`))
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	preview, err := service.PlatformDeployWithoutCachePreview("app1")
	if err != nil {
		t.Fatal(err)
	}
	branch = "changed"
	if _, err := service.PlatformDeployWithoutCache(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "application changed") {
		t.Fatalf("changed application must reject no-cache deploy: %v", err)
	}
	if deploys != 0 {
		t.Fatalf("deployment ran after state change: %d", deploys)
	}
}
