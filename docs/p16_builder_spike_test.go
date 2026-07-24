package docs

import (
	"os"
	"strings"
	"testing"
)

func TestP16BuilderSpikeContractRemainsDiscoverable(t *testing.T) {
	document := readP16BuilderDoc(t, "buildkit-spike-harness.md")
	for _, required := range []string{
		"private Step 7 acceptance harness",
		"delegated service cgroup subtree",
		"buildkit-runc",
		"exact three-entry SHA-256 manifest",
		"stage-official-v0.31.2.sh",
		"p16-builder-spike.yml",
		"disposable CI evidence, not VPS calibration",
		"50/65/80 quota measurements",
		"final BuildKit-versus-Podman engine selection",
		"calibrate-vps.sh",
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("builder spike documentation lost %q", required)
		}
	}

	workflow := readP16BuilderDoc(t, "../.github/workflows/p16-builder-spike.yml")
	for _, required := range []string{
		"Rootless BuildKit candidate fixture",
		"stage-official-v0.31.2.sh",
		"Build the same commit twice and prove cache reuse",
		"Verify stop kills the complete service cgroup",
		"Verify conservative removal",
		"cache-usage.txt",
		"cache-policy.txt",
		"buildctl",
		"du -v",
		"maxUsedSpace = \"4GB\"",
		"1048576",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("builder spike workflow lost %q", required)
		}
	}
	for _, forbidden := range []string{"/var/run/docker.sock:/var/run/docker.sock", "--privileged", "continue-on-error"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("builder spike workflow contains forbidden %q", forbidden)
		}
	}

	calibration := readP16BuilderDoc(t, "vps-builder-calibration.md")
	for _, required := range []string{
		"closed Step 7 operator candidate",
		"50/65/80 percent calibration",
		"exact value from cgroup `cpu.max`",
		"30-minute hard timeout",
		"HTTP 502",
		"restores `CPUQuota=65%`",
		"real VPS execution and dated baseline are still validation pending",
		"Step 8 must not begin",
		"bootstrap-vps.sh",
		"mcp-devbox-builder-bootstrap.service",
		"work survives an SSH disconnect",
		"existing different or partial installation fails closed",
	} {
		if !strings.Contains(calibration, required) {
			t.Fatalf("VPS calibration documentation lost %q", required)
		}
	}
}

func readP16BuilderDoc(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
