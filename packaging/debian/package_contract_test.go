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
		"mcp-autopilot-worker", "model-turn-driver", "opencode-provider/htb-actions.js", "opencode-provider/dev-actions.js",
		"libexec/node", "libexec/gh", "gh", "golang-go", "chromium", "catatonit", "podman", "util-linux",
		"mcp-bundle-updater", "mcp-devbox-bundle-updater.service",
		"mcp-devbox-edge-onboard@.path",
		"autopilot-model.json", "DEBIAN/conffiles",
		"mcp-devbox-bundle-rollback.service", "mcp-devbox-edge-repair.service", "49-mcp-devbox-updater.rules.in", "policykit-1 | polkitd",
		"manifest.json", "manifest.sig", "/usr/share/doc/mcp-devbox",
	} {
		if !strings.Contains(build, required) {
			t.Fatalf("package builder missing %q", required)
		}
	}
	for _, required := range []string{
		"if [ \"$HAS_OPENCODE\" -eq 1 ]; then\n  install -d -m 0755 \"$RELEASE_ROOT/opencode\" \"$RELEASE_ROOT/opencode-provider\"",
		"if [ \"$HAS_CODEX\" -eq 1 ]; then\n  install -d -m 0755 \"$RELEASE_ROOT/codex\"",
	} {
		if !strings.Contains(build, required) {
			t.Fatalf("package builder must create optional component directories conditionally: %q", required)
		}
	}
	if strings.Contains(build, "\"$RELEASE_ROOT/opencode\" \"$RELEASE_ROOT/opencode-provider\" \"$RELEASE_ROOT/codex\" \"$RELEASE_ROOT/systemd\"") {
		t.Fatal("package builder must not materialize absent optional component directories")
	}

	postinst := repoFile(t, "packaging/debian/postinst.in")
	for _, required := range []string{
		"mv -Tf", "/opt/mcp-devbox", "/usr/local/bin/mcp-edge", "/usr/local/bin/gh",
		"mcp-devbox-edge.service mcp-devbox-opencode-edge.service", "systemctl disable --now \"$LEGACY_UNIT\"",
		"/usr/local/libexec/mcp-devbox/mcp-autopilot-worker", "systemctl enable",
		"mcp-edge bundle verify", "rollback_install", "onboarding-preflight",
		"edge_lifecycle recover-state", "edge_lifecycle prepare-state-migration",
		"edge_lifecycle finalize-state-migration", "edge_lifecycle rollback-state-migration",
		"49-mcp-devbox-updater.rules", "@EDGE_USER@",
		"loginctl enable-linger", "podman.socket",
		"stage_legacy_directory", "restore_legacy_directory",
		"LEGACY_PROVIDER_BACKUP", "LEGACY_OPENCODE_BACKUP",
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
	if strings.Contains(postinst, "id charles") || strings.Contains(postinst, "EDGE_USER='charles'") {
		t.Fatal("postinst must not select the maintainer's local account")
	}

	prerm := repoFile(t, "packaging/debian/prerm")
	for _, required := range []string{
		"mcp-devbox-edge-onboard@${EDGE_USER}.path",
		"for EDGE_UNIT_BASE in mcp-devbox-edge mcp-devbox-opencode-edge",
		"EDGE_UNIT=\"${EDGE_UNIT_BASE}@${EDGE_USER}.service\"",
		"mcp-devbox-edge-onboard@${EDGE_USER}.path.d/10-edge-home.conf",
		"${EDGE_UNIT_BASE}@${EDGE_USER}.service.d/10-edge-home.conf",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(prerm, required) {
			t.Fatalf("prerm missing safe lifecycle cleanup %q", required)
		}
	}
	if strings.Contains(prerm, "systemctl disable --now \"mcp-devbox-edge-onboard@${EDGE_USER}.path\" || true") || strings.HasPrefix(strings.TrimSpace(prerm), "rm -f /etc/polkit") {
		t.Fatal("prerm must fail closed before removing updater authority")
	}

	updaterUnit := repoFile(t, "packaging/systemd/mcp-devbox-bundle-updater.service")
	for _, required := range []string{"User=root", "update stable", "ProtectSystem=strict", "ReadWritePaths=/opt/mcp-devbox /etc/systemd/system /usr/local/bin", "CapabilityBoundingSet="} {
		if !strings.Contains(updaterUnit, required) {
			t.Fatalf("updater unit missing %q", required)
		}
	}
	rollbackUnit := repoFile(t, "packaging/systemd/mcp-devbox-bundle-rollback.service")
	for _, required := range []string{"rollback", "ProtectSystem=strict", "ReadWritePaths=/opt/mcp-devbox /etc/systemd/system /usr/local/bin", "CapabilityBoundingSet="} {
		if !strings.Contains(rollbackUnit, required) {
			t.Fatalf("rollback unit missing %q", required)
		}
	}
	for _, forbidden := range []string{"/bin/sh", "/bin/bash", "sudo", "ExecStart=/usr/bin/env"} {
		if strings.Contains(updaterUnit, forbidden) {
			t.Fatalf("updater unit contains open-ended authority %q", forbidden)
		}
	}
}

func TestPrivilegedUpdaterAuthorityIsLimitedToFixedUnits(t *testing.T) {
	rule := repoFile(t, "packaging/polkit/49-mcp-devbox-updater.rules.in")
	for _, required := range []string{"org.freedesktop.systemd1.manage-units", "@EDGE_USER@", "verb == \"start\"", "mcp-devbox-bundle-updater.service", "mcp-devbox-bundle-rollback.service", "mcp-devbox-edge-repair.service"} {
		if !strings.Contains(rule, required) {
			t.Fatalf("polkit rule missing %q", required)
		}
	}
	for _, forbidden := range []string{"subject.active", "unix-process", "run_command", "/bin/sh", "sudo"} {
		if strings.Contains(rule, forbidden) {
			t.Fatalf("polkit rule exposes %q", forbidden)
		}
	}
	units := map[string]string{"packaging/systemd/mcp-devbox-bundle-updater.service": "update stable", "packaging/systemd/mcp-devbox-bundle-rollback.service": "rollback", "packaging/systemd/mcp-devbox-edge-repair.service": "repair"}
	for path, operation := range units {
		unit := repoFile(t, path)
		if !strings.Contains(unit, "mcp-bundle-updater "+operation) || strings.Contains(unit, "%i") || strings.Contains(unit, "EnvironmentFile") {
			t.Fatalf("unsafe fixed updater unit %s", path)
		}
	}
}

func TestEdgeReleaseAutomationBuildsOneClosedSignedArtifactSet(t *testing.T) {
	stage := repoFile(t, "packaging/parrot/stage-edge-bundle.sh")
	for _, required := range []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "mcp-autopilot-worker", "mcp-bundle-updater", "mcp-bundle-manifest", "EdgeBundlePublicKey", "--gh-bin", "libexec/gh", "--manifest-version", "mcp-devbox-opencode-edge-bridge@.service", "mcp-devbox-edge@.service", "mcp-devbox-edge-onboard@.path", "codex/codex", "codex/pin.json"} {
		if !strings.Contains(stage, required) {
			t.Fatalf("bundle staging missing %q", required)
		}
	}
	pinnedCodex := repoFile(t, "packaging/codex/stage-pinned.sh")
	for _, required := range []string{"rust-v0.147.0", "codex-x86_64-unknown-linux-musl.tar.gz", "0246e2e773834e07f0fb5249ed6ebad12e4591e608f8c7bb97dd6a9690544c36", "cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40", "https://github.com/openai/codex/releases/download/$TAG/$ASSET", "sha256sum --check", "codex-cli 0.147.0"} {
		if !strings.Contains(pinnedCodex, required) {
			t.Fatalf("pinned Codex acquisition missing %q", required)
		}
	}
	pinnedGH := repoFile(t, "packaging/github-cli/stage-pinned.sh")
	for _, required := range []string{"v2.97.0", "gh_2.97.0_linux_amd64.tar.gz", "a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112", "https://github.com/cli/cli/releases/download/$TAG/$ASSET", "--proto '=https'", "sha256sum --check", "gh version 2.97.0"} {
		if !strings.Contains(pinnedGH, required) {
			t.Fatalf("pinned GitHub CLI acquisition missing %q", required)
		}
	}
	for _, forbidden := range []string{"curl ", "wget ", "eval ", "bash -c", "go get"} {
		if strings.Contains(stage, forbidden) {
			t.Fatalf("bundle staging contains open-ended acquisition %q", forbidden)
		}
	}
	release := repoFile(t, ".github/workflows/edge-release.yml")
	for _, required := range []string{"Publish signed Aeontra Edge release", "p15.x.y bridge or vMAJOR.MINOR.PATCH", "workflow_dispatch", "environment: edge-release", "EDGE_BUNDLE_ED25519_PRIVATE_KEY_B64", "EDGE_DEB_GPG_PRIVATE_KEY_B64", "stage-edge-bundle.sh", "build-edge-release.sh", "build-edge-deb.sh", "Generate Edge bundle SBOM", "gh release create", "gh release upload stable --clobber", "bridge-v3", "codex-v4", "codex-v5", "packaging/codex/stage-pinned.sh"} {
		if !strings.Contains(release, required) {
			t.Fatalf("release workflow missing %q", required)
		}
	}
	packageBuilder := repoFile(t, "packaging/debian/build-edge-deb.sh")
	for _, required := range []string{"p15.x.y|vMAJOR.MINOR.PATCH", `^v(0|[1-9][0-9]*)`, `PACKAGE_VERSION="${RELEASE#v}"`} {
		if !strings.Contains(packageBuilder, required) {
			t.Fatalf("Edge package builder missing version transition contract %q", required)
		}
	}
	evidence := repoFile(t, ".github/workflows/p15-edge.yml")
	for _, required := range []string{"Reproducible signed Debian package", "cmp ", "dpkg --force-depends -i", "mcp-edge bundle verify", "edge-state-fixture", "cmp /fixtures/legacy-state/identity.json", "p14-provider", "test ! -e /opt/mcp-devbox/opencode-provider", "test ! -e /opt/mcp-devbox/opencode-1.18.1", "--manifest-version 5"} {
		if !strings.Contains(evidence, required) {
			t.Fatalf("P15 evidence workflow missing %q", required)
		}
	}
}
