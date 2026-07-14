package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeGitRejectsExistingRemote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitIgnoreLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "remote", "add", "origin", "https://example.invalid/private.git")
	if err := store.InitializeGit(context.Background()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "remote") {
		t.Fatalf("existing remote error=%v", err)
	}
}

func TestInitializeGitRejectsUnsafeGitignore(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "broad permissions",
			setup: func(t *testing.T, root string) {
				path := filepath.Join(root, ".gitignore")
				if err := os.WriteFile(path, []byte(gitIgnoreLine+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, root string) {
				outside := filepath.Join(t.TempDir(), "ignore")
				if err := os.WriteFile(outside, []byte(gitIgnoreLine+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, ".gitignore")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "brain")
			store, err := OpenStore(root, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			runGitTest(t, root, "init", "--quiet", "--initial-branch=main")
			testCase.setup(t, root)
			if err := store.InitializeGit(context.Background()); err == nil {
				t.Fatal("unsafe .gitignore unexpectedly accepted")
			}
		})
	}
}

func TestWriteAgentRejectsGitMetadataSwap(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	gitPath := filepath.Join(root, ".git")
	realPath := filepath.Join(root, ".git-real")
	if err := os.Rename(gitPath, realPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, gitPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.WriteAgent(context.Background(), agentDraft("metadata-swap", "agent:chatgpt")); err == nil {
		t.Fatal("Git metadata symlink unexpectedly accepted")
	}
	if _, err := os.Stat(filepath.Join(root, WorkingDir, "metadata-swap.md")); !os.IsNotExist(err) {
		t.Fatalf("source created despite metadata swap: %v", err)
	}
}

func TestWriteAgentHonorsAlreadyCancelledContextBeforeMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.WriteAgent(ctx, agentDraft("cancelled-note", "agent:chatgpt")); err == nil {
		t.Fatal("cancelled write unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, WorkingDir, "cancelled-note.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled source exists: %v", err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("cancelled write changed commits=%s", count)
	}
}
