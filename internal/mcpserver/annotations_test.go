package mcpserver

import "testing"

func TestToolAnnotations_ReadOnlyHints(t *testing.T) {
	s := stampServer(t)
	byName := map[string]toolDef{}
	for _, d := range s.listTools() {
		byName[d.Name] = d
	}

	// Read-only tools must advertise readOnlyHint:true so a client (e.g. ChatGPT) does
	// not treat them as consequential and block/gate them.
	for _, n := range []string{
		"build_context_pack", "list_dir", "read_file", "read_many_files",
		"search_code", "git_status", "git_diff", "memory_read", "sandbox_status",
	} {
		a := byName[n].Annotations
		if a == nil || a["readOnlyHint"] != true {
			t.Errorf("%s must be annotated readOnlyHint:true, got %v", n, a)
		}
	}

	// Side-effecting tools must NOT claim to be read-only (honest labeling; the client
	// should still gate/confirm them).
	for _, n := range []string{
		"git_push", "git_commit", "run_command", "sandbox_exec", "coolify_deploy",
		"apply_patch", "create_file",
	} {
		a := byName[n].Annotations
		if a != nil && a["readOnlyHint"] == true {
			t.Errorf("%s must NOT be marked read-only", n)
		}
	}
}
