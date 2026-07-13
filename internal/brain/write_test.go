package brain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func agentDraft(slug, author string) AgentDraft {
	return AgentDraft{
		Slug:       slug,
		Title:      "Working note " + slug,
		Type:       TypeNote,
		Author:     author,
		Provenance: "owner discussion on 2026-07-13",
		ReviewBy:   "2026-08-13",
		Body:       "Bounded working memory linked to [[release-gates]].",
	}
}

func TestWriteAgentAtomicallyCreatesAndUpdatesOneCommitPerSuccess(t *testing.T) {
	now := fixedNow
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStoreWithClock(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("initial count=%s", count)
	}

	created, err := store.WriteAgent(context.Background(), agentDraft("rollback-note", "agent:chatgpt"))
	if err != nil {
		t.Fatal(err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "2" {
		t.Fatalf("create count=%s", count)
	}
	path := filepath.Join(root, WorkingDir, "rollback-note.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	working, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	committed := runGitTest(t, root, "show", "HEAD:working/rollback-note.md")
	if strings.TrimSpace(committed) != strings.TrimSpace(string(working)) {
		t.Fatalf("committed source differs\nworking:\n%s\ncommitted:\n%s", working, committed)
	}

	now = now.Add(time.Hour)
	update := agentDraft("rollback-note", "agent:chatgpt")
	update.Title = "Updated rollback note"
	update.Body = "Updated body with [[release-gates]]."
	updated, err := store.WriteAgent(context.Background(), update)
	if err != nil {
		t.Fatal(err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "3" {
		t.Fatalf("update count=%s", count)
	}
	if updated.Metadata.Created != created.Metadata.Created || updated.Metadata.Updated == created.Metadata.Updated {
		t.Fatalf("created=%+v updated=%+v", created.Metadata, updated.Metadata)
	}
	if status := runGitTest(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("dirty repository after update: %q", status)
	}
}

func TestWriteAgentRejectsSecretAndCrossAuthorWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	initialCount := runGitTest(t, root, "rev-list", "--count", "HEAD")
	secret := agentDraft("secret-note", "agent:chatgpt")
	secret.Body = "github_pat_0123456789abcdefghijklmnopQRSTUV"
	if _, err := store.WriteAgent(context.Background(), secret); err == nil {
		t.Fatal("secret write unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, WorkingDir, "secret-note.md")); !os.IsNotExist(err) {
		t.Fatalf("secret file exists: %v", err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != initialCount {
		t.Fatalf("secret write changed commit count=%s", count)
	}

	if _, err := store.WriteAgent(context.Background(), agentDraft("shared-note", "agent:chatgpt")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, WorkingDir, "shared-note.md"))
	if err != nil {
		t.Fatal(err)
	}
	countBefore := runGitTest(t, root, "rev-list", "--count", "HEAD")
	if _, err := store.WriteAgent(context.Background(), agentDraft("shared-note", "agent:claude")); err == nil {
		t.Fatal("cross-author write unexpectedly succeeded")
	}
	after, err := os.ReadFile(filepath.Join(root, WorkingDir, "shared-note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("cross-author write changed source")
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != countBefore {
		t.Fatalf("cross-author write changed commit count=%s", count)
	}
}

type failGitRunner struct {
	delegate gitCommandRunner
	command  string
}

func (r *failGitRunner) Run(ctx context.Context, root string, env []string, args ...string) (string, error) {
	for _, arg := range args {
		if arg == r.command {
			return "", errors.New("injected private git failure text")
		}
	}
	return r.delegate.Run(ctx, root, env, args...)
}

func TestWriteAgentRollsBackNewSourceAndIndexWhenGitFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	headBefore := runGitTest(t, root, "rev-parse", "HEAD")
	store.git.runner = &failGitRunner{delegate: store.git.runner, command: "commit-tree"}
	_, err = store.WriteAgent(context.Background(), agentDraft("failed-note", "agent:chatgpt"))
	if err == nil {
		t.Fatal("injected Git failure unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "private git failure") {
		t.Fatalf("Git error leaked private details: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, WorkingDir, "failed-note.md")); !os.IsNotExist(statErr) {
		t.Fatalf("failed source exists: %v", statErr)
	}
	if head := runGitTest(t, root, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("HEAD changed from %s to %s", headBefore, head)
	}
	if status := runGitTest(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("repository dirty after rollback: %q", status)
	}
}

func TestWriteAgentRollsBackExistingSourceWhenRefUpdateFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteAgent(context.Background(), agentDraft("existing-note", "agent:chatgpt")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, WorkingDir, "existing-note.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := runGitTest(t, root, "rev-parse", "HEAD")
	store.git.runner = &failGitRunner{delegate: store.git.runner, command: "update-ref"}
	update := agentDraft("existing-note", "agent:chatgpt")
	update.Body = "This source must roll back."
	if _, err := store.WriteAgent(context.Background(), update); err == nil {
		t.Fatal("injected ref failure unexpectedly succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing source was not restored")
	}
	if head := runGitTest(t, root, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("HEAD changed from %s to %s", headBefore, head)
	}
	if status := runGitTest(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("repository dirty after restore: %q", status)
	}
}

type recordingGitRunner struct {
	delegate gitCommandRunner
	mu       sync.Mutex
	calls    [][]string
}

func (r *recordingGitRunner) Run(ctx context.Context, root string, env []string, args ...string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	r.mu.Unlock()
	return r.delegate.Run(ctx, root, env, args...)
}

func TestWriteAgentUsesOnlyLocalPlumbingCommands(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingGitRunner{delegate: store.git.runner}
	store.git.runner = recorder
	if _, err := store.WriteAgent(context.Background(), agentDraft("plumbing-note", "agent:chatgpt")); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"symbolic-ref": true,
		"rev-parse":    true,
		"read-tree":    true,
		"hash-object":  true,
		"update-index": true,
		"write-tree":   true,
		"commit-tree":  true,
		"update-ref":   true,
	}
	for _, call := range recorder.calls {
		command := gitSubcommand(call)
		if !allowed[command] {
			t.Fatalf("unexpected Git command %q in %v", command, call)
		}
		for _, argument := range call {
			switch argument {
			case "push", "fetch", "pull", "remote", "clone", "upload-pack", "receive-pack":
				t.Fatalf("forbidden Git capability %q in %v", argument, call)
			}
			if strings.HasPrefix(argument, "--upload-pack") || strings.HasPrefix(argument, "--receive-pack") {
				t.Fatalf("forbidden Git transport option %q in %v", argument, call)
			}
		}
	}
}

func TestConcurrentAgentWritesAreSerializedAndCommitted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			slug := "concurrent-note-" + string(rune('a'+index))
			_, err := store.WriteAgent(context.Background(), agentDraft(slug, "agent:chatgpt"))
			errorsOut <- err
		}(i)
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "9" {
		t.Fatalf("commit count=%s", count)
	}
	if status := runGitTest(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("repository dirty: %q", status)
	}
}
