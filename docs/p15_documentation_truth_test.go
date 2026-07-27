package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP15CurrentDocumentationSeparatesSourceDeploymentAndDeviceEvidence(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(body)
	}

	const catalogHash = "sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12"

	readme := read("../README.md")
	for _, required := range []string{
		"docs/tools.md",
		"/version",
		"system_runtime_info",
		"docs/baselines/",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README missing canonical/live identity pointer %q", required)
		}
	}
	for _, forbidden := range []string{
		"p15.0.5",
		"98 deliberately",
		catalogHash,
		"P13 opaque workspace continuation is deployed",
		"P14 first-class authorized HTB actions are deployed",
		"P15 signed zero-touch Edge is the current release line",
		"Parrot `p15.0.4`",
	} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README embeds mutable or historical state %q", forbidden)
		}
	}

	docMap := read("documentation-map.md")
	for _, required := range []string{
		"P14 target-locked runtime actions",
		"P15 historical release-candidate evidence remains",
		"Source/release,",
		"VPS deployment and real Edge installation must be reported as separate facts",
	} {
		if !strings.Contains(docMap, required) {
			t.Errorf("documentation map missing %q", required)
		}
	}
}

func TestP15SecurityDocumentationIsProfileSpecific(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(body)
	}

	policy := read("../SECURITY.md")
	model := read("security.md")

	for name, content := range map[string]string{
		"SECURITY.md":      policy,
		"docs/security.md": model,
	} {
		for _, required := range []string{
			"networkless",
			"Bubblewrap",
			"trusted_host_shared_network",
			"target",
			"VPN",
			"not",
		} {
			if !strings.Contains(content, required) {
				t.Errorf("%s missing security-boundary marker %q", name, required)
			}
		}
	}

	for _, stale := range []string{
		"does **not** yet provide\nOS-level isolation or egress control",
		"No OS sandbox (Layer 2)",
		"Keep the MIT \"as is\" disclaimer",
	} {
		if strings.Contains(policy, stale) || strings.Contains(model, stale) {
			t.Errorf("security documentation retains stale claim %q", stale)
		}
	}

	for _, required := range []string{
		"header-only recovery",
		"Ed25519-signed",
		"Authorized target-locked workspace",
		"Development Edge Git boundary",
	} {
		if !strings.Contains(policy+"\n"+model, required) {
			t.Errorf("security documentation missing %q", required)
		}
	}
}
