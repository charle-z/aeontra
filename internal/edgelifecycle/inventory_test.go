package edgelifecycle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectLayoutPrefersCurrentStateWithoutMigration(t *testing.T) {
	home := t.TempDir()
	preferred := filepath.Join(home, ".local", "state", "mcp-edge")
	writeFixtureFile(t, filepath.Join(preferred, "identity.json"), "preferred-identity")
	writeFixtureFile(t, filepath.Join(preferred, "workspaces.db"), "workspace-state")

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.PreferredState.Kind != PathEdgeState || !report.PreferredState.IdentityPresent {
		t.Fatalf("preferred=%+v", report.PreferredState)
	}
	if report.LegacyState.Kind != PathMissing {
		t.Fatalf("legacy=%+v", report.LegacyState)
	}
	if report.StateMigration != MigrationNone || len(report.Blockers) != 0 {
		t.Fatalf("migration=%q blockers=%+v", report.StateMigration, report.Blockers)
	}
}

func TestInspectLayoutMarksLegacyOnlyStateForMigration(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeFixtureFile(t, filepath.Join(legacy, "identity.json"), "legacy-identity")
	writeFixtureFile(t, filepath.Join(legacy, "workspaces.db"), "workspace-state")

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.LegacyState.Kind != PathEdgeState || report.StateMigration != MigrationLegacyToPreferred {
		t.Fatalf("legacy=%+v migration=%q", report.LegacyState, report.StateMigration)
	}
	if len(report.Blockers) != 0 {
		t.Fatalf("blockers=%+v", report.Blockers)
	}
}

func TestInspectLayoutBlocksConflictingPreferredAndLegacyIdentities(t *testing.T) {
	home := t.TempDir()
	writeFixtureFile(t, filepath.Join(home, ".local", "state", "mcp-edge", "identity.json"), "preferred")
	writeFixtureFile(t, filepath.Join(home, ".config", "mcp-devbox-edge", "identity.json"), "legacy")

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.StateMigration != MigrationBlocked {
		t.Fatalf("migration=%q", report.StateMigration)
	}
	assertBlocker(t, report.Blockers, BlockerStateIdentityConflict)
}

func TestInspectLayoutClassifiesUnknownP12DirectoryWithoutMutation(t *testing.T) {
	home := t.TempDir()
	p12 := filepath.Join(home, "p12")
	writeFixtureFile(t, filepath.Join(p12, "notes.txt"), "do not touch")
	before := snapshotTree(t, p12)

	report, err := InspectLayout(InventoryConfig{
		HomeDir:         home,
		InstallRoot:     filepath.Join(home, "opt", "mcp-devbox"),
		HistoricalPaths: []string{p12},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Historical) != 1 || report.Historical[0].Kind != PathUnknownDirectory {
		t.Fatalf("historical=%+v", report.Historical)
	}
	after := snapshotTree(t, p12)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("inventory mutated unknown p12 directory\nbefore=%v\nafter=%v", before, after)
	}
}

func TestInspectLayoutClassifiesHistoricalRepositoryAndSignedRelease(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "p12-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(home, "p15-release")
	writeFixtureFile(t, filepath.Join(release, "manifest.json"), "{}")
	writeFixtureFile(t, filepath.Join(release, "manifest.sig"), "signature")

	report, err := InspectLayout(InventoryConfig{
		HomeDir:         home,
		InstallRoot:     filepath.Join(home, "opt", "mcp-devbox"),
		HistoricalPaths: []string{repo, release},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []PathKind{report.Historical[0].Kind, report.Historical[1].Kind}; !reflect.DeepEqual(got, []PathKind{PathRepository, PathSignedRelease}) {
		t.Fatalf("historical kinds=%v", got)
	}
}

func TestInspectLayoutRejectsSymlinkedStateAndWorkspaceRoots(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	preferred := filepath.Join(home, ".local", "state", "mcp-edge")
	if err := os.MkdirAll(filepath.Dir(preferred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, preferred); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "workspaces")); err != nil {
		t.Fatal(err)
	}

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	assertBlocker(t, report.Blockers, BlockerPreferredStateSymlink)
	assertBlocker(t, report.Blockers, BlockerDevelopmentRootSymlink)
	if report.StateMigration != MigrationBlocked {
		t.Fatalf("migration=%q", report.StateMigration)
	}
}

func TestInspectLayoutBlocksCurrentReleaseOutsideReleaseRoot(t *testing.T) {
	home := t.TempDir()
	installRoot := filepath.Join(home, "opt", "mcp-devbox")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "other-release"), filepath.Join(installRoot, "current")); err != nil {
		t.Fatal(err)
	}

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: installRoot})
	if err != nil {
		t.Fatal(err)
	}
	assertBlocker(t, report.Blockers, BlockerCurrentReleaseOutsideRoot)
}

func TestInspectLayoutIsDeterministicAndDoesNotCreateMissingRoots(t *testing.T) {
	home := t.TempDir()
	config := InventoryConfig{
		HomeDir:         home,
		InstallRoot:     filepath.Join(home, "opt", "mcp-devbox"),
		HistoricalPaths: []string{filepath.Join(home, "p12")},
	}
	first, err := InspectLayout(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InspectLayout(config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("inventory not deterministic\nfirst=%+v\nsecond=%+v", first, second)
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "state", "mcp-edge"),
		filepath.Join(home, ".config", "mcp-devbox-edge"),
		filepath.Join(home, "workspaces"),
		filepath.Join(home, "htb-machines"),
		filepath.Join(home, "p12"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("inventory created %s: %v", path, err)
		}
	}
}

func writeFixtureFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertBlocker(t *testing.T, blockers []Blocker, code BlockerCode) {
	t.Helper()
	for _, blocker := range blockers {
		if blocker.Code == code {
			return
		}
	}
	t.Fatalf("missing blocker %q in %+v", code, blockers)
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			result[relative] = "dir:" + info.Mode().Perm().String()
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = "file:" + info.Mode().Perm().String() + ":" + string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
