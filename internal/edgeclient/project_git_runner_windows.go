//go:build windows

package edgeclient

func NewDevGitCommandRunner(string, string) DevGitCommandRunner {
	return nil
}
