package workflowpolicy

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSandboxImageReleaseKeepsPublicationProtectedAndImmutable(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/sandbox-image-release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("sandbox image release workflow is invalid YAML: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"workflow_dispatch:",
		"environment: sandbox-image-release",
		"contents: read",
		"packages: write",
		"test \"$GITHUB_REPOSITORY\" = \"charle-z/aeontra\"",
		"test \"$(git rev-parse HEAD)\" = \"$(git rev-parse origin/main)\"",
		"actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"go-version-file: go.mod",
		"docker/setup-buildx-action@8d2750c68a42422c14e847fe6c8ac0403b4cbd6f",
		"driver: docker-container",
		"Dockerfile.sandbox-runner",
		"Dockerfile.sandbox-workcell",
		"--provenance=mode=max",
		"--sbom=true",
		"sha-$revision",
		"docker buildx imagetools inspect",
		"actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("sandbox image release workflow missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"pull_request:",
		"pull_request_target:",
		":latest",
		"continue-on-error",
		"secrets.",
		"/var/run/docker.sock",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("sandbox image release workflow contains %q", forbidden)
		}
	}
}

func TestSandboxRunnerComposeKeepsEngineAuthorityPrivate(t *testing.T) {
	content, err := os.ReadFile("../../deploy/sandbox-runner-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("sandbox runner compose is invalid YAML: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"MCP_DEVBOX_SANDBOX_RUNNER_IMAGE:?set an immutable runner image digest",
		"user: \"10001:10001\"",
		"read_only: true",
		"no-new-privileges:true",
		"cap_drop:",
		"- ALL",
		"/run/user/10001/podman/podman.sock:/run/user/10001/podman/podman.sock",
		"/srv/aeontra-l3/workspace:/srv/aeontra-l3/workspace",
		"/srv/aeontra-l3/state:/srv/aeontra-l3/state",
		"external: true",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("sandbox runner compose missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"ports:",
		"/var/run/docker.sock",
		"privileged:",
		"network_mode: host",
		"pid: host",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("sandbox runner compose contains %q", forbidden)
		}
	}
}
