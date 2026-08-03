package edgeclient

import "strings"

const managedProjectRuntimeStatusPrefix = "?? .mcp-devbox/"

// ProjectCheckoutStatusClean reports whether porcelain status contains only
// untracked files owned by the workspace-local MCP Devbox runtime. Tracked,
// staged, renamed, deleted, and every other untracked path remain dirty.
func ProjectCheckoutStatusClean(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, managedProjectRuntimeStatusPrefix) {
			continue
		}
		return false
	}
	return true
}
