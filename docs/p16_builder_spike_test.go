package docs_test

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
		"exact three-entry SHA-256 manifest",
		"disposable CI evidence, not VPS calibration",
		"50/65/80 quota measurements",
		"final BuildKit-versus-Podman engine selection",
	} {
		if !containsNormalizedProse(document, required) {
			t.Fatalf("builder spike documentation lost prose %q", required)
		}
	}
	for _, literal := range []string{
		"buildkit-runc",
		"stage-official-v0.31.2.sh",
		"p16-builder-spike.yml",
		"calibrate-vps.sh",
	} {
		if !strings.Contains(document, literal) {
			t.Fatalf("builder spike documentation lost literal %q", literal)
		}
	}

	workflow := readP16BuilderDoc(t, "../.github/workflows/p16-builder-spike.yml")
	for _, required := range []string{
		"Rootless BuildKit candidate fixture",
		"stage-official-v0.31.2.sh",
		"Classify hosted-runner runc reproducibility",
		"Require explicit runc acceptance boundary",
		"runs-on: ubuntu-24.04",
		"FROM busybox:1.37.0",
		"RUN echo p16-runc-ok > /ok",
		"# syntax=docker/dockerfile:1.7",
		"p16-external-runc-ok",
		"integrated-second-build.log",
		"runc-reproducibility.tsv",
		"not-reproducible",
		"target VPS preflights remain mandatory",
		"unexpected systemd namespace filter in builder unit",
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
			t.Fatalf("builder spike workflow lost literal %q", required)
		}
	}
	for _, forbidden := range []string{"/var/run/docker.sock:/var/run/docker.sock", "--privileged", "continue-on-error", "FROM scratch\nCOPY hello /hello", "RestrictNamespaces=~cgroup ipc uts", "runs-on: ubuntu-22.04", "sysctl -w"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("builder spike workflow contains forbidden %q", forbidden)
		}
	}

	calibration := readP16BuilderDoc(t, "vps-builder-calibration.md")
	for _, required := range []string{
		"target-VPS rootless runtime preflight accepted",
		"six-run 50/65/80 quota baseline remains validation pending",
		"Step 8 must not begin",
		"work survives an SSH disconnect",
		"existing different or partial installation fails closed",
		"supports only Debian or Ubuntu",
		"fixed host prerequisite packages",
		"integrated Dockerfile frontend preflight",
		"external Dockerfile frontend preflight",
	} {
		if !containsNormalizedProse(calibration, required) {
			t.Fatalf("VPS calibration documentation lost prose %q", required)
		}
	}
	for _, literal := range []string{
		"50/65/80 percent calibration",
		"`cpu.max`",
		"30-minute hard timeout",
		"HTTP 502",
		"`CPUQuota=65%`",
		"bootstrap-vps.sh",
		"mcp-devbox-builder-bootstrap.service",
		"`rootlesskit`",
		"`uidmap`",
		"`slirp4netns`",
		"`fuse-overlayfs`",
		"host-prerequisites.tsv",
		"`ProtectControlGroups=yes`",
		"`Delegate=yes`",
		"ProtectKernelTunables",
		"ProtectKernelModules",
		"ProtectHostname",
	} {
		if !strings.Contains(calibration, literal) {
			t.Fatalf("VPS calibration documentation lost literal %q", literal)
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
