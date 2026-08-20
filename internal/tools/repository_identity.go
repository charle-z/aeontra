package tools

import "strings"

const (
	managedSourceRepository        = "aeontra"
	managedCompatibilityRepository = "mcp-devbox"
)

func managedRepositoryURL(owner string) string {
	return "https://github.com/" + owner + "/" + managedSourceRepository + ".git"
}

func managedRepositoryMatches(owner, raw string) bool {
	normalized := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(raw), "/"), ".git")
	for _, repository := range []string{managedSourceRepository, managedCompatibilityRepository} {
		ownerRepo := owner + "/" + repository
		if strings.EqualFold(normalized, ownerRepo) || strings.EqualFold(normalized, "https://github.com/"+ownerRepo) {
			return true
		}
	}
	return false
}
