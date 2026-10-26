//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

// DirectWorkspaceStorage is a best-effort read. Its 10k-entry/100ms budget
// limits inspection cost, not workspace capacity. Incomplete accounting is
// explicit and never authorizes deletion or blocks status/stop.
func DirectWorkspaceStorage(ctx context.Context, stateRoot string, snapshot ProjectWorktreeSnapshot) edge.ProjectWorkspaceStorage {
	view := edge.ProjectWorkspaceStorage{Complete: true, RetentionState: "unknown", ScratchBudgetState: "unconfigured"}
	budgetText := os.Getenv("MCP_DEVBOX_SCRATCH_BUDGET_BYTES")
	if budgetText != "" {
		budget, err := strconv.ParseInt(budgetText, 10, 64)
		if err != nil || budget <= 0 {
			view.ScratchBudgetState = "invalid"
		} else {
			view.ScratchBudgetBytes = budget
			view.ScratchBudgetState = "unknown"
		}
	}
	roots, _, err := projectRuntimeRootPaths(stateRoot, Workspace{ID: snapshot.WorkspaceID, Path: snapshot.path})
	if err != nil || validateProjectWorktreeRuntime(stateRoot, snapshot) != nil {
		view.Complete = false
		return view
	}
	if retained, err := projectWorktreeRuntimeRetained(ctx, stateRoot, snapshot.WorkspaceID); err == nil {
		view.RetentionState = "unreferenced"
		if retained {
			view.RetentionState = "retained"
		}
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	for _, item := range []struct {
		path  string
		bytes *int64
	}{{roots.Runtime, &view.RuntimeBytes}, {roots.Cache, &view.CacheBytes}, {roots.Artifacts, &view.ArtifactBytes}} {
		root, err := os.OpenRoot(item.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			view.Complete = false
			continue
		}
		var visit func(string, int) bool
		visit = func(relative string, depth int) bool {
			if depth > 128 || view.ScannedEntries >= 10000 || ctx.Err() != nil || time.Now().After(deadline) {
				return false
			}
			file, err := root.Open(relative)
			if err != nil {
				return false
			}
			defer file.Close()
			for {
				entries, readErr := file.ReadDir(64)
				for _, entry := range entries {
					if view.ScannedEntries >= 10000 || ctx.Err() != nil || time.Now().After(deadline) {
						return false
					}
					view.ScannedEntries++
					if entry.Type()&os.ModeSymlink != 0 {
						view.Complete = false
						continue
					}
					name := filepath.Join(relative, entry.Name())
					if entry.IsDir() {
						if !visit(name, depth+1) {
							return false
						}
						continue
					}
					info, err := entry.Info()
					if err != nil || !info.Mode().IsRegular() {
						view.Complete = false
						continue
					}
					if info.Size() < 0 || *item.bytes > (1<<63-1)-info.Size() {
						return false
					}
					*item.bytes += info.Size()
					if item.path == roots.Runtime && strings.HasPrefix(name, "scratch"+string(filepath.Separator)) {
						view.ScratchBytes += info.Size()
					}
				}
				if readErr == io.EOF {
					return true
				}
				if readErr != nil {
					return false
				}
			}
		}
		if !visit(".", 0) {
			view.Complete = false
		}
		root.Close()
	}
	if view.ScratchBudgetBytes > 0 && view.Complete {
		view.ScratchBudgetState = "normal"
		if view.ScratchBytes > view.ScratchBudgetBytes {
			view.ScratchBudgetState = "over_budget"
		}
	}
	return view
}
