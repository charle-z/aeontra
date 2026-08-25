package frontdoorcoordinator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
)

type rewriteProbeTransport struct {
	target    *url.URL
	transport http.RoundTripper
}

func (r rewriteProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	return r.transport.RoundTrip(clone)
}

type rolloutFixture struct {
	mu              sync.Mutex
	previous        catalogrollout.Identity
	candidate       catalogrollout.Identity
	runtime         catalogrollout.Identity
	frontPrimary    string
	frontTransition string
	protocol        string
	pinnedCommit    string
	backendBranch   string
	autoDeploy      bool
	instantDeploy   bool
	backendDeploys  int
	backendStops    int
	frontDeploys    int
	backendRunning  bool
	backendStopFail bool
	backendBranches []string
	oauthCalls      int
	mcpCalls        int
	mcpFail         bool
	description     string
	runtimeFailures int
	runtimeProbes   int
}

func newRolloutFixture() *rolloutFixture {
	previous := catalogrollout.Identity{Commit: strings.Repeat("a", 40), ProtocolVersion: testProtocol, ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("1", 64)}
	candidate := catalogrollout.Identity{Commit: strings.Repeat("b", 40), ProtocolVersion: testProtocol, ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("2", 64)}
	return &rolloutFixture{previous: previous, candidate: candidate, runtime: previous, frontPrimary: previous.CatalogHash, protocol: testProtocol, pinnedCommit: previous.Commit, backendBranch: "main", backendRunning: true}
}

func (f *rolloutFixture) coolifyHandler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/backend1":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uuid": "backend1", "git_repository": ManagedRepository, "git_branch": f.backendBranch,
			"git_commit_sha": f.pinnedCommit, "is_auto_deploy_enabled": f.autoDeploy, "instant_deploy": f.instantDeploy,
			"status": map[bool]string{true: "running:healthy", false: "exited:stopped"}[f.backendRunning],
		})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/backend1/stop":
		f.backendStops++
		if f.backendStopFail {
			http.Error(w, "stop failed", http.StatusInternalServerError)
			return
		}
		f.backendRunning = false
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
		entries := []environmentEntry{
			{Key: "MCP_FRONT_DOOR_EXPECTED_PROTOCOL", Comment: ManagedEnvironmentComment("token", "MCP_FRONT_DOOR_EXPECTED_PROTOCOL", f.protocol), IsLiteral: true, IsRuntime: true},
			{Key: "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", Comment: ManagedEnvironmentComment("token", "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", f.frontPrimary), IsLiteral: true, IsRuntime: true},
		}
		if f.frontTransition != "" {
			entries = append(entries, environmentEntry{Key: "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH", Comment: ManagedEnvironmentComment("token", "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH", f.frontTransition), IsLiteral: true, IsRuntime: true})
		}
		_ = json.NewEncoder(w).Encode(entries)
	case (r.Method == http.MethodPost || r.Method == http.MethodPatch) && r.URL.Path == "/api/v1/applications/front1/envs":
		var entry environmentEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			panic(err)
		}
		switch entry.Key {
		case "MCP_FRONT_DOOR_EXPECTED_PROTOCOL":
			f.protocol = entry.Value
		case "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH":
			f.frontPrimary = entry.Value
		case "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH":
			f.frontTransition = entry.Value
		default:
			panic("unexpected environment key")
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/backend1":
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			panic(err)
		}
		f.pinnedCommit, _ = payload["git_commit_sha"].(string)
		if branch, ok := payload["git_branch"].(string); ok {
			f.backendBranch = branch
		}
		f.autoDeploy, _ = payload["is_auto_deploy_enabled"].(bool)
		f.instantDeploy, _ = payload["instant_deploy"].(bool)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/coord1":
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			panic(err)
		}
		f.description, _ = payload["description"].(string)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
		app := r.URL.Query().Get("uuid")
		if app == "backend1" {
			if f.backendRunning {
				panic("backend deployment attempted before the previous singleton stopped")
			}
			f.backendDeploys++
			f.backendBranches = append(f.backendBranches, f.backendBranch)
			switch f.backendBranch {
			case ManagedBackendRollbackBranch:
				f.runtime = f.previous
			case "main":
				f.runtime = f.candidate
			default:
				panic("unknown backend branch")
			}
			f.pinnedCommit = f.runtime.Commit
			f.backendRunning = true
		} else if app == "front1" {
			f.frontDeploys++
		} else {
			panic("unexpected deployment app")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"deployment_uuid": app + "deploy"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/deployments/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "finished"})
	default:
		http.NotFound(w, r)
	}
}

func (f *rolloutFixture) probeHandler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	catalogAllowed := f.runtime.CatalogHash == f.frontPrimary || (f.frontTransition != "" && f.runtime.CatalogHash == f.frontTransition)
	switch r.URL.Path {
	case "/readyz":
		f.runtimeProbes++
		if f.runtimeFailures > 0 {
			f.runtimeFailures--
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "/version":
		w.Header().Set("X-MCP-Server-Commit", f.runtime.Commit)
		w.Header().Set("X-MCP-Catalog-Hash", f.runtime.CatalogHash)
		w.Header().Set("X-MCP-Front-Door-Commit", testFrontCommit)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "protocol_version": f.runtime.ProtocolVersion, "commit": f.runtime.Commit, "tool_count": f.runtime.ToolCount, "catalog_hash": f.runtime.CatalogHash})
	case "/front-door/readyz":
		if !catalogAllowed {
			http.Error(w, "blocked", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "/front-door/version":
		w.Header().Set("X-MCP-Front-Door-Commit", testFrontCommit)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "commit": testFrontCommit, "backend_ready": catalogAllowed, "backend": map[string]any{"status": "ok", "protocol_version": f.runtime.ProtocolVersion, "commit": f.runtime.Commit, "tool_count": f.runtime.ToolCount, "catalog_hash": f.runtime.CatalogHash}})
	case "/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server":
		f.oauthCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": "https://mcp-devbox-charlez.duckdns.org"})
	case "/mcp":
		if !catalogAllowed || r.Header.Get("Authorization") != "Bearer smoke-token" {
			http.Error(w, "blocked", http.StatusUnauthorized)
			return
		}
		f.mcpCalls++
		if f.mcpFail {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-MCP-Catalog-Hash", f.runtime.CatalogHash)
		w.Header().Set("X-MCP-Front-Door-Commit", testFrontCommit)
		w.Header().Set("Mcp-Session-Id", "session1")
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"method":"tools/call"`) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "commit=" + f.runtime.Commit + " catalog_hash=" + f.runtime.CatalogHash}}, "isError": false}})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"protocolVersion": f.runtime.ProtocolVersion}})
		}
	default:
		http.NotFound(w, r)
	}
}

func testCatalogPlatform(t *testing.T, fixture *rolloutFixture) (*CatalogPlatform, func()) {
	t.Helper()
	coolify := httptest.NewTLSServer(http.HandlerFunc(fixture.coolifyHandler))
	probe := httptest.NewTLSServer(http.HandlerFunc(fixture.probeHandler))
	client, err := NewClient(validClientConfig(coolify.URL))
	if err != nil {
		t.Fatal(err)
	}
	client.http = coolify.Client()
	probeURL, _ := url.Parse(probe.URL)
	client.probeHTTP = &http.Client{
		Transport:     rewriteProbeTransport{target: probeURL, transport: probe.Client().Transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	client.sleep = func(time.Duration) {}
	platform, err := NewCatalogPlatform(client, "smoke-token")
	if err != nil {
		t.Fatal(err)
	}
	return platform, func() { coolify.Close(); probe.Close() }
}

func TestCatalogPlatformRunnerExecutesChangedCatalogEndToEnd(t *testing.T) {
	fixture := newRolloutFixture()
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()
	journal, err := catalogrollout.OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := catalogrollout.Request{RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Previous: fixture.previous, Candidate: fixture.candidate}
	status, err := (catalogrollout.Runner{Platform: platform, Journal: journal}).Run(context.Background(), request)
	if err != nil || status.State != catalogrollout.StateSucceeded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.backendDeploys != 1 || fixture.backendStops != 1 || strings.Join(fixture.backendBranches, ",") != "main" || fixture.frontDeploys != 2 || fixture.pinnedCommit != fixture.candidate.Commit || fixture.autoDeploy || fixture.instantDeploy || fixture.frontPrimary != fixture.candidate.CatalogHash || fixture.frontTransition != "" || fixture.oauthCalls != 2 || fixture.mcpCalls != 2 || !strings.HasPrefix(fixture.description, catalogrollout.PublishedStatusPrefix) {
		t.Fatalf("fixture=%+v", fixture)
	}
}

func TestCatalogPlatformRunnerSkipsFrontForUnchangedCatalog(t *testing.T) {
	fixture := newRolloutFixture()
	fixture.candidate.CatalogHash = fixture.previous.CatalogHash
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()
	journal, _ := catalogrollout.OpenJournal(t.TempDir())
	request := catalogrollout.Request{RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Previous: fixture.previous, Candidate: fixture.candidate}
	status, err := (catalogrollout.Runner{Platform: platform, Journal: journal}).Run(context.Background(), request)
	if err != nil || status.State != catalogrollout.StateSucceeded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.backendDeploys != 1 || fixture.backendStops != 1 || strings.Join(fixture.backendBranches, ",") != "main" || fixture.frontDeploys != 0 {
		t.Fatalf("backend=%d stops=%d branches=%v front=%d", fixture.backendDeploys, fixture.backendStops, fixture.backendBranches, fixture.frontDeploys)
	}
}

func TestCatalogPlatformRunnerRollsBackAfterMCPFailure(t *testing.T) {
	fixture := newRolloutFixture()
	fixture.mcpFail = true
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()
	journal, _ := catalogrollout.OpenJournal(t.TempDir())
	request := catalogrollout.Request{RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Previous: fixture.previous, Candidate: fixture.candidate}
	status, err := (catalogrollout.Runner{Platform: platform, Journal: journal}).Run(context.Background(), request)
	if err == nil || status.State != catalogrollout.StateFailed {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.backendDeploys != 2 || fixture.backendStops != 2 || strings.Join(fixture.backendBranches, ",") != "main,"+ManagedBackendRollbackBranch || fixture.frontDeploys != 2 || fixture.runtime != fixture.previous || fixture.frontPrimary != fixture.previous.CatalogHash || fixture.frontTransition != "" || fixture.backendBranch != "main" {
		t.Fatalf("fixture=%+v", fixture)
	}
}

func TestCatalogPlatformDoesNotDeployWhenSingletonStopFails(t *testing.T) {
	fixture := newRolloutFixture()
	fixture.backendStopFail = true
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()
	if _, err := platform.DeployBackend(context.Background(), fixture.candidate); err == nil {
		t.Fatal("expected singleton stop failure")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.backendStops != 1 || fixture.backendDeploys != 0 || fixture.runtime != fixture.previous {
		t.Fatalf("fixture=%+v", fixture)
	}
}

func TestCatalogPlatformObserveRejectsEnabledAutoDeploy(t *testing.T) {
	fixture := newRolloutFixture()
	fixture.autoDeploy = true
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()
	if _, err := platform.Observe(context.Background()); err == nil || !strings.Contains(err.Error(), "auto-deploy") {
		t.Fatalf("err=%v", err)
	}
}

func TestCatalogPlatformObserveRetriesTransientRuntimeReadiness(t *testing.T) {
	fixture := newRolloutFixture()
	fixture.runtimeFailures = 1
	platform, closeFn := testCatalogPlatform(t, fixture)
	defer closeFn()

	observation, err := platform.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if observation.Backend != fixture.previous {
		t.Fatalf("backend=%+v", observation.Backend)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.runtimeProbes != 2 {
		t.Fatalf("runtime probes=%d", fixture.runtimeProbes)
	}
}
