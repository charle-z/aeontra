package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestRelayContainerRunsOpenCodeSlicesInSeparateProcesses(t *testing.T) {
	body, err := os.ReadFile("../../test/opencode-e2e/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"combined='-test.run=TestOpenCodeExternalModelVerticalSlice|TestRemoteOpenCodeDistributedRelay'",
		"-test.run='^TestOpenCodeExternalModelVerticalSlice$'",
		"-test.run='^TestRemoteOpenCodeDistributedRelay$'",
		"exec /usr/local/bin/mcp-devbox-opencode-e2e",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("relay container entrypoint missing %q", required)
		}
	}
	if strings.Contains(text, "retry") || strings.Contains(text, "sleep ") {
		t.Fatal("relay container isolation must not add retries or delay gates")
	}
	if strings.Index(text, "TestOpenCodeExternalModelVerticalSlice$") >= strings.Index(text, "TestRemoteOpenCodeDistributedRelay$") {
		t.Fatal("vertical slice must complete before the isolated relay process")
	}
}
