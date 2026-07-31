package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlatformFrontDoorCutoverFailsClosedWithoutExternalCoordinator(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"` + managedFrontDoorTemporaryOrigin + `","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/"+managedBackendAppUUID:
			_, _ = w.Write([]byte(`{"uuid":"` + managedBackendAppUUID + `","git_repository":"acme/mcp-devbox","git_branch":"main","fqdn":"` + managedFrontDoorPublicOrigin + `,` + managedFrontDoorBackendOrigin + `"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredManagedCutoverService(t, ts.URL)
	svc.PlatformCapability.managedFrontDoorExternalCoordinator = false
	_, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: managedFrontDoorPublicOrigin, BackendURL: managedFrontDoorBackendOrigin,
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err == nil || !strings.Contains(err.Error(), "external coordinator") {
		t.Fatalf("self-severing cutover was not rejected: %v", err)
	}
}
