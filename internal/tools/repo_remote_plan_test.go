package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestRepoRemotePreviewAndSetAddAllowedOwnerRemote(t *testing.T) {
	svc, root := initRepo(t, config.ModeAsk)
	svc.WithGitHub(NewGitHubClient("https://api.github.test", "token", "acme", "org", "private"))
	var commands [][]string
	svc.WithRunner(func(_ context.Context, _ string, _ string, args []string) (string, error) {
		commands = append(commands, append([]string(nil), args...))
		if strings.Join(args, " ") == "remote get-url origin" {
			return "", errCommand
		}
		return "ok", nil
	})
	preview, err := svc.RepoRemotePreview(root, "", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "proposed_url: https://github.com/acme/demo.git") || !strings.Contains(preview, "action: add") {
		t.Fatalf("bad remote preview:\n%s", preview)
	}
	planID := field(preview, "plan_id")
	out, err := svc.RepoRemoteSet(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask approval missing: out=%q err=%v", out, err)
	}
	if _, err := svc.RepoRemoteSet(planID, true); err != nil {
		t.Fatal(err)
	}
	last := strings.Join(commands[len(commands)-1], " ")
	if last != "remote add origin https://github.com/acme/demo.git" {
		t.Fatalf("remote command = %q", last)
	}
}

func TestRepoRemoteRejectsCredentialsWrongOwnerOptionAndChangedState(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient("https://api.github.test", "token", "acme", "org", "private"))
	for _, destination := range []string{
		"https://user:pass@github.com/acme/demo.git",
		"https://github.com/other/demo.git",
	} {
		if _, err := svc.RepoRemotePreview(root, "origin", destination); err == nil {
			t.Fatalf("unsafe destination accepted: %s", destination)
		}
	}
	if _, err := svc.RepoRemotePreview(root, "--delete", "demo"); err == nil {
		t.Fatal("option-like remote accepted")
	}

	current := "https://github.com/acme/old.git"
	svc.WithRunner(func(_ context.Context, _ string, _ string, args []string) (string, error) {
		if strings.Join(args, " ") == "remote get-url origin" {
			return current, nil
		}
		return "", nil
	})
	preview, err := svc.RepoRemotePreview(root, "origin", "demo")
	if err != nil {
		t.Fatal(err)
	}
	current = "https://github.com/acme/changed.git"
	if _, err := svc.RepoRemoteSet(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "remote changed") {
		t.Fatalf("changed remote state must fail: %v", err)
	}
}

var errCommand = &commandError{}

type commandError struct{}

func (*commandError) Error() string { return "command failed" }
