package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestPlatformFrontDoorCoordinatorReconcilesExistingWorkerReadiness(t *testing.T) {
	healthPath := managedFrontDoorCoordinatorLegacyPath
	dockerOptions := ""
	applicationPatches := 0
	applicationCreates := 0
	storageCreates := 0
	environmentWrites := 0
	deploys := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/main" || r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"},{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://front.mcp-devbox-charlez.duckdns.org","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","name":"mcp-devbox","git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1":
			_, _ = w.Write([]byte(`{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed","git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door-coordinator","ports_exposes":"8766","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"` + healthPath + `","custom_docker_run_options":"` + dockerOptions + `"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/coord1":
			applicationPatches++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["health_check_path"] != managedFrontDoorCoordinatorHealthPath || payload["health_check_port"] != float64(8766) || payload["custom_docker_run_options"] != managedFrontDoorCoordinatorDockerOptions {
				t.Fatalf("unexpected readiness reconciliation payload: %#v", payload)
			}
			healthPath = managedFrontDoorCoordinatorHealthPath
			dockerOptions = managedFrontDoorCoordinatorDockerOptions
			_, _ = w.Write([]byte(`{"uuid":"coord1"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/api/v1/applications/") && !strings.HasSuffix(r.URL.Path, "/envs") && !strings.HasSuffix(r.URL.Path, "/storages"):
			applicationCreates++
			t.Fatalf("reconciliation attempted to create an application: %s", r.URL.Path)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/storages":
			_, _ = w.Write([]byte(`[{"uuid":"storage1","type":"persistent","name":"mcp-devbox-front-door-coordinator-state","mount_path":"/coordinator-state"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/coord1/storages":
			storageCreates++
			t.Fatal("reconciliation attempted to create a second volume")
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/coord1/envs":
			environmentWrites++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			if r.URL.Query().Get("uuid") != "coord1" || r.URL.Query().Get("force") != "false" {
				t.Fatalf("unsafe deployment query: %s", r.URL.RawQuery)
			}
			deploys++
			_, _ = w.Write([]byte(`{"deployment_uuid":"coord-reconcile-dep","status":"queued"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredCoordinatorService(t, config.ModeAllow, ts.URL)
	preview, err := svc.PlatformFrontDoorCoordinatorPreview(PlatformFrontDoorCoordinatorRequest{
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "action: reconcile") || !strings.Contains(preview, "application_uuid: coord1") {
		t.Fatalf("unexpected reconciliation preview: %s", preview)
	}
	out, err := svc.PlatformFrontDoorCoordinatorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if applicationPatches != 1 || applicationCreates != 0 || storageCreates != 0 || environmentWrites != 12 || deploys != 1 {
		t.Fatalf("patches=%d creates=%d storageCreates=%d env=%d deploys=%d out=%s", applicationPatches, applicationCreates, storageCreates, environmentWrites, deploys, out)
	}
	if healthPath != managedFrontDoorCoordinatorHealthPath || dockerOptions != managedFrontDoorCoordinatorDockerOptions || !strings.Contains(out, "application_uuid: coord1") || !strings.Contains(out, "deployment_id: coord-reconcile-dep") {
		t.Fatalf("reconciliation did not preserve identity or readiness: health=%s out=%s", healthPath, out)
	}
}
