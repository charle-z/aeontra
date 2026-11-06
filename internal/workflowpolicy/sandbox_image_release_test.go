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
		"docker/setup-buildx-action@37fe631027851001ddb9b187196cc803df7f5f0e",
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
		`MCP_DEVBOX_SANDBOX_RUNNER_ADDR: "${MCP_DEVBOX_SANDBOX_RUNNER_IPV4:?set one private network address}:8770"`,
		"MCP_DEVBOX_SANDBOX_RUNNER_IPV4:?set one private network address",
		"user: \"10001:10001\"",
		"read_only: true",
		"no-new-privileges:true",
		"cap_drop:",
		"- ALL",
		"source: /run/user/10001/podman/podman.sock",
		"target: /run/user/10001/podman/podman.sock",
		"create_host_path: false",
		"type: bind",
		"source: ${MCP_DEVBOX_SANDBOX_RUNNER_WORKSPACE_SOURCE:?set the backend repository storage mountpoint}",
		"target: /srv/aeontra-l3/workspace",
		"propagation: rprivate",
		"/srv/aeontra-l3/state:/srv/aeontra-l3/state",
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
		"MCP_DEVBOX_SANDBOX_RUNNER_ADDR: mcp-sandbox-runner:8770",
		"MCP_DEVBOX_SANDBOX_RUNNER_ADDR: 0.0.0.0:8770",
		"/srv/aeontra-l3/workspace:/srv/aeontra-l3/workspace",
		"MCP_DEVBOX_SANDBOX_RUNNER_WORKSPACE_VOLUME",
		"repositories:/srv/aeontra-l3/workspace",
		"volumes:\n  repositories:",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("sandbox runner compose contains %q", forbidden)
		}
	}
}

func TestSandboxImageReleaseGatesExactRegistryImagesBeforePromotion(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/sandbox-image-release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		`docker buildx imagetools inspect "$ref" --raw`,
		`docker pull --platform linux/amd64 "$manifest_ref"`,
		`test "$actual_id" = "$config_digest"`,
		`test "$actual_revision" = "$revision"`,
		`test "$actual_platform" = "linux/amd64"`,
		`GRYPE_DB_REQUIRE_UPDATE_CHECK: "true"`,
		`GRYPE_DB_VALIDATE_BY_HASH_ON_START: "true"`,
		`GRYPE_DB_MAX_ALLOWED_BUILT_AGE: 24h`,
		`"$GRYPE" "docker:$scan_ref" -o json --file "$report"`,
		`.source.target.imageID == $id`,
		`.descriptor.db | type == "object" and length > 0`,
		`go run ./cmd/grype-gate --report "$report" --minimum high --annotation-file "$recipe"`,
		`candidate_tag="candidate-$revision-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"`,
		`docker buildx imagetools create --tag "$tag" "$ref"`,
		`test "$(docker buildx imagetools inspect "$tag" --format '{{.Manifest.Digest}}')" = "${ref##*@}"`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("sandbox release is missing %q", required)
		}
	}
	if strings.Contains(text, "--accepted-risk") {
		t.Error("the release gate must not accept historical risk exceptions")
	}
	if strings.Contains(text, `--tag "$runner_image:sha-`) || strings.Contains(text, `--tag "$workcell_image:sha-`) {
		t.Error("unscanned builds must not get release tags")
	}
	ordered := []string{
		"- name: Build and publish candidate images",
		"- name: Verify candidate image identities",
		"- name: Scan exact candidate images",
		"- name: Smoke test the scanned workcell",
		"- name: Promote only the scanned digests",
		"- name: Record scanned images for promotion",
		"- name: Retain immutable image identities",
	}
	previous := -1
	for _, step := range ordered {
		position := strings.Index(text, step)
		if position <= previous || position < 0 {
			t.Fatalf("release step is missing or out of order: %s", step)
		}
		previous = position
	}
}

func TestSandboxRunnerSocketMountCannotCreateHostDirectory(t *testing.T) {
	body, err := os.ReadFile("../../deploy/sandbox-runner-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(body, &compose); err != nil {
		t.Fatal(err)
	}
	const socket = "/run/user/10001/podman/podman.sock"
	for _, volume := range compose.Services["mcp-sandbox-runner"].Volumes {
		mount, ok := volume.(map[string]any)
		if !ok || mount["target"] != socket {
			continue
		}
		bind, ok := mount["bind"].(map[string]any)
		if !ok || mount["type"] != "bind" || mount["source"] != socket || bind["create_host_path"] != false {
			t.Fatal("socket must be an exact pre-existing bind, without directory autocreation")
		}
		return
	}
	t.Fatal("missing long-syntax socket bind; short syntax creates a directory when the socket is absent")
}
