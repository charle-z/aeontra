package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP11CandidateEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-15-p11.md")
	for _, required := range []string{
		"P11 release-candidate baseline",
		"2e6d017", "eff7eed", "cc9fb3f", "1a42bbe", "769a369", "7ce7439", "3a441e6",
		"tool_count=71",
		"sha256:7dfa9bb83c935c7df875740102dafa5572852e5e8cb6c064c89c1e3acb5e30ac",
		"tool_count=67",
		"sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed",
		"2 | 2052 | 260 | 18.63 ms",
		"1 | 406 | 0 | 16.66 ms",
		"128 MiB", "4 x 16 MiB", "4 x 32 MiB", "256 MiB",
		"24 hours", "7 days", "90 days",
		"one-use pairing", "Ed25519", "anti-replay", "Bubblewrap",
		"No remote shell", "No Docker socket", "No Windows mounts", "No remote sudo",
		"Rollback",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P11 baseline does not contain %q", required)
		}
	}

	review := read("security-reports/2026-07-15-p11-edge-review.md")
	for _, required := range []string{
		"P11 Edge final security review", "/edge/v1/pair", "nonce", "revocation",
		"idempotency", "redacted before persistence", "RestrictNamespaces=false",
		"No critical or high-severity finding",
	} {
		if !strings.Contains(review, required) {
			t.Errorf("P11 security review does not contain %q", required)
		}
	}

	if !strings.Contains(read("documentation-map.md"), "2026-07-15-p11.md") {
		t.Error("documentation map does not identify P11 candidate evidence")
	}
	if !strings.Contains(read("product-roadmap.md"), "| P11 bounded state and development Edge | Release candidate |") {
		t.Error("roadmap does not identify the P11 release candidate")
	}
	if !strings.Contains(read("design.md"), "current 71-tool P11 candidate") {
		t.Error("design does not identify the current P11 catalog")
	}

	systemd := read("../packaging/systemd/mcp-devbox-edge.service")
	for _, required := range []string{
		"User=mcpedge", "NoNewPrivileges=true", "CapabilityBoundingSet=",
		"AmbientCapabilities=", "PrivateDevices=true", "ProtectSystem=strict",
		"RestrictNamespaces=false", "ReadWritePaths=/var/lib/mcp-devbox-edge /srv/mcp-devbox-workspaces",
	} {
		if !strings.Contains(systemd, required) {
			t.Errorf("P11 systemd unit does not contain %q", required)
		}
	}

	for _, historical := range []string{"baselines/2026-07-14-p9.md", "baselines/2026-07-14-p8_1.md", "baselines/2026-07-14-p8_1-production.md"} {
		content := read(historical)
		if !strings.Contains(content, "tool_count=67") || !strings.Contains(content, "sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed") {
			t.Errorf("historical 67-tool baseline changed contract: %s", historical)
		}
	}
}
