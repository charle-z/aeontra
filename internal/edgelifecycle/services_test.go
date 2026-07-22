package edgelifecycle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectLayoutClassifiesKnownP12AndP15ServiceUnitsReadOnly(t *testing.T) {
	home := t.TempDir()
	systemdRoot := filepath.Join(home, "systemd")
	if err := os.MkdirAll(systemdRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(systemdRoot, "mcp-devbox-edge.service"), "legacy")
	writeFixtureFile(t, filepath.Join(systemdRoot, "mcp-devbox-opencode-edge@.service"), "current")
	if err := os.Symlink("/dev/null", filepath.Join(systemdRoot, "mcp-devbox-edge-repair.service")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, systemdRoot)

	report, err := InspectLayout(InventoryConfig{
		HomeDir:     home,
		InstallRoot: filepath.Join(home, "opt", "mcp-devbox"),
		SystemdRoot: systemdRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SystemdRoot.Kind != PathDirectory || len(report.Services) != len(knownEdgeServiceUnits) {
		t.Fatalf("systemd=%+v services=%+v", report.SystemdRoot, report.Services)
	}
	got := make([]ServiceStatus, len(report.Services))
	copy(got, report.Services)
	want := []ServiceStatus{
		{Name: "mcp-devbox-edge.service", Kind: PathFile},
		{Name: "mcp-devbox-opencode-edge.service", Kind: PathMissing},
		{Name: "mcp-devbox-opencode-edge@.service", Kind: PathFile},
		{Name: "mcp-devbox-edge-onboard@.path", Kind: PathMissing},
		{Name: "mcp-devbox-edge-repair.service", Kind: PathSymlink},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("services=%+v want=%+v", got, want)
	}
	if after := snapshotTree(t, systemdRoot); !reflect.DeepEqual(before, after) {
		t.Fatalf("service inventory mutated systemd fixture\nbefore=%v\nafter=%v", before, after)
	}
}

func TestInspectLayoutBlocksUnsafeSystemdRoot(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		home := t.TempDir()
		outside := t.TempDir()
		root := filepath.Join(home, "systemd")
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
		report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox"), SystemdRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocker(t, report.Blockers, BlockerSystemdRootSymlink)
		if len(report.Services) != 0 {
			t.Fatalf("services inspected through unsafe systemd root: %+v", report.Services)
		}
	})

	t.Run("file", func(t *testing.T) {
		home := t.TempDir()
		root := filepath.Join(home, "systemd")
		writeFixtureFile(t, root, "not-directory")
		report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox"), SystemdRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocker(t, report.Blockers, BlockerSystemdRootNotDirectory)
	})

	t.Run("relative", func(t *testing.T) {
		home := t.TempDir()
		if _, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox"), SystemdRoot: "relative"}); err == nil {
			t.Fatal("relative systemd root accepted")
		}
	})
}

func TestInspectLayoutWithoutSystemdRootPreservesHistoricalBehavior(t *testing.T) {
	home := t.TempDir()
	report, err := InspectLayout(InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")})
	if err != nil {
		t.Fatal(err)
	}
	if report.SystemdRoot != (PathStatus{}) || report.Services != nil {
		t.Fatalf("unexpected systemd inventory: root=%+v services=%+v", report.SystemdRoot, report.Services)
	}
}
