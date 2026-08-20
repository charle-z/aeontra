package docs_test

import (
	"regexp"
	"strings"
	"testing"
)

func TestStep17OperationalDocsDoNotEmbedMovingIdentity(t *testing.T) {
	paths := []string{
		"../README.md",
		"../AGENTS.md",
		"features.md",
		"design.md",
		"connect-remote.md",
		"deploy-coolify.md",
		"install-edge-parrot-p15.md",
		"install-edge-parrot-p16.md",
		"context-capsule.md",
	}

	toolCount := regexp.MustCompile(`(?i)\b(?:15|55|62|67|71|78|85|98|100|102)\s+(?:annotated\s+)?(?:MCP\s+)?tools?\b`)
	commit := regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	catalogHash := regexp.MustCompile(`sha256:[0-9a-f]{64}`)

	for _, path := range paths {
		doc := readDoc(t, path)
		if match := toolCount.FindString(doc); match != "" {
			t.Errorf("%s embeds moving tool count %q", path, match)
		}
		if match := commit.FindString(doc); match != "" {
			t.Errorf("%s embeds moving commit %q", path, match)
		}
		if match := catalogHash.FindString(doc); match != "" {
			t.Errorf("%s embeds moving catalog hash %q", path, match)
		}
	}
}

func TestStep17AgentsIsOperationalNotAReleaseDiary(t *testing.T) {
	doc := readDoc(t, "../AGENTS.md")
	for _, required := range []string{
		"docs/configuration.md",
		"docs/security.md",
		"docs/tools.md",
		"/version",
		"system_runtime_info",
		"## Tool Discovery Index",
		"## Development Discipline",
		"## Host-Specific Acceptance",
		"## Git Rules",
		"Mandatory lookup rule",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("AGENTS.md missing operational marker %q", required)
		}
	}
	for _, forbidden := range []string{
		"Current phase:",
		"P8.1 Console 2.0 deployed",
		"Current release baseline",
		"last repository-recorded Parrot install",
	} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("AGENTS.md retains release diary marker %q", forbidden)
		}
	}
}

func TestStep17FeaturesAndDesignAreClearlyClassified(t *testing.T) {
	features := readDoc(t, "features.md")
	design := readDoc(t, "design.md")

	for name, doc := range map[string]string{"features.md": features, "design.md": design} {
		for _, required := range []string{"Historical", "README.md", "configuration.md", "security.md", "tools.md"} {
			if !strings.Contains(doc, required) {
				t.Errorf("%s missing classification/source %q", name, required)
			}
		}
	}
	for _, forbidden := range []string{
		"mcp-devbox exposes 15 MCP tools",
		"current 78-tool",
		"deployed P8.1/P9 production baseline remains",
		"Install fork — DECISION PENDING",
		"Agent-first capability | In progress",
	} {
		if strings.Contains(features+"\n"+design, forbidden) {
			t.Errorf("features/design retain stale active claim %q", forbidden)
		}
	}
}

func TestStep17ConnectionAndDeploymentGuidesDelegateCanonicalConfiguration(t *testing.T) {
	connect := readDoc(t, "connect-remote.md")
	deploy := readDoc(t, "deploy-coolify.md")

	for name, doc := range map[string]string{"connect-remote.md": connect, "deploy-coolify.md": deploy} {
		for _, required := range []string{
			"configuration.md",
			"OAuth",
			"header-only recovery",
			"query-string credentials",
			"/healthz",
			"/version",
		} {
			if !strings.Contains(doc, required) {
				t.Errorf("%s missing flow contract %q", name, required)
			}
		}
	}

	for _, required := range []string{
		"OAuth client and refresh",
		"Brain is optional",
		"GitHub and Coolify integrations are optional",
		"must not mount `/var/run/docker.sock`",
		"rolling replacement",
		"exact commit",
	} {
		if !containsNormalizedProse(deploy, required) {
			t.Errorf("deploy-coolify.md missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"## Runtime Env Vars",
		"## Current Tool Surface",
		"ChatGPT should list these tools",
		"The P9 candidate exposes",
		"Container/Coolify env:",
	} {
		if strings.Contains(connect+"\n"+deploy, forbidden) {
			t.Errorf("connection/deployment guides duplicate canonical data via %q", forbidden)
		}
	}
}

func TestStep17EdgeInstallDocsSeparateCurrentProcedureFromHistoricalCandidates(t *testing.T) {
	p15 := readDoc(t, "install-edge-parrot-p15.md")
	p16 := readDoc(t, "install-edge-parrot-p16.md")

	for _, required := range []string{
		"Historical",
		"install-edge-parrot-p16.md",
		"source release",
		"installed Edge",
	} {
		if !strings.Contains(p15, required) {
			t.Errorf("P15 install document missing %q", required)
		}
	}
	for _, required := range []string{
		"Canonical",
		"configuration.md",
		"source release",
		"package artifact",
		"installed Edge",
		"separate evidence",
		"validation pending",
		"https://mcp.example.com",
		"not an installation default",
	} {
		if !strings.Contains(p16, required) {
			t.Errorf("P16 install document missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Status: **Step 2 implementation candidate**",
		"Step 2 does not yet claim",
		"Its final normal interaction will be",
		"Status: P15 release-candidate workflow",
	} {
		if strings.Contains(p15+"\n"+p16, forbidden) {
			t.Errorf("Edge install docs retain stale candidate wording %q", forbidden)
		}
	}
	if strings.Contains(p16, "mcp-devbox-charlez.duckdns.org") {
		t.Error("canonical P16 install document contains the maintainer demo domain")
	}
}

func TestStep17ContextCapsuleIsBoundedAndPointsToLiveTruth(t *testing.T) {
	doc := readDoc(t, "context-capsule.md")
	if lines := len(strings.Split(doc, "\n")); lines > 180 {
		t.Fatalf("context capsule is still a phase diary: %d lines", lines)
	}
	for _, required := range []string{
		"docs/configuration.md",
		"docs/security.md",
		"docs/tools.md",
		"docs/baselines/",
		"/version",
		"system_runtime_info",
		"source release",
		"VPS deployment",
		"installed Edge",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("context capsule missing source pointer %q", required)
		}
	}
	for _, forbidden := range []string{
		"### Historical P15 progression",
		"P1 catalog modularization is deployed",
		"## Historical console durable live state release candidate",
		"## Next Steps",
		"## Last Verified",
	} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("context capsule retains diary section %q", forbidden)
		}
	}
}
