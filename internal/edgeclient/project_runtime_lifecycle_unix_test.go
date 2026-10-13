//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectWorktreeRuntimeCleanupPreservesReferencesAndArtifacts(t *testing.T) {
	for _, kind := range []string{"artifact", "toolbox", "process", "model", "unknown journal", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			workspace := Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: t.TempDir()}
			roots, err := prepareProjectRuntimeRoots(state, workspace)
			if err != nil {
				t.Fatal(err)
			}
			var expected error
			switch kind {
			case "artifact":
				if err := os.WriteFile(filepath.Join(roots.Artifacts, "report.txt"), []byte("evidence"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "toolbox":
				if err := preparePrivateRoot(filepath.Join(state, projectToolboxStateDirectory)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(state, projectToolboxStateDirectory, workspace.ID+".json"), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "process", "model", "unknown journal":
				file, table, status := projectProcessDatabaseFile, "project_processes", "running"
				if kind == "model" {
					file, table, status = openCodeRuntimeJournalFile, "local_opencode_runtimes", "starting"
				}
				db, err := sql.Open("sqlite", filepath.Join(state, file))
				if err != nil {
					t.Fatal(err)
				}
				if kind != "unknown journal" {
					if _, err := db.Exec("CREATE TABLE "+table+" (workspace_id TEXT,state TEXT); INSERT INTO "+table+" VALUES(?,?)", workspace.ID, status); err != nil {
						t.Fatal(err)
					}
				} else {
					expected = ErrProjectWorktreeUnavailable
					if _, err := db.Exec("CREATE TABLE old_schema (id TEXT)"); err != nil {
						t.Fatal(err)
					}
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(state, file), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(roots.Cache); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), roots.Cache); err != nil {
					t.Fatal(err)
				}
				expected = ErrProjectWorktreeUnsafe
			}
			err = cleanupProjectWorktreeRuntime(context.Background(), state, ProjectWorktreeSnapshot{WorkspaceID: workspace.ID, path: workspace.Path})
			if !errors.Is(err, expected) {
				t.Fatalf("cleanup=%v want %v", err, expected)
			}
			if kind == "artifact" {
				if content, err := os.ReadFile(filepath.Join(roots.Artifacts, "report.txt")); err != nil || string(content) != "evidence" {
					t.Fatalf("artifact changed: %v", err)
				}
				if _, err := os.Lstat(roots.Cache); !os.IsNotExist(err) {
					t.Fatal("cache retained despite no active user")
				}
			} else if _, err := os.Stat(roots.Runtime); err != nil {
				t.Fatalf("referenced/unsafe runtime was removed: %v", err)
			}
		})
	}
}

func TestProjectRuntimeGateAllowsReadersAndDoesNotBlockOtherWorkspaces(t *testing.T) {
	state := t.TempDir()
	id := "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first, err := acquireProjectRuntimeGate(context.Background(), state, id, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := acquireProjectRuntimeGate(context.Background(), state, id, false)
	if err != nil {
		t.Fatal(err)
	}
	second()
	other, err := acquireProjectRuntimeGate(context.Background(), state, "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", true)
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireProjectRuntimeGate(ctx, state, id, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup passed an active command: %v", err)
	}
	first()
	projectRuntimeGates.Lock()
	defer projectRuntimeGates.Unlock()
	if len(projectRuntimeGates.entries) != 0 {
		t.Fatal("gate registry leaked completed workspaces")
	}
}

func TestProjectWorktreeCleanupKeepsUnreadableStorageWithoutBlockingCompletion(t *testing.T) {
	manager, worktree, authority := newProjectWorktreeForContentDigest(t)
	workspace, err := manager.workspaces.Get(worktree.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := prepareProjectRuntimeRoots(manager.stateRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.stateRoot, projectProcessDatabaseFile), []byte("unreadable journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := ProjectWorktreeCleanupRequest{ID: worktree.ID, JobID: authority.JobID, LeaseID: authority.LeaseID, Fence: authority.Fence, IdempotencyKey: "runtime-cleanup-unreadable-0001"}
	removed, _, err := manager.Cleanup(context.Background(), request)
	if err != nil || removed.State != ProjectWorktreeRemoved {
		t.Fatalf("optional storage reclamation blocked source cleanup: %v", err)
	}
	if _, err := os.Stat(roots.Runtime); err != nil {
		t.Fatalf("uncertain storage was removed: %v", err)
	}
	if _, reused, err := manager.Cleanup(context.Background(), request); err != nil || !reused {
		t.Fatalf("cleanup retry=%v reused=%v", err, reused)
	}
}
