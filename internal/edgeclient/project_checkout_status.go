package edgeclient

import (
	"strconv"
	"strings"
)

// These paths are the legacy, workspace-local runtime surfaces created by a
// Linux workcell or browser harness.  Do not treat the whole .mcp-devbox
// directory as managed: it is also a valid place for user-authored state and
// project files.  New runtime roots live outside the checkout and therefore
// do not need an entry here.
var managedProjectRuntimeStatusPrefixes = [...]string{
	".mcp-devbox/runtime/",
	".mcp-devbox/cache/",
	".mcp-devbox/tools/",
	".mcp-devbox/browser-harness/",
}

var managedProjectRuntimeStatusFiles = [...]string{
	".mcp-devbox/authorization-revision",
	".mcp-devbox/instructions.md",
	".mcp-devbox/current-state.md",
	".mcp-devbox/lab-contract.json",
	".mcp-devbox/tool-inventory.json",
}

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
// untracked files owned by one of the explicitly managed legacy runtime
// surfaces. Tracked, staged, renamed, deleted, and every other untracked path
// remain dirty. In particular, arbitrary files below .mcp-devbox are not
// silently treated as runtime data.
func ProjectCheckoutStatusClean(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || isManagedProjectRuntimeStatusLine(line) {
			continue
		}
		return false
	}
	return true
}

func isManagedProjectRuntimeStatusLine(line string) bool {
	if !strings.HasPrefix(line, "?? ") {
		return false
	}
	path := strings.TrimPrefix(line, "?? ")
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		decoded, err := strconv.Unquote(path)
		if err != nil {
			return false
		}
		path = decoded
	}
	for _, prefix := range managedProjectRuntimeStatusPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, managedFile := range managedProjectRuntimeStatusFiles {
		if path == managedFile {
			return true
		}
	}
	return false
}
