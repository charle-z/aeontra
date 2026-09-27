package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestSourcePublicationUsesGitObjectsNotRedactedToolOutput(t *testing.T) {
	for path, required := range map[string][]string{
		"../AGENTS.md": {"redacted tool output", "project_git_publish"},
		"security.md":  {"source-looking text", "Git objects"},
		"tools.md":     {"redacted command output", "Git objects"},
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range required {
			if !strings.Contains(string(content), phrase) {
				t.Errorf("%s does not state publication invariant %q", path, phrase)
			}
		}
	}
}
