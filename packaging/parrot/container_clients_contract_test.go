package parrot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionSevenArchiveIncludesBothSignedContainerClients(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GNU tar packaging acceptance requires Linux")
	}
	root := t.TempDir()
	bundle := filepath.Join(root, "bundle")
	for _, relative := range []string{
		"manifest.json", "manifest.sig", "bin/mcp-edge", "libexec/gh", "libexec/mcp-autopilot-worker",
		"libexec/mcp-bundle-updater", "codex/codex", "codex/pin.json", "systemd/mcp-devbox-edge@.service",
		"systemd/mcp-devbox-edge-onboard@.path", "codex/container-tools/bin/docker",
		"codex/container-tools/config/cli-plugins/docker-buildx",
	} {
		path := filepath.Join(bundle, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(root, "output")
	script := filepath.Join(repositoryRoot(t), "packaging", "parrot", "build-edge-release.sh")
	command := exec.Command("bash", script, "--bundle", bundle, "--output", output, "--release", "v1.2.46", "--architecture", "amd64")
	command.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("archive builder failed: %v: %s", err, result)
	}
	archive := filepath.Join(output, "v1.2.46", "mcp-devbox-edge_v1.2.46_amd64.tar.gz")
	result, err := exec.Command("tar", "-tzf", archive).CombinedOutput()
	if err != nil {
		t.Fatalf("archive listing failed: %v: %s", err, result)
	}
	for _, relative := range []string{"codex/container-tools/bin/docker", "codex/container-tools/config/cli-plugins/docker-buildx"} {
		if !strings.Contains(string(result), relative+"\n") {
			t.Fatalf("signed archive omits %s: %s", relative, result)
		}
	}
	if err := os.Remove(filepath.Join(bundle, "codex", "container-tools", "config", "cli-plugins", "docker-buildx")); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("bash", script, "--bundle", bundle, "--output", filepath.Join(root, "incomplete"), "--release", "v1.2.46", "--architecture", "amd64")
	command.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("incomplete signed container clients were accepted: %s", result)
	}
}
