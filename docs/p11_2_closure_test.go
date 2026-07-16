package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP112ReleaseCandidateEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-16-p11_2.md")
	for _, required := range []string{
		"P11.2 release-candidate baseline",
		"Remote OpenCode Model-Turn Relay over Edge",
		"ef2cb7eee4ecb67e5526fc1d055a482edd92877e",
		"b2f8d3b11c2bcbc08433f8ac9679aa1fe61ed6f5",
		"d3d57668504e1d38edfee7baf9f18379e37f205f",
		"2bda26d4382feca7b9367a068dfbeff917e5538d",
		"e8862ee9229ec8a98237251de6d3272e3f72ee1e",
		"tool_count=78",
		"sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed",
		"OpenCode: `1.18.1`",
		"Bubblewrap: `0.6.1`",
		"Project Node runtime: `22`",
		"29540554711",
		"29540554731",
		"29540554743",
		"29540553465",
		"relay_container_e2e",
		"bubblewrap_host_e2e",
		"combined_opencode_sandbox_e2e",
		"24.922154068 s",
		"4.128888126 s",
		"24.515693098 s",
		"6.617088 ms",
		"45.288636 ms",
		"read`, `grep`, `edit`, and `bash`",
		"zero duplicate turns",
		"runtime directory `0700` and Unix socket `0600`",
		"not GPT latency",
		"docs/install-opencode-edge-parrot.md",
		"No merge, deployment, pairing, real Parrot installation, tag, Coolify change",
		"Rollback",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P11.2 baseline does not contain %q", required)
		}
	}

	guide := read("install-opencode-edge-parrot.md")
	for _, required := range []string{
		"Parrot WSL2",
		"mcpedge",
		"Bubblewrap preflight",
		"OpenCode 1.18.1",
		"systemd",
		"pair",
		"heartbeat",
		"Kill switch",
		"Revocation",
		"Rollback",
		"Uninstall",
		"0600",
		"0700",
	} {
		if !strings.Contains(strings.ToLower(guide), strings.ToLower(required)) {
			t.Errorf("Parrot guide does not contain %q", required)
		}
	}

	if !strings.Contains(read("documentation-map.md"), "2026-07-16-p11_2.md") {
		t.Error("documentation map does not identify P11.2 evidence")
	}
	if !strings.Contains(read("product-roadmap.md"), "| P11.2 remote OpenCode relay | Release candidate |") {
		t.Error("roadmap does not identify P11.2 release candidate")
	}
	if !strings.Contains(read("design.md"), "current 78-tool P11.2 candidate") {
		t.Error("design does not identify the current P11.2 catalog")
	}

	for _, historical := range []string{
		"baselines/2026-07-15-p11.md",
		"baselines/2026-07-14-p8_1-production.md",
		"baselines/2026-07-14-p9.md",
	} {
		if strings.TrimSpace(read(historical)) == "" {
			t.Errorf("historical baseline became empty: %s", historical)
		}
	}
}
