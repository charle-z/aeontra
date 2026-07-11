package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

type publishFixture struct {
	head       string
	branch     string
	dirty      bool
	remoteURL  string
	remoteSHA  string
	counts     string
	commands   [][]string
	pushOutput string
}

func (f *publishFixture) runner(_ context.Context, _ string, _ string, args []string) (string, error) {
	f.commands = append(f.commands, append([]string(nil), args...))
	switch args[0] {
	case "status":
		status := fmt.Sprintf("# branch.oid %s\n# branch.head %s\n", f.head, f.branch)
		if f.dirty {
			status += "? dirty.txt\n"
		}
		return status, nil
	case "remote":
		return f.remoteURL, nil
	case "ls-remote":
		if f.remoteSHA == "" {
			return "", nil
		}
		return f.remoteSHA + "\trefs/heads/" + f.branch + "\n", nil
	case "rev-list":
		if args[1] == "--count" {
			return "3\n", nil
		}
		return f.counts, nil
	case "cat-file":
		return "", nil
	case "push":
		return f.pushOutput, nil
	}
	return "", nil
}

func newPublishService(t *testing.T, mode config.Mode, fixture *publishFixture) (*Service, string) {
	t.Helper()
	svc, root := newTestService(t, mode)
	svc.WithGitHub(NewGitHubClient("https://api.github.test", "token", "acme", "org", "private"))
	svc.WithRunner(fixture.runner)
	return svc, root
}

func TestRepoPublishSuccessfulInitialAndNormalPush(t *testing.T) {
	for _, tc := range []struct {
		name       string
		remoteSHA  string
		counts     string
		wantPush   string
		wantExists string
	}{
		{"initial", "", "", "push -u origin main", "remote_branch_exists: false"},
		{"normal", strings.Repeat("b", 40), "0 1", "push origin main", "remote_branch_exists: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git", remoteSHA: tc.remoteSHA, counts: tc.counts, pushOutput: "pushed"}
			svc, root := newPublishService(t, config.ModeAsk, f)
			preview, err := svc.RepoPublishPreview(root, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(preview, tc.wantExists) || !strings.Contains(preview, "sanitized_url: https://github.com/acme/demo.git") {
				t.Fatalf("bad preview:\n%s", preview)
			}
			planID := field(preview, "plan_id")
			out, err := svc.RepoPublish(planID, false)
			if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") {
				t.Fatalf("approval gate: out=%q err=%v", out, err)
			}
			if _, err := svc.RepoPublish(planID, true); err != nil {
				t.Fatal(err)
			}
			last := strings.Join(f.commands[len(f.commands)-1], " ")
			if last != tc.wantPush {
				t.Fatalf("push command = %q, want %q", last, tc.wantPush)
			}
			if _, err := svc.RepoPublish(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
				t.Fatalf("replay must fail: %v", err)
			}
		})
	}
}

func TestRepoPublishUsesConfiguredTokenOnlyForBoundGitHubHTTPSRemote(t *testing.T) {
	f := &publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git", pushOutput: "pushed"}
	svc, root := newPublishService(t, config.ModeAllow, f)
	var gotToken string
	var calls int
	svc.githubRun = func(_ context.Context, _ string, _ string, args []string, token string) (string, error) {
		calls++
		gotToken = token
		return f.runner(context.Background(), "", "git", args)
	}
	preview, err := svc.RepoPublishPreview(root, "origin", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RepoPublish(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || gotToken != "token" {
		t.Fatalf("GitHub HTTPS runner calls=%d token=%q, want two calls with configured token", calls, gotToken)
	}
	for _, args := range f.commands {
		if strings.Contains(strings.Join(args, " "), "token") {
			t.Fatalf("token leaked into git argv: %#v", args)
		}
	}
}

func TestRepoPublishPreviewRejectsUnsafeStateAndArguments(t *testing.T) {
	base := publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git", counts: "0 1"}
	for _, tc := range []struct {
		name   string
		mutate func(*publishFixture)
		remote string
		branch string
	}{
		{"dirty", func(f *publishFixture) { f.dirty = true }, "origin", "main"},
		{"detached", func(f *publishFixture) { f.branch = "(detached)" }, "origin", ""},
		{"credentials", func(f *publishFixture) { f.remoteURL = "https://user:pass@github.com/acme/demo.git" }, "origin", "main"},
		{"wrong-host", func(f *publishFixture) { f.remoteURL = "https://example.com/acme/demo.git" }, "origin", "main"},
		{"option-remote", func(*publishFixture) {}, "--force", "main"},
		{"option-branch", func(*publishFixture) {}, "origin", "--mirror"},
		{"other-branch", func(*publishFixture) {}, "origin", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.mutate(&f)
			svc, root := newPublishService(t, config.ModeAllow, &f)
			if _, err := svc.RepoPublishPreview(root, tc.remote, tc.branch); err == nil {
				t.Fatal("unsafe publication preview unexpectedly succeeded")
			}
		})
	}
}

func TestRepoPublishRejectsDivergenceChangedStateAndExpiry(t *testing.T) {
	f := &publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git", remoteSHA: strings.Repeat("b", 40), counts: "1 1"}
	svc, root := newPublishService(t, config.ModeAllow, f)
	if _, err := svc.RepoPublishPreview(root, "origin", "main"); err == nil || !strings.Contains(err.Error(), "behind") {
		t.Fatalf("divergence must fail: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*publishFixture)
		want   string
	}{
		{"head", func(f *publishFixture) { f.head = strings.Repeat("c", 40) }, "HEAD changed"},
		{"branch", func(f *publishFixture) { f.branch = "other" }, "branch changed"},
		{"remote-url", func(f *publishFixture) { f.remoteURL = "https://github.com/acme/changed.git" }, "remote changed"},
		{"remote-sha", func(f *publishFixture) { f.remoteSHA = strings.Repeat("d", 40) }, "remote branch changed"},
		{"dirty", func(f *publishFixture) { f.dirty = true }, "working tree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git", counts: ""}
			svc, root := newPublishService(t, config.ModeAllow, fixture)
			preview, err := svc.RepoPublishPreview(root, "origin", "main")
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(fixture)
			if _, err := svc.RepoPublish(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}

	fixture := &publishFixture{head: strings.Repeat("a", 40), branch: "main", remoteURL: "https://github.com/acme/demo.git"}
	svc, root = newPublishService(t, config.ModeAllow, fixture)
	preview, _ := svc.RepoPublishPreview(root, "origin", "main")
	planID := field(preview, "plan_id")
	svc.plans.mu.Lock()
	svc.plans.plans[planID].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.RepoPublish(planID, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired publication plan must fail: %v", err)
	}
}
