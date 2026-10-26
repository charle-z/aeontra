//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeDirectFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func directWorkspaceFixture(t *testing.T) (*ProjectWorktreeManager, ProjectResolution, string) {
	t.Helper()
	f := newProjectWorktreeFixture(t)
	m, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{StateRoot: f.stateRoot, Roots: f.roots, Workspaces: f.workspaces,
		Runner: NewDevGitCommandRunner(f.stateRoot, "/usr/local/bin:/usr/bin:/bin"), Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("a", 36)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, ProjectResolution{Project: Project{Alias: "project", Owner: "charle-z", Repository: "project"}, TargetAlias: "parrot", Workspace: f.canonical}, f.head
}

func TestDirectWorkspacesReuseIdentityAndSeparateWriters(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	ctx := context.Background()
	a, _, err := m.PrepareDirectWorkspace(ctx, resolved, head, "direct-writer-one")
	if err != nil {
		t.Fatal(err)
	}
	again, reused, err := m.PrepareDirectWorkspace(ctx, resolved, head, "direct-writer-one")
	if err != nil || !reused || again.ID != a.ID {
		t.Fatalf("resume %+v %t %v", again, reused, err)
	}
	b, _, err := m.PrepareDirectWorkspace(ctx, resolved, head, "direct-writer-two")
	if err != nil || b.WorkspaceID == a.WorkspaceID || b.path == a.path {
		t.Fatalf("writers not separated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.path, "README.md"), []byte("writer A"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveDirectWorkspace(ctx, resolved, a.ID); err != nil {
		t.Fatalf("dirty workspace unusable: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(b.path, "README.md")); string(data) == "writer A" {
		t.Fatal("cross-writer source mutation")
	}
	cmd := exec.Command("git", "-C", a.path, "switch", "-c", "fix/direct-branch")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %s %v", out, err)
	}
	if _, err := m.ResolveDirectWorkspace(ctx, resolved, a.ID); err != nil {
		t.Fatalf("normal branch change invalidated workspace: %v", err)
	}
	foreign := resolved
	foreign.Project.Alias = "other"
	if _, err := m.ResolveDirectWorkspace(ctx, foreign, a.ID); !errors.Is(err, ErrProjectWorktreeConflict) {
		t.Fatalf("foreign alias accepted: %v", err)
	}
}

func TestDirectWorkspaceCannotSelectModelWorker(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	worker, _, err := m.Create(context.Background(), ProjectWorktreeCreateRequest{Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project", CanonicalWorkspaceID: resolved.Workspace.ID, CanonicalPath: resolved.Workspace.Path,
		BaseCommit: head, Role: ProjectWorktreeWriter, JobID: "wj_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LeaseID: "wl_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Fence: 1, IdempotencyKey: "model-worker-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveDirectWorkspace(context.Background(), resolved, worker.ID); !errors.Is(err, ErrProjectWorktreeConflict) {
		t.Fatalf("model fence bypass: %v", err)
	}
}

func TestDirectWorkspaceCommandsMapAttestedLinkedGitMetadata(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	w, _, err := m.PrepareDirectWorkspace(context.Background(), resolved, head, "direct-git-mapping-test")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := m.workspaces.Get(w.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := prepareDirectWorkcellProcessSpec(DirectWorkcellCommandRequest{
		OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: workspace, WorkspaceRoots: m.roots,
		StateRoot: m.stateRoot, Argv: []string{"git", "status", "--short"}, TimeoutSeconds: 10,
	}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	metadata, linked, err := resolveCodexWorktreeGitMetadata(workspace, m.roots)
	if err != nil || !linked {
		t.Fatalf("linked fixture: %v", err)
	}
	joined := strings.Join(spec.Args, "\n")
	for _, expected := range []string{"--bind\n" + metadata.CommonDir + "\n" + metadata.CommonDir} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("direct workspace Git mapping missing %q", expected)
		}
	}
	if strings.Contains(joined, "--setenv\nGIT_DIR\n") || strings.Contains(joined, "--setenv\nGIT_WORK_TREE\n") {
		t.Fatal("global Git overrides would break nested repository discovery")
	}
	if strings.Contains(joined, "--bind\n"+resolved.Workspace.Path+"\n") {
		t.Fatal("canonical source was mounted alongside the linked workspace")
	}
}

func TestDirectWorkspaceToolboxMapsAttestedLinkedGitMetadata(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	w, _, err := m.PrepareDirectWorkspace(context.Background(), resolved, head, "direct-toolbox-git-test")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := m.workspaces.Get(w.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(m.stateRoot, "podman.sock")}
	tb, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: m.stateRoot, WorkspaceRoots: m.roots,
		Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"},
		Runner:   runner, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := tb.Create(ctx, ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	metadata, linked, err := resolveCodexWorktreeGitMetadata(workspace, m.roots)
	if err != nil || !linked {
		t.Fatalf("linked fixture: %v", err)
	}
	found := false
	for _, mount := range runner.mounts {
		if mount.Destination == metadata.CommonDir && mount.Source == metadata.CommonDir && mount.RW {
			found = true
		}
	}
	if !found {
		t.Fatal("toolbox cannot resolve its linked Git metadata")
	}
	for _, item := range runner.containerEnv {
		if strings.HasPrefix(item, "GIT_DIR=") || strings.HasPrefix(item, "GIT_WORK_TREE=") {
			t.Fatal("global Git override")
		}
	}
	// The explicit repair path can upgrade an older linked toolbox without
	// treating its missing Git mount as permission to discard source.
	runner.mounts = runner.mounts[:4]
	if _, err := tb.Status(ctx, ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxMountMismatch) {
		t.Fatalf("old linked toolbox: %v", err)
	}
	if repaired, err := tb.Reconcile(ctx, ProjectToolboxReconcileRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil || repaired.Generation != 2 {
		t.Fatalf("linked toolbox reconciliation: %v", err)
	}
}

func TestDirectWorkspaceGitMappingRejectsAnotherRepository(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	w, _, err := m.PrepareDirectWorkspace(context.Background(), resolved, head, "direct-git-binding-test")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := m.ResolveDirectWorkspace(context.Background(), resolved, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Workspace.ManagedGitCommonDir != filepath.Join(resolved.Workspace.Path, ".git") {
		t.Fatal("canonical binding lost")
	}
	foreign := filepath.Join(m.roots.Dev, "other", ".git", "worktrees", "foreign")
	if err := os.MkdirAll(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	writeDirectFixture(t, filepath.Join(foreign, "commondir"), "../..\n")
	writeDirectFixture(t, filepath.Join(w.path, ".git"), "gitdir: "+foreign+"\n")
	if _, _, err := resolveCodexWorktreeGitMetadata(selected.Workspace, m.roots); err == nil {
		t.Fatal("other repository metadata admitted")
	}
}

func TestDirectWorkspaceGitNativeWorkcell(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_NATIVE_WORKCELL_TEST") != "1" {
		t.Skip("requires intended Linux host Bubblewrap; run explicitly")
	}
	m, resolved, head := directWorkspaceFixture(t)
	w, _, err := m.PrepareDirectWorkspace(context.Background(), resolved, head, "native-direct-git-test")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := m.ResolveDirectWorkspace(context.Background(), resolved, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, argv := range [][]string{
		{"git", "switch", "-c", "qa/native-direct-git"},
		{"sh", "-c", "git init -b nested nested >/dev/null && test \"$(git -C nested symbolic-ref HEAD)\" = refs/heads/nested && test \"$(git rev-parse --show-toplevel)\" = /workspace"},
	} {
		r, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
			OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: selected.Workspace, WorkspaceRoots: m.roots,
			StateRoot: m.stateRoot, Argv: argv, TimeoutSeconds: 20,
		}, nil)
		if err != nil || r.ExitCode != 0 {
			t.Fatalf("native step %d: %+v %v", i, r, err)
		}
	}
}

func TestDirectWorkspaceCleanupPreservesSourceAndLiveReferences(t *testing.T) {
	for _, kind := range []string{"dirty", "ignored", "committed", "detached-commit", "toolbox", "stale-head", "clean"} {
		t.Run(kind, func(t *testing.T) {
			m, resolved, head := directWorkspaceFixture(t)
			ctx := context.Background()
			w, _, err := m.PrepareDirectWorkspace(ctx, resolved, head, "cleanup-direct-test")
			if err != nil {
				t.Fatal(err)
			}
			workspace, _ := m.workspaces.Get(w.WorkspaceID)
			roots, err := prepareProjectRuntimeRoots(m.stateRoot, workspace)
			if err != nil {
				t.Fatal(err)
			}
			writeDirectFixture(t, filepath.Join(roots.Cache, "temporary"), "cache")
			writeDirectFixture(t, filepath.Join(roots.Artifacts, "report"), "preserved")
			switch kind {
			case "dirty":
				writeDirectFixture(t, filepath.Join(w.path, "unique.txt"), "work")
			case "ignored":
				gitDir, err := os.ReadFile(filepath.Join(w.path, ".git"))
				if err != nil {
					t.Fatal(err)
				}
				common := filepath.Dir(filepath.Dir(strings.TrimSpace(strings.TrimPrefix(string(gitDir), "gitdir: "))))
				writeDirectFixture(t, filepath.Join(common, "info", "exclude"), "ignored.txt\n")
				writeDirectFixture(t, filepath.Join(w.path, "ignored.txt"), "unique ignored work")
			case "committed", "detached-commit":
				if kind == "detached-commit" {
					if out, err := exec.Command("git", "-C", w.path, "switch", "--detach").CombinedOutput(); err != nil {
						t.Fatalf("detach: %s %v", out, err)
					}
				}
				writeDirectFixture(t, filepath.Join(w.path, "unique.txt"), "work")
				for _, args := range [][]string{{"add", "unique.txt"}, {"commit", "-m", "fixture"}} {
					cmd := exec.Command("git", append([]string{"-C", w.path}, args...)...)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("git: %s %v", out, err)
					}
				}
				current, err := m.Status(ctx, w.ID)
				if err != nil {
					t.Fatal(err)
				}
				head = current.HeadCommit
			case "toolbox":
				if err := preparePrivateRoot(filepath.Join(m.stateRoot, projectToolboxStateDirectory)); err != nil {
					t.Fatal(err)
				}
				writeDirectFixture(t, filepath.Join(m.stateRoot, projectToolboxStateDirectory, w.WorkspaceID+".json"), "{}")
			case "stale-head":
				head = strings.Repeat("a", 40)
			}
			request := ProjectWorktreeCleanupRequest{ID: w.ID, JobID: w.JobID, LeaseID: w.LeaseID, Fence: w.Fence, Direct: true, ExpectedHead: head, IdempotencyKey: "direct-cleanup-receipt"}
			removed, _, err := m.Cleanup(ctx, request)
			if kind != "clean" && kind != "committed" {
				if err == nil {
					t.Fatal("unsafe cleanup accepted")
				}
				if _, err := os.Stat(w.path); err != nil {
					t.Fatal("source was removed")
				}
				return
			}
			if err != nil || removed.State != ProjectWorktreeRemoved {
				t.Fatalf("clean cleanup: %v", err)
			}
			if kind == "committed" {
				out, err := exec.Command("git", "-C", resolved.Workspace.Path, "rev-parse", "refs/heads/"+w.Branch).Output()
				if err != nil || strings.TrimSpace(string(out)) != head {
					t.Fatalf("committed source ref lost: %v", err)
				}
			}
			if _, reused, err := m.Cleanup(ctx, request); err != nil || !reused {
				t.Fatalf("receipt recovery: %v", err)
			}
			if _, err := os.Stat(roots.Cache); !os.IsNotExist(err) {
				t.Fatal("cache retained")
			}
			if data, err := os.ReadFile(filepath.Join(roots.Artifacts, "report")); err != nil || string(data) != "preserved" {
				t.Fatal("artifact lost")
			}
		})
	}
}

func TestDirectWorkspaceStorageIsBoundedAndDoesNotFollowSymlinks(t *testing.T) {
	m, resolved, head := directWorkspaceFixture(t)
	w, _, err := m.PrepareDirectWorkspace(context.Background(), resolved, head, "storage-direct-test")
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := m.workspaces.Get(w.WorkspaceID)
	roots, err := prepareProjectRuntimeRoots(m.stateRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roots.Cache, "cache"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	view := DirectWorkspaceStorage(context.Background(), m.stateRoot, w)
	if !view.Complete || view.CacheBytes != 5 || view.RetentionState != "unreferenced" {
		t.Fatalf("accounting %+v", view)
	}
	t.Setenv("MCP_DEVBOX_SCRATCH_BUDGET_BYTES", "4")
	writeDirectFixture(t, filepath.Join(roots.Runtime, "scratch", "log"), "12345")
	view = DirectWorkspaceStorage(context.Background(), m.stateRoot, w)
	if view.ScratchBytes != 5 || view.ScratchBudgetState != "over_budget" {
		t.Fatalf("advisory budget %+v", view)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(roots.Cache, "outside")); err != nil {
		t.Fatal(err)
	}
	view = DirectWorkspaceStorage(context.Background(), m.stateRoot, w)
	if view.Complete || view.ScannedEntries > 10000 {
		t.Fatalf("symlink followed %+v", view)
	}
}
