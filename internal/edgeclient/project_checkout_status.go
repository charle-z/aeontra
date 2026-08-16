package edgeclient

import "strings"

const managedProjectRuntimeStatusPrefix = "?? .mcp-devbox/"

var projectCheckoutStatusArgs = [...]string{
	"status",
	"--porcelain=v1",
	"--untracked-files=normal",
}

// ProjectCheckoutStatusArgs returns the fixed Git status invocation used by
// project cleanliness gates. "normal" still reports every untracked file or
// directory that can make a checkout dirty, but it avoids recursively
// expanding large untracked trees such as the workspace-local MCP runtime.
func ProjectCheckoutStatusArgs() []string {
	return append([]string(nil), projectCheckoutStatusArgs[:]...)
}

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
