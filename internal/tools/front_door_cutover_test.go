package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func configuredManagedCutoverService(t *testing.T, baseURL string) *Service {
	t.Helper()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithMaintainerProfile(MaintainerProfileCharleZProduction)
	svc.WithGitHub(NewGitHubClient(baseURL, "github-token", "acme", "org", "private"))
	svc.WithCoolify(NewCoolifyClient(baseURL, "coolify-token", nil).
		WithBuilderConfig("server1", "project1", "production", "", []string{
			"mcp-devbox-charlez.duckdns.org",
			"144-225-147-58.sslip.io",
		}).
		WithBuilderRuntime("destination1", nil))
	svc.PlatformCapability.managedFrontDoorProbe = func(context.Context, string, bool, string, string, string) error { return nil }
	svc.PlatformCapability.managedFrontDoorSleepFn = func(time.Duration) {}
	svc.PlatformCapability.managedFrontDoorExternalCoordinator = true
	return svc
}

func TestPlatformFrontDoorManagedCutoverSequenceIsReversible(t *testing.T) {
	frontDomain := managedFrontDoorLegacyOrigin
	backendDomain := managedFrontDoorPublicOrigin
	var domainUpdates []string
	environmentUpdates := 0
	deployments := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"` + frontDomain + `","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","name":"mcp-devbox","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"main","fqdn":"` + backendDomain + `"}`))
		case r.Method == http.MethodPatch && (r.URL.Path == "/api/v1/applications/front1" || r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			domains, _ := payload["domains"].(string)
			domainUpdates = append(domainUpdates, r.URL.Path+"="+domains)
			if r.URL.Path == "/api/v1/applications/front1" {
				frontDomain = domains
			} else {
				backendDomain = domains
			}
			_, _ = w.Write([]byte(`{"uuid":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`[{"uuid":"env1","key":"MCP_FRONT_DOOR_BACKEND_URL"},{"uuid":"env2","key":"MCP_FRONT_DOOR_EXPECTED_PROTOCOL"},{"uuid":"env3","key":"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1/envs":
			environmentUpdates++
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deployments++
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep1","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deployments/dep1":
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep1","status":"finished"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	probe := func(_ context.Context, origin string, _ bool, _ string, _ string, _ string) error {
		switch origin {
		case managedFrontDoorBackendOrigin:
			if deployments < 1 {
				return errors.New("backend origin was probed before routing deployment")
			}
		case managedFrontDoorPublicOrigin:
			if deployments < 4 {
				return errors.New("public front door was probed before backend release and routing deployment")
			}
		}
		return nil
	}
	newService := func() *Service {
		svc := configuredManagedCutoverService(t, ts.URL)
		svc.PlatformCapability.managedFrontDoorProbe = probe
		return svc
	}
	svc := newService()
	request := PlatformFrontDoorRequest{
		Domain: managedFrontDoorPublicOrigin, BackendURL: managedFrontDoorBackendOrigin,
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	}
	preview, err := svc.PlatformFrontDoorCreatePreview(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "action: cutover") || !strings.Contains(preview, "return its deployment id") {
		t.Fatalf("cutover preview was incomplete:\n%s", preview)
	}
	phaseOne, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(phaseOne, "action: cutover") || !strings.Contains(phaseOne, "backend_origin_deployment_id: dep1") || !strings.Contains(phaseOne, "next_action: "+frontDoorActionResumeCutoverBackend) {
		t.Fatalf("first cutover phase was incomplete: %s", phaseOne)
	}
	svc = newService()

	preview, err = svc.PlatformFrontDoorCreatePreview(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "action: "+frontDoorActionResumeCutoverBackend) {
		t.Fatalf("backend-ready resume preview was incomplete:\n%s", preview)
	}
	phaseTwo, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(phaseTwo, "front_door_backend_deployment_id: dep1") || !strings.Contains(phaseTwo, "backend_release_deployment_id: dep1") || !strings.Contains(phaseTwo, "next_action: "+frontDoorActionResumeCutoverPublic) {
		t.Fatalf("second cutover phase was incomplete: %s", phaseTwo)
	}
	svc = newService()

	preview, err = svc.PlatformFrontDoorCreatePreview(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "action: "+frontDoorActionResumeCutoverPublic) {
		t.Fatalf("public-ready resume preview was incomplete:\n%s", preview)
	}
	out, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/v1/applications/" + managedBackendAppUUID + "=" + managedFrontDoorPublicOrigin + "," + managedFrontDoorBackendOrigin,
		"/api/v1/applications/" + managedBackendAppUUID + "=" + managedFrontDoorBackendOrigin,
		"/api/v1/applications/front1=" + managedFrontDoorPublicOrigin,
	}
	if !reflect.DeepEqual(domainUpdates, want) {
		t.Fatalf("domain sequence=%v want=%v", domainUpdates, want)
	}
	if environmentUpdates != 3 || deployments != 4 || !strings.Contains(out, "action: "+frontDoorActionResumeCutoverPublic) || !strings.Contains(out, "rollback_request_domain: "+managedFrontDoorTemporaryOrigin) || !strings.Contains(out, "public_domain_deployment_id: dep1") {
		t.Fatalf("env=%d deployments=%d out=%s", environmentUpdates, deployments, out)
	}
}

func TestPlatformFrontDoorManagedRollbackSequence(t *testing.T) {
	frontDomain := managedFrontDoorPublicOrigin
	backendDomain := managedFrontDoorBackendOrigin
	var domainUpdates []string
	deployments := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"` + frontDomain + `","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","git_repository":"acme/mcp-devbox","git_branch":"main","fqdn":"` + backendDomain + `"}`))
		case r.Method == http.MethodPatch && (r.URL.Path == "/api/v1/applications/front1" || r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID):
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			domains, _ := payload["domains"].(string)
			domainUpdates = append(domainUpdates, r.URL.Path+"="+domains)
			if r.URL.Path == "/api/v1/applications/front1" {
				frontDomain = domains
			} else {
				backendDomain = domains
			}
			_, _ = w.Write([]byte(`{"uuid":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`[{"uuid":"env1","key":"MCP_FRONT_DOOR_BACKEND_URL"},{"uuid":"env2","key":"MCP_FRONT_DOOR_EXPECTED_PROTOCOL"},{"uuid":"env3","key":"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deployments++
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep2","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deployments/dep2":
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep2","status":"finished"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredManagedCutoverService(t, ts.URL)
	svc.PlatformCapability.managedFrontDoorProbe = func(_ context.Context, origin string, _ bool, _ string, _ string, _ string) error {
		if origin == managedFrontDoorPublicOrigin && deployments < 2 {
			return errors.New("public backend was probed before routing deployment")
		}
		return nil
	}
	preview, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: managedFrontDoorTemporaryOrigin, BackendURL: managedFrontDoorPublicOrigin,
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	// The existing assertion counts only the two front-door deployments.
	deployments -= 2
	want := []string{
		"/api/v1/applications/front1=" + managedFrontDoorTemporaryOrigin,
		"/api/v1/applications/" + managedBackendAppUUID + "=" + managedFrontDoorBackendOrigin + "," + managedFrontDoorPublicOrigin,
		"/api/v1/applications/" + managedBackendAppUUID + "=" + managedFrontDoorPublicOrigin,
	}
	if !reflect.DeepEqual(domainUpdates, want) || deployments != 2 || !strings.Contains(out, "action: rollback") || !strings.Contains(out, "temporary_domain_deployment_id: dep2") {
		t.Fatalf("domain sequence=%v want=%v deployments=%d out=%s", domainUpdates, want, deployments, out)
	}
}

func TestPlatformFrontDoorRenameTemporaryCompensatesOnProbeFailure(t *testing.T) {
	frontDomain := managedFrontDoorLegacyOrigin
	var updates []string
	deployments := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte("{\"object\":{\"sha\":\"" + frontDoorTestSHA + "\"}}"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte("[{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\"}]"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte("{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\",\"status\":\"running:healthy\",\"git_repository\":\"acme/mcp-devbox\",\"git_branch\":\"front-door-stable\",\"git_commit_sha\":\"" + frontDoorTestSHA + "\",\"fqdn\":\"" + frontDomain + "\",\"build_pack\":\"dockerfile\",\"dockerfile_location\":\"/Dockerfile.front-door\",\"ports_exposes\":\"8765\",\"is_auto_deploy_enabled\":false,\"instant_deploy\":false,\"health_check_path\":\"/front-door/healthz\"}"))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			frontDomain, _ = payload["domains"].(string)
			updates = append(updates, frontDomain)
			_, _ = w.Write([]byte("{\"uuid\":\"front1\"}"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deployments++
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep-rename","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deployments/dep-rename":
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep-rename","status":"finished"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredManagedCutoverService(t, ts.URL)
	svc.PlatformCapability.managedFrontDoorProbe = func(_ context.Context, origin string, _ bool, _ string, _ string, _ string) error {
		if deployments < 1 {
			return errors.New("probe ran before deployment")
		}
		if origin == managedFrontDoorTemporaryOrigin {
			return errors.New("probe failed")
		}
		return nil
	}
	preview, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: managedFrontDoorTemporaryOrigin, BackendURL: managedFrontDoorPublicOrigin,
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true); err == nil {
		t.Fatal("failed temporary origin was accepted")
	}
	want := []string{managedFrontDoorTemporaryOrigin, managedFrontDoorLegacyOrigin}
	if !reflect.DeepEqual(updates, want) || frontDomain != managedFrontDoorLegacyOrigin || deployments != 2 {
		t.Fatalf("updates=%v domain=%s deployments=%d", updates, frontDomain, deployments)
	}
}

func TestPlatformFrontDoorPublicReconcileRejectsBackendOriginDrift(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte("{\"object\":{\"sha\":\"" + frontDoorTestSHA + "\"}}"))
		case r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte("[{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\"}]"))
		case r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte("{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\",\"status\":\"running:healthy\",\"git_repository\":\"acme/mcp-devbox\",\"git_branch\":\"front-door-stable\",\"git_commit_sha\":\"" + frontDoorTestSHA + "\",\"fqdn\":\"" + managedFrontDoorPublicOrigin + "\",\"build_pack\":\"dockerfile\",\"dockerfile_location\":\"/Dockerfile.front-door\",\"ports_exposes\":\"8765\",\"is_auto_deploy_enabled\":false,\"instant_deploy\":false,\"health_check_path\":\"/front-door/healthz\"}"))
		case r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte("{\"uuid\":\"" + managedBackendAppUUID + "\",\"git_repository\":\"acme/mcp-devbox\",\"git_branch\":\"main\",\"fqdn\":\"" + managedFrontDoorPublicOrigin + "\"}"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredManagedCutoverService(t, ts.URL)
	_, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: managedFrontDoorPublicOrigin, BackendURL: managedFrontDoorBackendOrigin,
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err == nil || !strings.Contains(err.Error(), "expected origin") {
		t.Fatalf("backend origin drift was accepted: %v", err)
	}
}
