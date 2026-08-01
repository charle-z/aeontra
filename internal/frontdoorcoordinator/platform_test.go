package frontdoorcoordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testFrontCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBackCommit  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testProtocol    = "2024-11-05"
	testCatalog     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func validClientConfig(coolifyURL string) Config {
	return Config{
		CoolifyURL: coolifyURL, CoolifyToken: "token", CoordinatorAppID: "coord1",
		FrontAppID: "front1", BackendAppID: "backend1", ExpectedFrontCommit: testFrontCommit,
		ExpectedBackendCommit: testBackCommit, ExpectedProtocol: testProtocol, ExpectedCatalogHash: testCatalog,
	}
}

func TestNewClientValidatesFixedIdentity(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(validClientConfig("https://coolify.example")); err != nil {
		t.Fatal(err)
	}
	cases := []Config{
		validClientConfig("http://coolify.example"),
		validClientConfig("https://coolify.example/path"),
		validClientConfig("https://user@coolify.example"),
	}
	badCommit := validClientConfig("https://coolify.example")
	badCommit.ExpectedFrontCommit = "abc"
	cases = append(cases, badCommit)
	badProtocol := validClientConfig("https://coolify.example")
	badProtocol.ExpectedProtocol = "latest"
	cases = append(cases, badProtocol)
	badCatalog := validClientConfig("https://coolify.example")
	badCatalog.ExpectedCatalogHash = "sha256:bad"
	cases = append(cases, badCatalog)
	for _, cfg := range cases {
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
}

func TestDecodeDeploymentResponseSupportsDirectAndWrappedShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{raw: `{"deployment_uuid":"dep1","status":"queued"}`, want: "dep1"},
		{raw: `{"uuid":"dep2","status":"queued"}`, want: "dep2"},
		{raw: `{"deployments":[{"deployment_uuid":"dep3","status":"queued"}]}`, want: "dep3"},
	}
	for _, tc := range cases {
		got := decodeDeploymentResponse([]byte(tc.raw))
		if got.DeploymentUUID != tc.want || got.Status != "queued" {
			t.Fatalf("decode %s = %+v", tc.raw, got)
		}
	}
	if got := decodeDeploymentResponse([]byte(`{"message":"no deployment"}`)); got.DeploymentUUID != "" {
		t.Fatalf("unexpected deployment: %+v", got)
	}
}

func TestTopologyReadsRepositoryBranchesDomainsAndFrontBackend(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","git_repository":"https://github.com/charle-z/mcp-devbox.git","git_branch":"front-door-stable","fqdn":"` + FrontTemporaryOrigin + `"}`))
		case "/api/v1/applications/backend1":
			_, _ = w.Write([]byte(`{"uuid":"backend1","repository":"charle-z/mcp-devbox","branch":"main","fqdn":"` + FrontPublicOrigin + `,` + BackendOrigin + `"}`))
		case "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`{"data":[` + managedEnvironmentEntryJSON("token", "MCP_FRONT_DOOR_BACKEND_URL", BackendOrigin) + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	client, err := NewClient(validClientConfig(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	client.http = ts.Client()
	topology, err := client.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if topology.FrontDomain != FrontTemporaryOrigin || topology.FrontBackendURL != BackendOrigin || topology.BackendDomains != FrontPublicOrigin+","+BackendOrigin {
		t.Fatalf("topology=%+v", topology)
	}
}

func TestTopologyRejectsAmbiguousFrontBackend(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","git_repository":"charle-z/mcp-devbox","git_branch":"front-door-stable","fqdn":"` + FrontTemporaryOrigin + `"}`))
		case "/api/v1/applications/backend1":
			_, _ = w.Write([]byte(`{"uuid":"backend1","git_repository":"charle-z/mcp-devbox","git_branch":"main","fqdn":"` + FrontPublicOrigin + `"}`))
		case "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`[` + managedEnvironmentEntryJSON("token", "MCP_FRONT_DOOR_BACKEND_URL", FrontPublicOrigin) + `,` + managedEnvironmentEntryJSON("token", "MCP_FRONT_DOOR_BACKEND_URL", BackendOrigin) + `]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	client, err := NewClient(validClientConfig(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	client.http = ts.Client()
	if _, err := client.Topology(context.Background()); err != ErrTopologyFrontBackend {
		t.Fatalf("ambiguous topology accepted: %v", err)
	}
}

func TestProbeOnceVerifiesFrontAndProxiedBackendIdentity(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/front-door/healthz", "/front-door/readyz", "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/front-door/version":
			w.Header().Set("X-MCP-Front-Door-Commit", testFrontCommit)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "commit": testFrontCommit, "backend_ready": true,
				"backend": map[string]any{"status": "ok", "protocol_version": testProtocol, "commit": testBackCommit, "catalog_hash": testCatalog, "tool_count": 114},
			})
		case "/version":
			w.Header().Set("X-MCP-Server-Commit", testBackCommit)
			w.Header().Set("X-MCP-Catalog-Hash", testCatalog)
			w.Header().Set("X-MCP-Front-Door-Commit", testFrontCommit)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "protocol_version": testProtocol, "commit": testBackCommit, "catalog_hash": testCatalog, "tool_count": 114})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	client, err := NewClient(validClientConfig("https://coolify.example"))
	if err != nil {
		t.Fatal(err)
	}
	probeClient := ts.Client()
	probeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.probeHTTP = probeClient
	if err := client.probeOnce(context.Background(), ts.URL, true); err != nil {
		t.Fatal(err)
	}
	if err := client.probeOnce(context.Background(), ts.URL, false); err != nil {
		t.Fatal(err)
	}
}

func TestProbeOnceRejectsRedirect(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
	}))
	defer ts.Close()
	client, err := NewClient(validClientConfig("https://coolify.example"))
	if err != nil {
		t.Fatal(err)
	}
	probeClient := ts.Client()
	probeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.probeHTTP = probeClient
	if err := client.probeOnce(context.Background(), ts.URL, false); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect accepted: %v", err)
	}
}

func managedEnvironmentEntryJSON(token, key, value string) string {
	encoded, _ := json.Marshal(environmentEntry{
		Key: key, Comment: ManagedEnvironmentComment(token, key, value),
		IsLiteral: true, IsRuntime: true,
	})
	return string(encoded)
}
