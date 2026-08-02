package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestProjectToolboxToolsExposeClosedProjectScopedInputs(t *testing.T) {
	server := stampServer(t)
	for _, name := range []string{"project_toolbox_create", "project_toolbox_status", "project_toolbox_exec", "project_toolbox_install", "project_toolbox_cleanup"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		encoded, err := json.Marshal(entry.def.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		if !containsAll(text, `"additionalProperties":false`, `"alias"`, `"target"`) {
			t.Fatalf("%s schema=%s", name, text)
		}
		for _, forbidden := range []string{`"image"`, `"socket"`, `"container"`, `"path"`, `"url"`, `"privileged"`, `"volume"`} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s exposed %s", name, forbidden)
			}
		}
	}
	status := server.table["project_toolbox_status"].def.Annotations
	if status["readOnlyHint"] != true || status["destructiveHint"] != false {
		t.Fatalf("status annotations=%v", status)
	}
}

func TestProjectToolboxHandlerQueuesExplicitOperationAndFiltersInternalState(t *testing.T) {
	store := &projectGitSyncToolStore{waitResult: edge.Operation{
		State: edge.OperationSucceeded,
		Result: edge.OperationResult{
			ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
			ToolboxID: "tb_11111111111111111111111111111111", ToolboxState: "running", ToolboxBase: "debian-bookworm-slim",
			ToolboxBaseImageID: "sha256:" + strings.Repeat("a", 64), ToolboxCreatedAt: "2026-08-02T12:00:00Z", ToolboxUpdatedAt: "2026-08-02T12:01:00Z", ToolboxOutput: "ruby 3.3\n",
		},
	}}
	server := New(nil).WithEdgeStore(store)
	output, err := server.handleProjectToolbox(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"toolbox-exec-1","argv":["ruby","--version"],"cwd":"src","environment":{"CI":"true"},"timeout_seconds":60}`), edge.OperationProjectToolboxExec)
	if err != nil {
		t.Fatal(err)
	}
	if store.createdKind != edge.OperationProjectToolboxExec || store.createdRequest.Profile != "linux-workcell" || store.createdRequest.Argv[0] != "ruby" || store.createdRequest.TimeoutSeconds != 60 {
		t.Fatalf("kind=%q request=%+v", store.createdKind, store.createdRequest)
	}
	for _, required := range []string{`"operation_state":"succeeded"`, `"repository":"charle-z/repo"`, `"toolbox_id":"tb_11111111111111111111111111111111"`, `"base":"debian-bookworm-slim"`, `"output":"ruby 3.3\n"`} {
		if !strings.Contains(output, required) {
			t.Fatalf("output missing %q: %s", required, output)
		}
	}
	for _, forbidden := range []string{"device_id", "workspace_id", "container", "socket", "/private", "environment", "argv"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output exposed %q: %s", forbidden, output)
		}
	}
}
