//go:build windows

package edgeclient

import "context"

// Managed task worktree execution is Linux-only. Preserve Windows storage.
func acquireProjectRuntimeGate(context.Context, string, string, bool) (func(), error) {
	return func() {}, nil
}

func cleanupProjectWorktreeRuntime(context.Context, string, ProjectWorktreeSnapshot) error {
	return nil
}

func validateProjectWorktreeRuntime(string, ProjectWorktreeSnapshot) error { return nil }

func projectWorktreeRuntimeRetained(context.Context, string, string) (bool, error) { return false, nil }
