package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestProjectExecAcceptsAnExplicitManagedDirectWorkspace(t *testing.T) {
	store := &projectExecToolStore{}
	server := New(nil).WithEdgeStore(store)
	_, err := server.table["project_exec"].handler(json.RawMessage(`{"alias":"project","target":"parrot","worktree_id":"wt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","idempotency_key":"direct-workspace-exec-1","argv":["pwd"],"timeout_seconds":10}`))
	if err != nil {
		t.Fatalf("direct MCP cannot select its isolated managed workspace: %v", err)
	}
	if store.createdRequest.WorktreeID != "wt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatal("workspace binding was lost")
	}
}

func TestManagedDirectWorkspaceLifecycleIsAvailableWithoutAModel(t *testing.T) {
	server := New(nil)
	for _, name := range []string{"project_workspace_prepare", "project_workspace_status", "project_workspace_cleanup"} {
		if _, ok := server.table[name]; !ok {
			t.Fatalf("missing direct workspace operation %s", name)
		}
	}
}
