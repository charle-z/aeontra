package workflowpolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryWorkflowsSatisfyPolicy(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no GitHub Actions workflows found")
	}
	for _, path := range matches {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(filepath.Base(path), content); err != nil {
				t.Fatal(err)
			}
		})
	}
}
