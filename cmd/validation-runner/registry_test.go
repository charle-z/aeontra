package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createRepositoryFixture(t *testing.T, root, repoID string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(root, repoID)
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRegistryFixture(t *testing.T, repoID string) (*repositoryRegistry, string) {
	t.Helper()
	root := t.TempDir()
	createRepositoryFixture(t, root, repoID, 0o700)
	registry, err := discoverRepositoryRegistry(root, filepath.Join(t.TempDir(), "host-repositories"), time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return registry, root
}

func TestRepositoryRegistryAcceptsOnlyDiscoveredDirectRepository(t *testing.T) {
	registry, root := newRegistryFixture(t, "valid-repo")
	entry, err := registry.lookup("valid-repo")
	if err != nil {
		t.Fatal(err)
	}
	if entry.repoID != "valid-repo" || entry.canonicalPath != filepath.Join(root, "valid-repo") || entry.mode != repositoryModeReadWrite || entry.discoveredAt.IsZero() {
		t.Fatalf("unexpected registry entry metadata")
	}
	if filepath.Base(entry.hostPath) != "valid-repo" {
		t.Fatal("host mount was not derived from the server-owned snapshot")
	}
	if _, err := registry.lookup("unknown"); err == nil {
		t.Fatal("unknown repository accepted")
	}
}

func TestRepositoryRegistryRejectsRemotePathSyntax(t *testing.T) {
	registry, _ := newRegistryFixture(t, "valid-repo")
	for _, value := range []string{"", ".", "..", "../valid-repo", "valid-repo/child", `valid-repo\\child`, "/absolute", "valid\x00repo", " valid-repo", "valid-repo ", "repo..other"} {
		if _, err := registry.lookup(value); err == nil {
			t.Fatalf("accepted invalid repo_id %q", value)
		}
	}
}

func TestRepositoryRegistryRejectsSymlinkAtDiscoveryAndRegistration(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	createRepositoryFixture(t, outside, "target", 0o700)
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	registry, err := discoverRepositoryRegistry(root, filepath.Join(t.TempDir(), "host"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.lookup("linked"); err == nil {
		t.Fatal("symlink was registered")
	}
	if err := registry.register("linked", time.Now()); err == nil {
		t.Fatal("explicit symlink registration succeeded")
	}
}

func TestRepositoryRegistryRejectsSymlinkSubstitutionAfterDiscovery(t *testing.T) {
	registry, root := newRegistryFixture(t, "repo")
	outside := t.TempDir()
	createRepositoryFixture(t, outside, "replacement", 0o700)
	if err := os.RemoveAll(filepath.Join(root, "repo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "replacement"), filepath.Join(root, "repo")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.lookup("repo"); err == nil {
		t.Fatal("symlink substitution was accepted")
	}
}

func TestRepositoryRegistryRejectsMovedOrReplacedRepository(t *testing.T) {
	t.Run("moved", func(t *testing.T) {
		registry, root := newRegistryFixture(t, "repo")
		if err := os.Rename(filepath.Join(root, "repo"), filepath.Join(root, "moved")); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.lookup("repo"); err == nil {
			t.Fatal("moved repository accepted")
		}
	})
	t.Run("replaced", func(t *testing.T) {
		registry, root := newRegistryFixture(t, "repo")
		if err := os.Rename(filepath.Join(root, "repo"), filepath.Join(root, "old")); err != nil {
			t.Fatal(err)
		}
		createRepositoryFixture(t, root, "repo", 0o700)
		if _, err := registry.lookup("repo"); err == nil {
			t.Fatal("replacement repository accepted")
		}
	})
}

func TestRepositoryRegistryRejectsReplacedRoot(t *testing.T) {
	registry, root := newRegistryFixture(t, "repo")
	oldRoot := root + "-old"
	if err := os.Rename(root, oldRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	createRepositoryFixture(t, root, "repo", 0o700)
	if _, err := registry.lookup("repo"); err == nil {
		t.Fatal("replacement root accepted")
	}
}

func TestRepositoryRegistryRejectsNestedOutsideAndPrefixMatches(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	createRepositoryFixture(t, parent, "nested", 0o700)
	outsideRoot := root + "-outside"
	if err := os.Mkdir(outsideRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	createRepositoryFixture(t, outsideRoot, "outside", 0o700)
	registry, err := discoverRepositoryRegistry(root, filepath.Join(t.TempDir(), "host"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"nested", "outside", filepath.Base(outsideRoot)} {
		if _, err := registry.lookup(id); err == nil {
			t.Fatalf("accepted non-direct repository %q", id)
		}
	}
}

func TestRepositoryRegistryRejectsWritableDirectoriesAndChanges(t *testing.T) {
	for _, mode := range []os.FileMode{0o720, 0o702, 0o777} {
		root := t.TempDir()
		createRepositoryFixture(t, root, "repo", mode)
		registry, err := discoverRepositoryRegistry(root, filepath.Join(t.TempDir(), "host"), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.lookup("repo"); err == nil {
			t.Fatalf("registered repository mode %o", mode)
		}
	}
	registry, root := newRegistryFixture(t, "repo")
	if err := os.Chmod(filepath.Join(root, "repo"), 0o770); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.lookup("repo"); err == nil {
		t.Fatal("permission change was accepted")
	}
}

func TestRepositoryRegistryRejectsDuplicateRegistration(t *testing.T) {
	registry, _ := newRegistryFixture(t, "repo")
	if err := registry.register("repo", time.Now()); err == nil {
		t.Fatal("duplicate registry entry accepted")
	}
}

func TestValidationRequestCannotChooseContainerOrHostPath(t *testing.T) {
	registry, _ := newRegistryFixture(t, "repo")
	called := false
	cfg := config{
		token:    "01234567890123456789012345678901",
		registry: registry,
		image:    "node:22-alpine",
		store:    "store",
		user:     "10001:10001",
		timeout:  time.Minute,
		runDocker: func(_ context.Context, _ []string) (string, int, error) {
			called = true
			return "", 0, nil
		},
	}
	body := `{"repo_id":"repo","profile":"pnpm-validate","container_path":"/chosen"}`
	request := httptest.NewRequest(http.MethodPost, runPath, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+cfg.token)
	response := httptest.NewRecorder()
	cfg.handleRun(response, request)
	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("closed request schema status=%d called=%t", response.Code, called)
	}
	if strings.Contains(response.Body.String(), registry.root) || strings.Contains(response.Body.String(), registry.hostRoot) {
		t.Fatal("invalid request exposed registry paths")
	}
}

func TestValidationResponseRedactsHostPathsAndUsesFixedWorkspace(t *testing.T) {
	registry, _ := newRegistryFixture(t, "repo")
	entry, err := registry.lookup("repo")
	if err != nil {
		t.Fatal(err)
	}
	var captured []string
	cfg := config{
		token:    "01234567890123456789012345678901",
		registry: registry,
		image:    "node:22-alpine",
		store:    "store",
		user:     "10001:10001",
		timeout:  time.Minute,
		runDocker: func(_ context.Context, argv []string) (string, int, error) {
			captured = append([]string(nil), argv...)
			return entry.hostPath + " " + entry.canonicalPath + " completed", 0, nil
		},
	}
	body, err := json.Marshal(request{RepoID: "repo", Profile: "pnpm-validate"})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, runPath, strings.NewReader(string(body)))
	httpRequest.Header.Set("Authorization", "Bearer "+cfg.token)
	response := httptest.NewRecorder()
	cfg.handleRun(response, httpRequest)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	joined := strings.Join(captured, " ")
	if !strings.Contains(joined, "type=bind,src="+entry.hostPath+",dst=/workspace") || strings.Contains(joined, "dst=/chosen") {
		t.Fatal("mount did not use the fixed container destination")
	}
	if strings.Contains(response.Body.String(), entry.hostPath) || strings.Contains(response.Body.String(), entry.canonicalPath) {
		t.Fatal("validation response exposed a host or registry path")
	}
}
