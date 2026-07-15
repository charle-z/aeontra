package edgeclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRegistryAddListResolveRemove(t *testing.T) {
	state := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry, err := OpenWorkspaceRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	entry, created, err := registry.Add(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !workspaceIDPattern.MatchString(entry.ID) || entry.Path != workspace {
		t.Fatalf("entry=%+v created=%t", entry, created)
	}
	replayed, replayCreated, err := registry.Add(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if replayCreated || replayed.ID != entry.ID {
		t.Fatalf("duplicate registration: first=%+v replay=%+v", entry, replayed)
	}
	items, err := registry.List()
	if err != nil || len(items) != 1 || items[0].ID != entry.ID {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	resolved, err := registry.Resolve(entry.ID)
	if err != nil || resolved != workspace {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	if err := registry.Remove(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(entry.ID); err == nil {
		t.Fatal("removed workspace resolved")
	}
	info, err := os.Stat(filepath.Join(state, workspaceRegistryFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestWorkspaceRegistryRejectsUnsafePathsAndRevalidates(t *testing.T) {
	registry, err := OpenWorkspaceRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	for _, path := range []string{"relative", "/", "/mnt/c/project", "/mnt/d/project"} {
		if _, _, err := registry.Add(path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	entry, _, err := registry.Add(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workspace, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(entry.ID); err == nil {
		t.Fatal("workspace permission change was not detected")
	}
}

func TestWorkspaceRegistryRejectsSymlinkAndDoesNotAcceptCallerIDs(t *testing.T) {
	registry, err := OpenWorkspaceRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := registry.Add(link); err == nil {
		t.Fatal("symlink workspace accepted")
	}
	entry, _, err := registry.Add(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry.ID, filepath.Base(target)) || entry.ID == "ws_target" {
		t.Fatalf("workspace id is not opaque: %q", entry.ID)
	}
}
