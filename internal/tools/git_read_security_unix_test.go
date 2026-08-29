//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitReadToolsDoNotExecuteRepositoryFSMonitor(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	hook := filepath.Join(root, "hostile-fsmonitor.sh")
	body := "#!/bin/sh\ntouch '" + marker + "'\nprintf '0\\n'\n"
	if err := os.WriteFile(hook, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "config", "--local", "core.fsmonitor", hook)

	if _, err := svc.GitStatus(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("read-only Git executed repository fsmonitor, stat err=%v", err)
	}
}

func TestHardenGitReadArgumentsDisableDiffPrograms(t *testing.T) {
	got := hardenGitReadArguments([]string{"diff", "--stat"})
	want := []string{"diff", "--no-ext-diff", "--no-textconv", "--stat"}
	if len(got) != len(want) {
		t.Fatalf("arguments=%q want=%q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("arguments=%q want=%q", got, want)
		}
	}
}
