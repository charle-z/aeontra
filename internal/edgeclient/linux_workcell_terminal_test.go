//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRecordLinuxWorkcellTerminalStatePersistsBoundedCheckpoint(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	path := devRoot + "/terminal"
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "finish"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordLinuxWorkcellTerminalState(&prepared, "completed", "not-required"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(prepared.CurrentStatePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Runtime state: completed", "Container cleanup: not-required", "Active runtime process group: stopped", prepared.CurrentStatePath} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("terminal state missing %q: %s", expected, content)
		}
	}
}

func TestLinuxWorkcellContainerCleanupState(t *testing.T) {
	if state := LinuxWorkcellContainerCleanupState(nil, nil); state != "not-required" {
		t.Fatalf("state=%q", state)
	}
	preparation := &LinuxWorkcellPreparation{RootlessContainer: &RootlessContainerEndpoint{Engine: "docker"}}
	if state := LinuxWorkcellContainerCleanupState(preparation, nil); !strings.HasPrefix(state, "complete:") {
		t.Fatalf("state=%q", state)
	}
	if state := LinuxWorkcellContainerCleanupState(preparation, context.Canceled); !strings.HasPrefix(state, "pending:") {
		t.Fatalf("state=%q", state)
	}
}
