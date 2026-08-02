package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectGitSyncToolsExposeOnlyClosedProjectScopedInputs(t *testing.T) {
	server := stampServer(t)
	for _, name := range []string{"project_git_status", "project_git_fetch", "project_git_fast_forward_preview", "project_git_fast_forward"} {
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
		for _, forbidden := range []string{`"url"`, `"remote"`, `"refspec"`, `"force"`, `"tags"`, `"path"`} {
			if containsAll(text, forbidden) {
				t.Fatalf("%s exposed %s", name, forbidden)
			}
		}
	}
	status := server.table["project_git_status"].def.Annotations
	if status["readOnlyHint"] != true || status["destructiveHint"] != false {
		t.Fatalf("status annotations=%v", status)
	}
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}
