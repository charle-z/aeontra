package docs_test

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestOSSGovernanceDocumentsDefinePublicBoundaries(t *testing.T) {
	for path, required := range map[string][]string{
		"../CONTRIBUTING.md": {
			"## Development setup",
			"## Change workflow",
			"Use Conventional Commits",
			"## Verification tiers",
			"## Security-sensitive changes",
			"## Developer Certificate of Origin",
			"git commit -s",
		},
		"../SUPPORT.md": {
			"community-maintained",
			"no service-level agreement",
			"GitHub Issues",
			"SECURITY.md",
			"Source, server deployment, Front Door, and installed Edge versions are independent facts",
		},
		"../GOVERNANCE.md": {
			"primary-maintainer model",
			"## Decision making",
			"## Roles",
			"Enterprise governance, commercial licensing, billing, and multi-tenant administration are deferred",
		},
		"../.github/PULL_REQUEST_TEMPLATE.md": {
			"## Problem and invariant",
			"## Verification",
			"## Compatibility and rollback",
			"## Security and authority",
			"DCO sign-off",
		},
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, marker := range required {
			if !containsNormalizedProse(string(content), marker) {
				t.Errorf("%s missing governance marker %q", path, marker)
			}
		}
	}
}

func TestOSSIssueFormsAreValidAndDoNotSolicitSecrets(t *testing.T) {
	for _, path := range []string{
		"../.github/ISSUE_TEMPLATE/config.yml",
		"../.github/ISSUE_TEMPLATE/bug_report.yml",
		"../.github/ISSUE_TEMPLATE/feature_request.yml",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var document any
		if err := yaml.Unmarshal(content, &document); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		lower := strings.ToLower(string(content))
		if strings.Contains(lower, "paste your token") || strings.Contains(lower, "paste your password") {
			t.Errorf("%s asks contributors to disclose a secret", path)
		}
	}

	bug := readDoc(t, "../.github/ISSUE_TEMPLATE/bug_report.yml")
	for _, marker := range []string{"Minimal reproduction", "Version and environment", "Bounded diagnostics", "removed secrets"} {
		if !strings.Contains(bug, marker) {
			t.Errorf("bug report form missing %q", marker)
		}
	}

	feature := readDoc(t, "../.github/ISSUE_TEMPLATE/feature_request.yml")
	for _, marker := range []string{"Authority and trust boundaries", "Alternatives considered", "Compatibility considerations"} {
		if !strings.Contains(feature, marker) {
			t.Errorf("feature request form missing %q", marker)
		}
	}
}
