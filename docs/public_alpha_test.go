package docs_test

import (
	"strings"
	"testing"
)

func TestPublicAlphaGuideProvidesOneBoundedFirstRun(t *testing.T) {
	guide := readDoc(t, "public-alpha.md")
	for _, required := range []string{
		"# Public alpha",
		"single operator",
		"Go 1.26",
		"go install github.com/charle-z/mcp-devbox/cmd/mcp-devbox@latest",
		"--mode read-only",
		"absolute path",
		"workspace_checkpoint",
		"system_runtime_info",
		"disposable repository",
		"GitHub Issues",
		"SECURITY.md",
		"No service-level agreement",
		"install-edge-linux.md",
	} {
		if !strings.Contains(strings.ToLower(guide), strings.ToLower(required)) {
			t.Errorf("public alpha guide missing %q", required)
		}
	}
}

func TestPublicEntryPointsLinkToAlphaGuide(t *testing.T) {
	readme := readDoc(t, "../README.md")
	docMap := readDoc(t, "documentation-map.md")
	for name, body := range map[string]string{"README": readme, "documentation map": docMap} {
		if !strings.Contains(body, "docs/public-alpha.md") {
			t.Errorf("%s does not link to docs/public-alpha.md", name)
		}
	}
}

func TestPublicAlphaReleaseAndFeedbackEntryPoints(t *testing.T) {
	release := readDoc(t, "../.github/workflows/edge-release.yml")
	for _, required := range []string{
		"Signed Aeontra Edge release",
		"Windows package",
		"Debian, Parrot, or WSL",
		"checksums, signatures, SBOMs",
		"docs/install-edge-windows.md",
		"docs/install-edge-linux.md",
	} {
		if !strings.Contains(release, required) {
			t.Errorf("release workflow notes missing %q", required)
		}
	}

	linuxInstall := readDoc(t, "install-edge-linux.md")
	for _, required := range []string{
		"# Linux Edge installation",
		"amd64",
		"sudo apt install ./mcp-devbox-edge_",
		"mcp-devbox edge pairing-create",
		"mcp-edge onboard --server",
		"mcp-edge doctor",
		"systemctl is-active",
		"edge_bundle_update",
		"edge_bundle_rollback",
	} {
		if !strings.Contains(linuxInstall, required) {
			t.Errorf("Linux install guide missing %q", required)
		}
	}

	feedback := readDoc(t, "../.github/ISSUE_TEMPLATE/alpha_feedback.yml")
	for _, required := range []string{"Public alpha feedback", "What did you try?", "Where did you get stuck?", "redacted"} {
		if !strings.Contains(feedback, required) {
			t.Errorf("alpha feedback form missing %q", required)
		}
	}

	dockerfile := readDoc(t, "../Dockerfile")
	for _, required := range []string{
		`org.opencontainers.image.title="Aeontra"`,
		`org.opencontainers.image.description="Scoped, auditable MCP operations for software development"`,
		`org.opencontainers.image.source="https://github.com/charle-z/aeontra"`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("container metadata missing %q", required)
		}
	}
}
