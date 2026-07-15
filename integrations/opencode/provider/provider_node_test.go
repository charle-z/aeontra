package provider_test

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestNodeProviderContract(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required for the pinned OpenCode provider tests: %v", err)
	}
	command := exec.Command(node, "--test", "provider.test.mjs")
	command.Dir = "."
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("provider Node tests failed: %v\n%s", err, output.String())
	}
}
