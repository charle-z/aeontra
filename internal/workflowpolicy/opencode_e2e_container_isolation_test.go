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

func TestContainerBuildsIncludeProfilesAndKeepFixtureSigningSeparate(t *testing.T) {
	production, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(production), "COPY profiles ./profiles") {
		t.Fatal("production Dockerfile does not copy the embedded workcell profiles")
	}

	fixture, err := os.ReadFile("../../test/opencode-e2e/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(fixture)
	stageLine := "stage-edge-bundle.sh --output /out/release"
	start := strings.Index(text, stageLine)
	if start < 0 {
		t.Fatal("OpenCode fixture does not stage an Edge bundle")
	}
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		end = len(text) - start
	}
	if strings.Contains(text[start:start+end], "--private-key") {
		t.Fatal("unsigned OpenCode fixture staging receives a signing key")
	}
	if !strings.Contains(text, "go run ./cmd/mcp-bundle-manifest") {
		t.Fatal("OpenCode fixture does not sign its staged manifest separately")
	}
	if strings.Contains(text, "rm /out/release/manifest.json /out/release/manifest.sig") {
		t.Fatal("OpenCode fixture assumes unsigned staging created a manifest")
	}
}
