//go:build windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsProjectRegistryCreatesAndReopens(t *testing.T) {
	state, workspaces, _ := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	config := ProjectRegistryConfig{
		StateRoot:    state,
		AllowedOwner: "charle-z",
		Workspaces:   workspaces,
		Inspector:    fixedProjectInspector{state: ProjectCheckoutReady},
	}
	registry, err := OpenProjectRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(state, projectRegistryFile)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("project registry was not retained: info=%v err=%v", info, err)
	}

	registry, err = OpenProjectRegistry(config)
	if err != nil {
		t.Fatalf("reopen project registry: %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
}
