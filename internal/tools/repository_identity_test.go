package tools

import "testing"

func TestManagedRepositoryIdentityUsesAeontraAndAcceptsOnlyCompatibilitySlug(t *testing.T) {
	if got := managedRepositoryURL("acme"); got != "https://github.com/acme/aeontra.git" {
		t.Fatalf("managed repository URL=%q", got)
	}
	for _, repository := range []string{
		"acme/aeontra",
		"https://github.com/acme/aeontra.git",
		"acme/mcp-devbox",
		"https://github.com/acme/mcp-devbox.git",
	} {
		if !managedRepositoryMatches("acme", repository) {
			t.Errorf("managed repository rejected %q", repository)
		}
	}
	for _, repository := range []string{
		"other/aeontra",
		"acme/aeontra-extra",
		"ssh://github.com/acme/aeontra",
		"https://github.com/acme/third.git",
		"",
	} {
		if managedRepositoryMatches("acme", repository) {
			t.Errorf("managed repository accepted %q", repository)
		}
	}
}

func TestManagedGitHubOperationsTargetAeontra(t *testing.T) {
	if edgeReleaseRepo != managedSourceRepository {
		t.Fatalf("edge release repository=%q", edgeReleaseRepo)
	}
	if managedBackendRepository != managedSourceRepository {
		t.Fatalf("managed backend repository=%q", managedBackendRepository)
	}
}
