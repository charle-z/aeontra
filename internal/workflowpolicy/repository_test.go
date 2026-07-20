package workflowpolicy

import (
	"errors"
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

func TestContentsWriteIsLimitedToProtectedManualEdgeRelease(t *testing.T) {
	allowed, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "edge-release.yml"))
	if err != nil || Validate("edge-release.yml", allowed) != nil {
		t.Fatalf("protected release rejected: read=%v validate=%v", err, Validate("edge-release.yml", allowed))
	}
	unsafe := []byte("name: unsafe\non: workflow_dispatch\npermissions:\n  contents: read\njobs:\n  publish:\n    timeout-minutes: 10\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n    steps:\n      - run: echo unsafe\n")
	if err := Validate("other.yml", unsafe); !errors.Is(err, ErrForbiddenPermission) {
		t.Fatalf("unprotected contents write accepted: %v", err)
	}
}
