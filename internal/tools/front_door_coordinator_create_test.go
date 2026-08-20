package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestPlatformFrontDoorCoordinatorCreatesOnePrivateWorker(t *testing.T) {
	appExists := false
	storageExists := false
	created := 0
	storageCreates := 0
	environmentWrites := 0
	deploys := 0
	var createPayload map[string]any
	var storagePayload map[string]any
	seenKeys := map[string]bool{}
	seenValues := map[string]string{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/repos/acme/aeontra/git/ref/heads/main" || r.URL.Path == "/repos/acme/aeontra/git/ref/heads/front-door-stable"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			body := `[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}`
			if appExists {
				body += `,{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}`
			}
			_, _ = w.Write([]byte(body + `]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://front.mcp-devbox-charlez.duckdns.org","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","name":"mcp-devbox","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"main","fqdn":"https://mcp-devbox-charlez.duckdns.org,https://backend.mcp-devbox-charlez.duckdns.org"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/private-github-app":
			created++
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Fatal(err)
			}
			appExists = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"coord1","name":"mcp-devbox-front-door-coordinator-managed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/storages":
			if storageExists {
				_, _ = w.Write([]byte(`[{"uuid":"storage1","type":"persistent","name":"mcp-devbox-front-door-coordinator-state","mount_path":"/coordinator-state"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/coord1/storages":
			storageCreates++
			if err := json.NewDecoder(r.Body).Decode(&storagePayload); err != nil {
				t.Fatal(err)
			}
			storageExists = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"storage1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/coord1/envs":
			environmentWrites++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			key, _ := payload["key"].(string)
			value, _ := payload["value"].(string)
			seenKeys[key] = true
			seenValues[key] = value
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			if r.URL.Query().Get("uuid") != "coord1" || r.URL.Query().Get("force") != "false" {
				t.Fatalf("unsafe coordinator deployment: %s", r.URL.RawQuery)
			}
			deploys++
			_, _ = w.Write([]byte(`{"deployment_uuid":"coord-dep","status":"queued"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredCoordinatorService(t, config.ModeAsk, ts.URL)
	svc.WithGitHub(NewGitHubClient(ts.URL, "github-token", "acme", "org", "private"))
	svc.WithCoolify(svc.coolify.WithGitHubApp("githubapp1"))
	preview, err := svc.PlatformFrontDoorCoordinatorPreview(PlatformFrontDoorCoordinatorRequest{
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || !strings.Contains(preview, "action: create") || !strings.Contains(preview, "domain: none") || strings.Contains(preview, "coolify-token") {
		t.Fatalf("unsafe preview created=%d:\n%s", created, preview)
	}
	planID := field(preview, "plan_id")
	out, err := svc.PlatformFrontDoorCoordinatorCreate(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || created != 0 {
		t.Fatalf("approval gate out=%q err=%v created=%d", out, err, created)
	}
	out, err = svc.PlatformFrontDoorCoordinatorCreate(planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || storageCreates != 1 || environmentWrites != 12 || deploys != 1 {
		t.Fatalf("created=%d storage=%d env=%d deploys=%d out=%s", created, storageCreates, environmentWrites, deploys, out)
	}
	if createPayload["autogenerate_domain"] != false || createPayload["git_repository"] != "https://github.com/acme/aeontra.git" || createPayload["custom_docker_run_options"] != managedFrontDoorCoordinatorDockerOptions || createPayload["dockerfile_location"] != "/Dockerfile.front-door-coordinator" || createPayload["ports_exposes"] != "8766" {
		t.Fatalf("unsafe coordinator create payload: %#v", createPayload)
	}
	for _, forbidden := range []string{"fqdn", "domains", "ports_mappings"} {
		if forbidden != "ports_mappings" {
			if _, ok := createPayload[forbidden]; ok {
				t.Fatalf("coordinator payload contains %s: %#v", forbidden, createPayload)
			}
		}
	}
	if storagePayload["type"] != "persistent" || storagePayload["name"] != "mcp-devbox-front-door-coordinator-state" || storagePayload["mount_path"] != "/coordinator-state" {
		t.Fatalf("unexpected storage payload: %#v", storagePayload)
	}
	for _, key := range []string{"COOLIFY_URL", "COOLIFY_API_TOKEN", "MCP_FRONT_DOOR_COORDINATOR_APP_UUID", "MCP_FRONT_DOOR_APP_UUID", "MCP_FRONT_DOOR_BACKEND_APP_UUID", "MCP_FRONT_DOOR_EXPECTED_COMMIT", "MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT", "MCP_FRONT_DOOR_EXPECTED_PROTOCOL", "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", "MCP_FRONT_DOOR_COORDINATOR_TARGET", "MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT", "MCP_FRONT_DOOR_COORDINATOR_ADDR"} {
		if !seenKeys[key] {
			t.Fatalf("missing fixed coordinator environment key %s", key)
		}
	}
	if seenValues["COOLIFY_URL"] != coordinatorRuntimeCoolifyURL(ts.URL) {
		t.Fatalf("coordinator Coolify URL was not rewritten to the private gateway")
	}
	if strings.Contains(out, "coolify-token") || !strings.Contains(out, "domain: none") || !strings.Contains(out, "deployment_id: coord-dep") {
		t.Fatalf("unsafe coordinator output: %s", out)
	}
}
