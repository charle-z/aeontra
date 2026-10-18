package brain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type errorAfterUpdateRefRunner struct {
	delegate gitCommandRunner
}

func (r *errorAfterUpdateRefRunner) Run(ctx context.Context, root string, env []string, args ...string) (string, error) {
	output, err := r.delegate.Run(ctx, root, env, args...)
	if err != nil {
		return output, err
	}
	if gitSubcommand(args) == "update-ref" {
		return "", errors.New("injected transport error after successful ref update")
	}
	return output, nil
}

func TestWriteAgentAcceptsVerifiedRefAfterAmbiguousUpdateRefResult(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.git.runner = &errorAfterUpdateRefRunner{delegate: store.git.runner}
	note, err := store.WriteAgent(context.Background(), agentDraft("ambiguous-ref", "agent:chatgpt"))
	if err != nil {
		t.Fatal(err)
	}
	if note.Metadata.Slug != "ambiguous-ref" {
		t.Fatalf("note=%+v", note)
	}
	path := filepath.Join(root, WorkingDir, "ambiguous-ref.md")
	working, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	committed := runGitTest(t, root, "show", "HEAD:working/ambiguous-ref.md")
	if strings.TrimSpace(committed) != strings.TrimSpace(string(working)) {
		t.Fatal("verified HEAD and working source differ")
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "2" {
		t.Fatalf("commit count=%s", count)
	}
}
