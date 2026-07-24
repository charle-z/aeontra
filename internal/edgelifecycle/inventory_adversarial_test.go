package edgelifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectLayoutBlocksOccupiedPreferredStateBeforeLegacyMigration(t *testing.T) {
	home := t.TempDir()
	preferred := filepath.Join(home, ".local", "state", "mcp-edge")
	writeFixtureFile(t, filepath.Join(preferred, "unrelated.txt"), "keep")
	writeFixtureFile(t, filepath.Join(home, ".config", "mcp-devbox-edge", "identity.json"), "legacy")

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.StateMigration != MigrationBlocked {
		t.Fatalf("migration=%q", report.StateMigration)
	}
	assertBlocker(t, report.Blockers, BlockerPreferredStateOccupied)
	if content, err := os.ReadFile(filepath.Join(preferred, "unrelated.txt")); err != nil || string(content) != "keep" {
		t.Fatalf("preferred state changed: content=%q err=%v", content, err)
	}
}

func TestInspectLayoutBlocksSymlinkedStateAncestor(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	local := filepath.Join(home, ".local")
	if err := os.Symlink(outside, local); err != nil {
		t.Fatal(err)
	}

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	assertBlocker(t, report.Blockers, BlockerPreferredStateAncestorLink)
	if report.StateMigration != MigrationBlocked {
		t.Fatalf("migration=%q", report.StateMigration)
	}
}

func TestInspectLayoutBlocksSymlinkedIdentityMarker(t *testing.T) {
	home := t.TempDir()
	preferred := filepath.Join(home, ".local", "state", "mcp-edge")
	if err := os.MkdirAll(preferred, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "identity-target")
	writeFixtureFile(t, target, "outside")
	if err := os.Symlink(target, filepath.Join(preferred, "identity.json")); err != nil {
		t.Fatal(err)
	}

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.PreferredState.IdentityPresent {
		t.Fatal("symlinked identity marker was accepted as identity")
	}
	assertBlocker(t, report.Blockers, BlockerPreferredIdentityLink)
	if report.StateMigration != MigrationBlocked {
		t.Fatalf("migration=%q", report.StateMigration)
	}
}

func TestInspectLayoutAcceptsCurrentReleaseInsideReleaseRoot(t *testing.T) {
	home := t.TempDir()
	installRoot := filepath.Join(home, "opt", "mcp-devbox")
	release := filepath.Join(installRoot, "releases", "p15.0.5")
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "p15.0.5"), filepath.Join(installRoot, "current")); err != nil {
		t.Fatal(err)
	}

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: installRoot})
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentRelease.Kind != PathSymlink || len(report.Blockers) != 0 {
		t.Fatalf("current=%+v blockers=%+v", report.CurrentRelease, report.Blockers)
	}
}

func TestInspectLayoutBlocksMissingAndNonSymlinkCurrentRelease(t *testing.T) {
	t.Run("missing target", func(t *testing.T) {
		home := t.TempDir()
		installRoot := filepath.Join(home, "opt", "mcp-devbox")
		if err := os.MkdirAll(installRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("releases", "missing"), filepath.Join(installRoot, "current")); err != nil {
			t.Fatal(err)
		}
		report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: installRoot})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocker(t, report.Blockers, BlockerCurrentReleaseTargetAbsent)
	})

	t.Run("not a symlink", func(t *testing.T) {
		home := t.TempDir()
		installRoot := filepath.Join(home, "opt", "mcp-devbox")
		if err := os.MkdirAll(filepath.Join(installRoot, "current"), 0o755); err != nil {
			t.Fatal(err)
		}
		report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: installRoot})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocker(t, report.Blockers, BlockerCurrentReleaseNotSymlink)
	})
}

func TestInspectLayoutBlocksWorkspaceAndLabFiles(t *testing.T) {
	home := t.TempDir()
	writeFixtureFile(t, filepath.Join(home, "workspaces"), "not-directory")
	writeFixtureFile(t, filepath.Join(home, "htb-machines"), "not-directory")

	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	assertBlocker(t, report.Blockers, BlockerDevelopmentRootNotDir)
	assertBlocker(t, report.Blockers, BlockerLabRootNotDir)
}

func TestInspectLayoutRejectsUnsafeConfigurationAndDeduplicatesHistory(t *testing.T) {
	home := t.TempDir()
	installRoot := filepath.Join(home, "opt", "mcp-devbox")
	outside := t.TempDir()

	for name, config := range map[string]InventoryConfig{
		"relative home":        {HomeDir: "relative", InstallRoot: installRoot},
		"root home":            {HomeDir: string(os.PathSeparator), InstallRoot: installRoot},
		"relative install":     {HomeDir: home, InstallRoot: "relative"},
		"historical outside":   {HomeDir: home, InstallRoot: installRoot, HistoricalPaths: []string{outside}},
		"historical home root": {HomeDir: home, InstallRoot: installRoot, HistoricalPaths: []string{home}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectLayout(config); err == nil {
				t.Fatal("unsafe inventory configuration accepted")
			}
		})
	}

	candidate := filepath.Join(home, "p12")
	report, err := InspectLayout(InventoryConfig{
		HomeDir:         home,
		InstallRoot:     installRoot,
		HistoricalPaths: []string{candidate, candidate},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Historical) != 1 || report.Historical[0].Path != candidate {
		t.Fatalf("historical=%+v", report.Historical)
	}
}
