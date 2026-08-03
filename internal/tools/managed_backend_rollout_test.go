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
	service.WithCoolify(&CoolifyClient{baseURL: server.URL, token: "token", http: server.Client()})
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
	service.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	app := platformApplication{UUID: managedBackendAppUUID, GitRepository: "https://github.com/acme/mcp-devbox.git", GitBranch: managedBackendBranch}
	if _, err := service.PlatformCapability.managedBackendRolloutIdentity(t.Context(), app); err == nil || !strings.Contains(err.Error(), "green checks") {
		t.Fatalf("incomplete checks were accepted: %v", err)
	}
}

func TestActionPlanPeekDoesNotConsumeOrExposeMutableArgs(t *testing.T) {
	service, logger := newTestService(t, config.ModeAllow)
	store := NewActionPlanStore(logger)
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
	_ = service
}
