package debian

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoFile(t *testing.T, relative string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestDebianPackageBuildIsSignedReproducibleAndComplete(t *testing.T) {
	build := repoFile(t, "packaging/debian/build-edge-deb.sh")
	for _, required := range []string{
		"SOURCE_DATE_EPOCH", "dpkg-deb --root-owner-group", "gpg --batch", "sha256sum",
		"mcp-autopilot-worker", "model-turn-driver", "opencode-provider/htb-actions.js",
		"mcp-bundle-updater", "mcp-devbox-bundle-updater.service",
		"mcp-devbox-edge-onboard@.path",
		"manifest.json", "manifest.sig", "/usr/share/doc/mcp-devbox",
	} {
		if !strings.Contains(build, required) {
			t.Fatalf("package builder missing %q", required)
		}
	}

	postinst := repoFile(t, "packaging/debian/postinst.in")
	for _, required := range []string{
		"mv -Tf", "/opt/mcp-devbox", "/usr/local/bin/mcp-edge",
		"/usr/local/libexec/mcp-devbox/mcp-autopilot-worker", "systemctl enable",
		"mcp-edge bundle verify", "rollback_install", "onboarding-preflight",
		"LEGACY_STATE", "mv \"$LEGACY_STATE\" \"$PREFERRED_STATE\"",
	} {
		if !strings.Contains(postinst, required) {
			t.Fatalf("postinst missing %q", required)
		}
	}
	for _, forbidden := range []string{"rm -rf /home", "rm -rf /opt/mcp-devbox/releases", "curl |", "wget |"} {
		if strings.Contains(postinst, forbidden) {
			t.Fatalf("postinst contains forbidden operation %q", forbidden)
		}
	}

	updaterUnit := repoFile(t, "packaging/systemd/mcp-devbox-bundle-updater.service")
	for _, required := range []string{"User=root", "update stable", "ProtectSystem=strict", "ReadWritePaths=/opt/mcp-devbox /etc/systemd/system", "CapabilityBoundingSet="} {
		if !strings.Contains(updaterUnit, required) {
			t.Fatalf("updater unit missing %q", required)
		}
	}
	for _, forbidden := range []string{"/bin/sh", "/bin/bash", "sudo", "ExecStart=/usr/bin/env"} {
		if strings.Contains(updaterUnit, forbidden) {
			t.Fatalf("updater unit contains open-ended authority %q", forbidden)
		}
	}
}
