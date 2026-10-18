package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestWorkspaceCheckpointCleanExactSchemaAndBound(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	configIdentity(t, root)
	write(t, root, "tracked.txt", "base\n")
	write(t, root, ".agent-memory/current-task.md", "# Current task\n\nShip compact checkpoint safely.\n")
	gitCmd(t, root, "add", "tracked.txt", ".agent-memory/current-task.md")
	gitCmd(t, root, "commit", "-qm", "base")

	out, err := svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxWorkspaceCheckpointBytes {
		t.Fatalf("checkpoint bytes=%d, max=%d", len(out), maxWorkspaceCheckpointBytes)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode checkpoint: %v\n%s", err, out)
	}
	wantKeys := []string{"schema_version", "repository", "branch", "head_commit", "upstream", "upstream_commit", "origin_main_commit", "ahead", "behind", "tree_clean", "staged_file_count", "modified_file_count", "untracked_file_count", "temporary_file_count", "diff_files", "insertions", "deletions", "current_task_summary"}
	if len(got) != len(wantKeys) {
		t.Fatalf("schema keys=%d want=%d: %#v", len(got), len(wantKeys), got)
	}
	for _, key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("missing schema key %q", key)
		}
	}
	if got["tree_clean"] != true || got["current_task_summary"] != "Ship compact checkpoint safely." {
		t.Fatalf("unexpected clean checkpoint: %#v", got)
	}
	if strings.Contains(out, filepath.ToSlash(root)) || strings.Contains(out, filepath.Clean(root)) {
		t.Fatalf("absolute path leaked: %s", out)
	}
}

func TestWorkspaceCheckpointCountsChangesTemporaryFilesAndRedacts(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	configIdentity(t, root)
	write(t, root, "tracked.txt", "one\n")
	gitCmd(t, root, "add", "tracked.txt")
	gitCmd(t, root, "commit", "-qm", "base")
	write(t, root, "staged.txt", "staged\n")
	gitCmd(t, root, "add", "staged.txt")
	write(t, root, "tracked.txt", "one\ntwo\n")
	write(t, root, "scratch.tmp", "temporary\n")
	write(t, root, "untracked.txt", "new\n")
	write(t, root, ".agent-memory/current-task.md", "# Current task\n\nUse gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz safely.\n")

	var got workspaceCheckpoint
	out, err := svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.TreeClean || got.StagedFileCount != 1 || got.ModifiedFileCount != 1 || got.UntrackedFileCount != 3 || got.TemporaryFileCount != 1 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.DiffFiles != 2 || got.Insertions < 2 || got.Deletions != 0 {
		t.Fatalf("unexpected diff stats: %+v", got)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") || !strings.Contains(got.CurrentTaskSummary, "REDACTED") {
		t.Fatalf("task summary was not redacted: %s", out)
	}
}

func TestWorkspaceCheckpointUpstreamAheadBehindDetachedAndMissingUpstream(t *testing.T) {
	svc, root := initFastForwardRepo(t, config.ModeReadOnly)
	out, err := svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	var behind workspaceCheckpoint
	if err := json.Unmarshal([]byte(out), &behind); err != nil {
		t.Fatal(err)
	}
	if behind.Upstream != "origin/main" || behind.UpstreamCommit == "" || behind.OriginMainCommit == "" || behind.Behind != 1 || behind.Ahead != 0 {
		t.Fatalf("unexpected upstream checkpoint: %+v", behind)
	}

	gitCmd(t, root, "branch", "--unset-upstream")
	out, err = svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	var noUpstream workspaceCheckpoint
	_ = json.Unmarshal([]byte(out), &noUpstream)
	if noUpstream.Upstream != "" || noUpstream.UpstreamCommit != "" {
		t.Fatalf("missing upstream not represented safely: %+v", noUpstream)
	}

	gitCmd(t, root, "checkout", "--detach", "-q")
	out, err = svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	var detached workspaceCheckpoint
	_ = json.Unmarshal([]byte(out), &detached)
	if detached.Branch != "(detached)" {
		t.Fatalf("detached state missing: %+v", detached)
	}
}

func TestWorkspaceCheckpointReportsAheadWithoutFetching(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	configIdentity(t, root)
	write(t, root, "tracked.txt", "base\n")
	gitCmd(t, root, "add", "tracked.txt")
	gitCmd(t, root, "commit", "-qm", "base")
	gitCmd(t, root, "branch", "-M", "main")
	base := strings.TrimSpace(gitCmd(t, root, "rev-parse", "HEAD"))
	gitCmd(t, root, "remote", "add", "origin", root)
	gitCmd(t, root, "update-ref", "refs/remotes/origin/main", base)
	gitCmd(t, root, "branch", "--set-upstream-to=origin/main", "main")
	write(t, root, "tracked.txt", "base\nlocal\n")
	gitCmd(t, root, "add", "tracked.txt")
	gitCmd(t, root, "commit", "-qm", "local")

	out, err := svc.WorkspaceCheckpointIn("")
	if err != nil {
		t.Fatal(err)
	}
	var got workspaceCheckpoint
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Ahead != 1 || got.Behind != 0 || got.Upstream != "origin/main" || got.UpstreamCommit != base {
		t.Fatalf("unexpected ahead checkpoint: %+v", got)
	}
}

func TestWorkspaceCheckpointRejectsInvalidRepoAndJailEscape(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, "plain.txt", "not git\n")
	if _, err := svc.WorkspaceCheckpointIn(""); err == nil {
		t.Fatal("non-repository must fail")
	}
	if _, err := svc.WorkspaceCheckpointIn(filepath.Join(root, "..", "escape")); err == nil {
		t.Fatal("jail escape must fail")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := svc.WorkspaceCheckpointIn("link"); err == nil {
			t.Fatal("symlink escape must fail")
		}
	}
}
