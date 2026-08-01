package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func configuredCoordinatorService(t *testing.T, mode config.Mode, baseURL string) *Service {
	t.Helper()
	svc := configuredPlatformService(t, mode, baseURL)
	svc.WithGitHub(NewGitHubClient(baseURL, "github-token", "acme", "org", "private"))
	svc.WithCoolify(NewCoolifyClient(baseURL, "coolify-token", nil).
		WithBuilderConfig("server1", "project1", "production", "", []string{"mcp-devbox-charlez.duckdns.org"}).
		WithBuilderRuntime("destination1", nil))
	return svc
}

func coordinatorRuntimeEnvironmentEntry(key, value string) map[string]any {
	return map[string]any{
		"key": key, "comment": frontdoorcoordinator.ManagedEnvironmentComment("coolify-token", key, value),
		"is_preview": false, "is_literal": true, "is_runtime": true, "is_buildtime": false,
	}
}

func coordinatorRuntimeEnvironment(baseURL, target, requestID string) string {
	entries := []map[string]any{
		coordinatorRuntimeEnvironmentEntry("COOLIFY_URL", baseURL),
		coordinatorRuntimeEnvironmentEntry("COOLIFY_API_TOKEN", "coolify-token"),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_COORDINATOR_APP_UUID", "coord1"),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_APP_UUID", "front1"),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_BACKEND_APP_UUID", managedBackendAppUUID),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_EXPECTED_COMMIT", frontDoorTestSHA),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT", frontDoorTestSHA),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_EXPECTED_PROTOCOL", "2024-11-05"),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", frontDoorTestCatalog),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_COORDINATOR_TARGET", target),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT", "/coordinator-state"),
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_COORDINATOR_ADDR", "0.0.0.0:8766"),
	}
	if requestID != "" {
		entries = append(entries, coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID", requestID))
	}
	encoded, _ := json.Marshal(entries)
	return string(encoded)
}

func TestPlatformFrontDoorTransitionDispatchesOnlyPrivateCoordinator(t *testing.T) {
	const coordinatorID = "coord1"
	environmentWrites := 0
	coordinatorDeploys := 0
	domainWrites := 0
	coordinatorHealthPath := managedFrontDoorCoordinatorHealthPath

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/main" || r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[
				{"uuid":"front1","name":"mcp-devbox-front-door-managed"},
				{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}
			]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://front.mcp-devbox-charlez.duckdns.org","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz","custom_docker_run_options":""}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1":
			_, _ = w.Write([]byte(`{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door-coordinator","ports_exposes":"8766","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"` + coordinatorHealthPath + `","custom_docker_run_options":""}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","name":"mcp-devbox","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/storages":
			_, _ = w.Write([]byte(`[{"uuid":"storage1","type":"persistent","name":"mcp-devbox-front-door-coordinator-state","mount_path":"/coordinator-state"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(frontBackendEnvironment(frontdoorcoordinator.FrontPublicOrigin)))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(coordinatorRuntimeEnvironment(ts.URL, "idle", "")))
		case (r.Method == http.MethodPost || r.Method == http.MethodPatch) && r.URL.Path == "/api/v1/applications/coord1/envs":
			environmentWrites++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["key"] != "MCP_FRONT_DOOR_COORDINATOR_TARGET" && payload["key"] != "MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID" {
				t.Fatalf("unexpected coordinator env payload: %#v", payload)
			}
			key, _ := payload["key"].(string)
			value, _ := payload["value"].(string)
			comment, _ := payload["comment"].(string)
			if _, err := frontdoorcoordinator.ManagedEnvironmentValue(comment, "coolify-token", key, value); err != nil {
				t.Fatalf("unauthenticated coordinator env payload: %v payload=%#v", err, payload)
			}
			for field, want := range map[string]bool{
				"is_preview": false, "is_literal": true, "is_runtime": true, "is_buildtime": false,
			} {
				if got, ok := payload[field].(bool); !ok || got != want {
					t.Fatalf("payload %s=%#v want=%t", field, payload[field], want)
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			if r.URL.Query().Get("uuid") != coordinatorID || r.URL.Query().Get("force") != "false" {
				t.Fatalf("unsafe deployment query: %s", r.URL.RawQuery)
			}
			coordinatorDeploys++
			_, _ = w.Write([]byte(`{"deployment_uuid":"coord-dep1","status":"queued"}`))
		case r.Method == http.MethodPatch && (r.URL.Path == "/api/v1/applications/front1" || r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID):
			domainWrites++
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredCoordinatorService(t, config.ModeAllow, ts.URL)
	coordinatorHealthPath = managedFrontDoorCoordinatorLegacyPath
	if blocked, err := svc.PlatformFrontDoorTransitionPreview("cutover"); err == nil || strings.Contains(blocked, "plan_id") {
		t.Fatalf("non-ready coordinator produced transition preview: preview=%q err=%v", blocked, err)
	}
	coordinatorHealthPath = managedFrontDoorCoordinatorHealthPath
	preview, err := svc.PlatformFrontDoorTransitionPreview("cutover")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"action: dispatch", "target: cutover", "phase: switch-front-backend", "coordinator_application_uuid: coord1"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	out, err := svc.PlatformFrontDoorTransition(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if environmentWrites != 2 || coordinatorDeploys != 1 || domainWrites != 0 {
		t.Fatalf("env=%d coordinatorDeploys=%d domainWrites=%d out=%s", environmentWrites, coordinatorDeploys, domainWrites, out)
	}
	if !strings.Contains(out, "deployment_id: coord-dep1") || !strings.Contains(out, "target: cutover") {
		t.Fatalf("unexpected dispatch output: %s", out)
	}
}

func TestPlatformFrontDoorTransitionPreviewIsNoopAtTargetAndRejectsConflictingJournal(t *testing.T) {
	const requestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coordinatorTarget := "rollback"
	statusDescription := `mcp-front-door-coordinator:v1 {"schema_version":1,"revision":7,"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","target":"rollback","state":"running","phase":"move-front-temporary","topology":{"front_domain":"https://mcp-devbox-charlez.duckdns.org","front_backend_url":"https://backend.mcp-devbox-charlez.duckdns.org","backend_domains":"https://backend.mcp-devbox-charlez.duckdns.org"},"updated_at":"2026-07-31T13:00:00Z"}`
	frontDomain := "https://mcp-devbox-charlez.duckdns.org"
	frontBackend := "https://backend.mcp-devbox-charlez.duckdns.org"
	backendDomains := "https://backend.mcp-devbox-charlez.duckdns.org"
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"},{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}]`))
		case "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"` + frontDomain + `"}`))
		case "/api/v1/applications/coord1":
			_, _ = w.Write([]byte(`{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed","status":"running:healthy","deployment_status":"finished","description":` + mustJSON(statusDescription) + `,"git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door-coordinator","ports_exposes":"8766","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/readyz","custom_docker_run_options":""}`))
		case "/api/v1/applications/" + managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"main","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"` + backendDomains + `"}`))
		case "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(frontBackendEnvironment(frontBackend)))
		case "/api/v1/applications/coord1/storages":
			_, _ = w.Write([]byte(`[{"type":"persistent","name":"mcp-devbox-front-door-coordinator-state","mount_path":"/coordinator-state"}]`))
		case "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(coordinatorRuntimeEnvironment(ts.URL, coordinatorTarget, requestID)))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()
	svc := configuredCoordinatorService(t, config.ModeReadOnly, ts.URL)

	preview, err := svc.PlatformFrontDoorTransitionPreview("cutover")
	if err != nil || !strings.Contains(preview, "action: noop") {
		t.Fatalf("closed target preview=%s err=%v", preview, err)
	}
	frontDomain = "https://front.mcp-devbox-charlez.duckdns.org"
	backendDomains = "https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"
	if _, err := svc.PlatformFrontDoorTransitionPreview("cutover"); err == nil || !strings.Contains(err.Error(), "different front-door transition") {
		t.Fatalf("conflicting active transition accepted: %v", err)
	}
	coordinatorTarget = "cutover"
	statusDescription = `mcp-front-door-coordinator:v1 {"schema_version":1,"revision":8,"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","target":"cutover","recovery_target":"rollback","state":"compensating","phase":"restore-public-backend","topology":{"front_domain":"https://front.mcp-devbox-charlez.duckdns.org","front_backend_url":"https://backend.mcp-devbox-charlez.duckdns.org","backend_domains":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"},"reason":"assign-public-front_failed","updated_at":"2026-07-31T13:01:00Z"}`
	preview, err = svc.PlatformFrontDoorTransitionPreview("cutover")
	if err != nil || !strings.Contains(preview, "action: observe") || !strings.Contains(preview, "disposition: observe") {
		t.Fatalf("active compensation preview=%s err=%v", preview, err)
	}
}

func mustJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestPlatformFrontDoorTransitionStatusOmitsDurableRequestID(t *testing.T) {
	const requestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	statusDescription := `mcp-front-door-coordinator:v1 {"schema_version":1,"revision":9,"request_id":"` + requestID + `","target":"cutover","state":"running","phase":"release-public-backend","topology":{"front_domain":"https://front.mcp-devbox-charlez.duckdns.org","front_backend_url":"https://backend.mcp-devbox-charlez.duckdns.org","backend_domains":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"},"updated_at":"2026-07-31T15:00:00Z"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"},{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}]`))
		case "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","deployment_status":"finished","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","fqdn":"https://front.mcp-devbox-charlez.duckdns.org","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz","custom_docker_run_options":""}`))
		case "/api/v1/applications/coord1":
			_, _ = w.Write([]byte(`{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed","status":"running:healthy","deployment_status":"finished","description":` + mustJSON(statusDescription) + `,"git_repository":"acme/mcp-devbox","git_branch":"main","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door-coordinator","ports_exposes":"8766","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/readyz","custom_docker_run_options":""}`))
		case "/api/v1/applications/" + managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","git_repository":"acme/mcp-devbox","git_branch":"main","fqdn":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"}`))
		case "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(frontBackendEnvironment(frontdoorcoordinator.BackendOrigin)))
		case "/api/v1/applications/coord1/storages":
			_, _ = w.Write([]byte(`[{"type":"persistent","name":"mcp-devbox-front-door-coordinator-state","mount_path":"/coordinator-state"}]`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredCoordinatorService(t, config.ModeReadOnly, ts.URL)
	out, err := svc.PlatformFrontDoorTransitionStatus()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, requestID) || strings.Contains(out, "request_id") {
		t.Fatalf("durable request id leaked: %s", out)
	}
	for _, want := range []string{"recovery_target: ", "state: running", "phase: release-public-backend", "revision: 9", "front_backend: https://backend.mcp-devbox-charlez.duckdns.org"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q: %s", want, out)
		}
	}
}

func TestDecodePublishedStatusRejectsInvalidDurableState(t *testing.T) {
	invalid := `mcp-front-door-coordinator:v1 {"schema_version":1,"revision":4,"request_id":"bad","target":"cutover","state":"running","phase":"add-backend-origin"}`
	if _, present, err := frontdoorcoordinator.DecodePublishedStatus(invalid); !present || err == nil {
		t.Fatalf("invalid published status present=%t err=%v", present, err)
	}
}

func frontBackendEnvironment(value string) string {
	encoded, _ := json.Marshal([]map[string]any{
		coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_BACKEND_URL", value),
	})
	return string(encoded)
}
