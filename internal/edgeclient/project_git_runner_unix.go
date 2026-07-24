//go:build !windows

package edgeclient

func NewDevGitCommandRunner(stateRoot, toolPath string) DevGitCommandRunner {
	return execDevGitCommandRunner{stateRoot: stateRoot, toolPath: toolPath}
}
