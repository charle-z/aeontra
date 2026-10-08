package debian

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestP15EdgeWorkflowParsesAndPackageFixtureHasValidBash(t *testing.T) {
	content := repoFile(t, ".github/workflows/p15-edge.yml")
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("parse P15 Edge workflow YAML: %v", err)
	}
	runScript := findWorkflowStepRun(t, &document, "Exercise clean package and P14 state migration in isolation")
	if !strings.Contains(runScript, `\${1:-}`) {
		t.Fatal("systemctl fixture expands its positional argument while being created")
	}
	ownedConfigParent := `install -d -o edgeci -g edgeci -m 0700 /srv/edgeci/.config`
	ownedLegacyState := `install -d -o edgeci -g edgeci -m 0700 /srv/edgeci/.config/mcp-devbox-edge`
	if !strings.Contains(runScript, ownedConfigParent+"\n") || !strings.Contains(runScript, ownedLegacyState) || strings.Index(runScript, ownedConfigParent) >= strings.Index(runScript, ownedLegacyState) {
		t.Fatal("package migration fixture must create the legacy state parent as the Edge user before the state directory")
	}
	path := filepath.Join(t.TempDir(), "package-fixture.sh")
	if err := os.WriteFile(path, []byte(runScript), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("sh", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("package workflow shell syntax failed: %v: %s", err, output)
	}
}

func findWorkflowStepRun(t *testing.T, document *yaml.Node, wantedName string) string {
	t.Helper()
	if document == nil || len(document.Content) != 1 {
		t.Fatal("workflow YAML document is empty")
	}
	root := document.Content[0]
	jobs := mappingValue(root, "jobs")
	if jobs == nil {
		t.Fatal("workflow jobs are missing")
	}
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		job := jobs.Content[index+1]
		steps := mappingValue(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			name := mappingValue(step, "name")
			if name == nil || name.Value != wantedName {
				continue
			}
			run := mappingValue(step, "run")
			if run == nil || strings.TrimSpace(run.Value) == "" {
				t.Fatalf("workflow step %q has no run script", wantedName)
			}
			return run.Value
		}
	}
	t.Fatalf("workflow step %q not found", wantedName)
	return ""
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}
