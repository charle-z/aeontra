package tools

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestManagedDeploymentCapabilityBlocksForceBackendDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/"+managedBackendAppUUID {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(platformApplication{
			UUID: managedBackendAppUUID, Name: "mcp-devbox",
			GitRepository: "https://github.com/charle-z/mcp-devbox.git", GitBranch: managedBackendBranch,
		})
	}))
	defer server.Close()

	service, _ := newTestService(t, config.ModeAllow)
	service.WithMaintainerProfile(MaintainerProfileCharleZProduction)
	client := NewCoolifyClient(server.URL, "token", nil)
	client.do = server.Client().Do
	service.WithCoolify(client)
	if _, err := service.PlatformDeployWithoutCachePreview(managedBackendAppUUID); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("managed backend force deployment was not blocked: %v", err)
	}
}

func TestGitHubRepositoryFileAtRefIsExactAndBounded(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := []byte(`{"schema_version":1,"protocol_version":"2024-11-05","tool_count":137,"catalog_hash":"sha256:` + strings.Repeat("b", 64) + `"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/mcp-devbox/contents/deploy/catalog-identity.json" || r.URL.Query().Get("ref") != commit {
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(githubRepositoryContent{
			Type: "file", Encoding: "base64", Content: base64.StdEncoding.EncodeToString(manifest),
			SHA: strings.Repeat("c", 40), Size: len(manifest),
		})
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	body, err := client.repositoryFileAtRef(t.Context(), "mcp-devbox", managedBackendManifestPath, commit)
	if err != nil || string(body) != string(manifest) {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if _, err := client.repositoryFileAtRef(t.Context(), "mcp-devbox", managedBackendManifestPath, "main"); err == nil {
		t.Fatal("mutable ref accepted")
	}
}

func TestGitHubManagedRollbackBranchCreatesAndFastForwards(t *testing.T) {
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	current := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/"+managedBackendRollbackBranch:
			if current == "" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": current}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/mcp-devbox/git/refs":
			var payload map[string]string
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["ref"] != "refs/heads/"+managedBackendRollbackBranch || payload["sha"] != first {
				t.Fatal("invalid managed rollback branch creation")
			}
			current = first
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/mcp-devbox/compare/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ahead"})
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/mcp-devbox/git/refs/heads/"+managedBackendRollbackBranch:
			var payload struct {
				SHA   string `json:"sha"`
				Force bool   `json:"force"`
			}
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.SHA != second || payload.Force {
				t.Fatal("managed rollback branch update was not a non-force fast-forward")
			}
			current = second
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	if err := client.ensureManagedRollbackBranch(t.Context(), "", false, first); err != nil {
		t.Fatal(err)
	}
	if err := client.ensureManagedRollbackBranch(t.Context(), first, true, second); err != nil {
		t.Fatal(err)
	}
	if current != second {
		t.Fatal(current)
	}
}

func TestGitHubManagedRollbackBranchRejectsDivergence(t *testing.T) {
	current := strings.Repeat("a", 40)
	target := strings.Repeat("b", 40)
	mutated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/"+managedBackendRollbackBranch:
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": current}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/mcp-devbox/compare/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "diverged"})
		case r.Method == http.MethodPatch:
			mutated = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	err := client.ensureManagedRollbackBranch(t.Context(), current, true, target)
	if err == nil || !strings.Contains(err.Error(), "cannot fast-forward") || mutated {
		t.Fatalf("err=%v mutated=%t", err, mutated)
	}
}

func TestManagedBackendRolloutRejectsIncompleteExactHeadChecks(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := []byte(`{"schema_version":1,"protocol_version":"2024-11-05","tool_count":137,"catalog_hash":"sha256:` + strings.Repeat("b", 64) + `"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": commit}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/contents/deploy/catalog-identity.json":
			_ = json.NewEncoder(w).Encode(githubRepositoryContent{Type: "file", Encoding: "base64", Content: base64.StdEncoding.EncodeToString(manifest), SHA: strings.Repeat("c", 40), Size: len(manifest)})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/commits/"+commit+"/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "check_runs": []map[string]any{{"id": 1, "name": "CI", "status": "in_progress", "conclusion": "", "html_url": "https://example.test/run"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/branches/main/protection/required_status_checks":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/commits/"+commit+"/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending", "total_count": 0, "statuses": []any{}})
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	service, _ := newTestService(t, config.ModeAllow)
	service.WithMaintainerProfile(MaintainerProfileCharleZProduction)
	service.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	app := platformApplication{UUID: managedBackendAppUUID, GitRepository: "https://github.com/acme/mcp-devbox.git", GitBranch: managedBackendBranch}
	if _, err := service.PlatformCapability.managedBackendRolloutIdentity(t.Context(), app); err == nil || !strings.Contains(err.Error(), "green checks") {
		t.Fatalf("incomplete checks were accepted: %v", err)
	}
}

func TestActionPlanPeekDoesNotConsumeOrExposeMutableArgs(t *testing.T) {
	service, _ := newTestService(t, config.ModeAllow)
	store := service.plans
	plan, err := store.Create("platform-deploy", map[string]string{"app": managedBackendAppUUID, "rollout": managedBackendRolloutMarker})
	if err != nil {
		t.Fatal(err)
	}
	peeked, err := store.Peek(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	peeked.Args["app"] = "changed"
	consumed, err := store.Consume(plan.ID, "platform-deploy")
	if err != nil || consumed.Args["app"] != managedBackendAppUUID {
		t.Fatalf("consumed=%+v err=%v", consumed, err)
	}
	if _, err := store.Consume(plan.ID, "platform-deploy"); err == nil {
		t.Fatal("single-use action plan was replayed")
	}
}

func TestDecodeManagedRuntimeIdentityAcceptsCanonicalVersionEnvelope(t *testing.T) {
	commit := strings.Repeat("a", 40)
	catalog := "sha256:" + strings.Repeat("b", 64)
	headers := http.Header{}
	headers.Set("X-MCP-Server-Commit", commit)
	headers.Set("X-MCP-Catalog-Hash", catalog)
	body := []byte(`{"status":"ok","version":"0.2.0","protocol_version":"2024-11-05","commit":"` + commit + `","built_at":"unknown","tool_count":137,"catalog_hash":"` + catalog + `"}`)

	identity, err := decodeManagedRuntimeIdentity(body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Commit != commit || identity.ProtocolVersion != "2024-11-05" || identity.ToolCount != 137 || identity.CatalogHash != catalog {
		t.Fatalf("identity=%+v", identity)
	}

	unknown := []byte(`{"status":"ok","version":"0.2.0","protocol_version":"2024-11-05","commit":"` + commit + `","built_at":"unknown","tool_count":137,"catalog_hash":"` + catalog + `","future":true}`)
	if _, err := decodeManagedRuntimeIdentity(unknown, headers); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unknown field accepted: %v", err)
	}
	if _, err := decodeManagedRuntimeIdentity(append(append([]byte{}, body...), []byte(`{}`)...), headers); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
	mismatched := headers.Clone()
	mismatched.Set("X-MCP-Server-Commit", strings.Repeat("c", 40))
	if _, err := decodeManagedRuntimeIdentity(body, mismatched); err == nil || !strings.Contains(err.Error(), "headers") {
		t.Fatalf("mismatched headers accepted: %v", err)
	}
}
