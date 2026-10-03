//go:build !windows

package edgeclient

import "slices"

func NewDevGitCommandRunner(stateRoot, toolPath string) DevGitCommandRunner {
	return execDevGitCommandRunner{stateRoot: stateRoot, toolPath: toolPath}
}

// NewRegisteredProjectSourceGitRunner accepts only the fixed read-only source
// inspection argv and no credential. Its larger output budget is not used by
// transport, publication, or managed-worktree callers.
func NewRegisteredProjectSourceGitRunner(stateRoot, toolPath string) DevGitCommandRunner {
	return execDevGitCommandRunner{stateRoot: stateRoot, toolPath: toolPath, sourceInspection: true}
}

func registeredProjectSourceGitArguments(args []string) bool {
	for _, allowed := range [][]string{
		{"rev-parse", "--verify", "HEAD"},
		{"status", "--porcelain=v1", "-z", "--untracked-files=all"},
		{"ls-tree", "-r", "--full-tree", "-z", "--name-only", "HEAD"},
		{"ls-files", "--cached", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		if slices.Equal(args, allowed) {
			return true
		}
	}
	return false
}
