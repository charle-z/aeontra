//go:build windows

package edgeclient

import (
	"context"
)

func projectWorktreeContentDigest(context.Context, *ProjectWorktreeManager, ProjectWorktreeSnapshot) (string, error) {
	// Managed project worktrees are Linux workcell checkouts. Do not approximate
	// their no-follow filesystem semantics on native Windows.
	return "", ErrProjectWorktreeUnavailable
}
