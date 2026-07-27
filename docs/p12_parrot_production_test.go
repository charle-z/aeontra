package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP12ParrotProductionEvidenceAndOnboardingStaySynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(body)
	}

	baseline := read("baselines/2026-07-18-p12-parrot-production.md")
	for _, expected := range []string{
		"merged, deployed, paired, and validated end to end",
		"3946fd7033f28906deb932298387034e2fa27fe8",
		"mr_829f6601fca6f887bc2d0133a4c5dff1",
		"ws_7c4686f5d9244bbad30ae705d4b660c5",
		"last_sequence=6",
		"AF_NETLINK",
		"bubblewrap_netlink_route_denied",
		"/mnt/wsl/resolv.conf",
		"shared host network",
	} {
		if !strings.Contains(strings.ToLower(baseline), strings.ToLower(expected)) {
			t.Errorf("P12 Parrot production baseline missing %q", expected)
		}
	}

	guide := read("install-opencode-edge-parrot.md")
	for _, expected := range []string{
		"node --test provider.test.mjs",
		"packaging/parrot/onboarding-preflight.sh",
		"mcp-devbox-opencode-edge@.service",
		"AF_NETLINK",
		"pair through stdin",
		"first remote smoke",
		"?? .mcp-devbox/",
		"repeat a completed objective",
	} {
		if !strings.Contains(strings.ToLower(guide), strings.ToLower(expected)) {
			t.Errorf("Parrot onboarding guide missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"cd /tmp/mcp-devbox-p11.2/integrations/opencode/provider\nnpm ci",
		"--socket-root /run/mcp-devbox-edge",
		"it is not merged, deployed, paired, or installed",
	} {
		if strings.Contains(strings.ToLower(guide), strings.ToLower(forbidden)) {
			t.Errorf("Parrot onboarding guide retained stale instruction %q", forbidden)
		}
	}

	readme := read("../README.md")
	for _, expected := range []string{
		"Hosted on CubePath",
		"hosted on **CubePath**",
		"mcp-devbox-charlez.duckdns.org",
		"/version",
		"system_runtime_info",
		"docs/baselines/",
	} {
		if !strings.Contains(readme, expected) {
			t.Errorf("README missing live-or-historical evidence pointer %q", expected)
		}
	}
	for _, forbidden := range []string{
		"Cubethon 2026 Q3",
		"cubethon-2026-q3-submission.md",
	} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README retains event-specific promotion %q", forbidden)
		}
	}

	submission := read("cubethon-2026-q3-submission.md")
	for _, expected := range []string{
		"MCP Devbox — Secure remote development workcells for AI agents",
		"https://mcp-devbox-charlez.duckdns.org",
		"https://github.com/charle-z/mcp-devbox",
		"Coolify on CubePath",
		"judge-accessible demo path",
		"<ADD_DISCORD_USERNAME>",
	} {
		if !strings.Contains(submission, expected) {
			t.Errorf("Cubethon draft missing %q", expected)
		}
	}
}
