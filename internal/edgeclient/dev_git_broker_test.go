//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDevGitRunner struct {
	workspace, owner, repository, branch, head, remoteHead, token string
	status                                                        string
	calls                                                         [][]string
}

func (runner *fakeDevGitRunner) Run(_ context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	runner.calls = append(runner.calls, append([]string(nil), args...))
	runner.token = credential.Token
	joined := strings.Join(args, " ")
	if strings.Contains(joined, credential.Token) {
		return "", errors.New("token appeared in argv")
	}
	switch {
	case len(args) > 0 && args[0] == "clone":
		target := filepath.Join(dir, args[len(args)-1])
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o700); err != nil {
			return "", err
		}
		return "cloned", nil
	case joined == "remote get-url origin":
		return "https://github.com/" + runner.owner + "/" + runner.repository + ".git\n", nil
	case joined == "remote get-url --push origin":
		return "https://github.com/" + runner.owner + "/" + runner.repository + ".git\n", nil
	case joined == "branch --show-current":
		return runner.branch + "\n", nil
	case joined == "rev-parse HEAD":
		return runner.head + "\n", nil
	case joined == "status --porcelain=v1 --untracked-files=normal":
		return runner.status, nil
	case strings.HasPrefix(joined, "ls-remote --heads origin "):
		if runner.remoteHead == "" {
			return "", nil
		}
		return runner.remoteHead + "\trefs/heads/" + runner.branch + "\n", nil
	case strings.HasPrefix(joined, "fetch --no-tags origin "), strings.HasPrefix(joined, "merge-base --is-ancestor "):
		return "", nil
	case joined == "push --porcelain origin "+runner.branch:
		runner.remoteHead = runner.head
		return "published", nil
	default:
		return "", errors.New("unexpected git call: " + joined)
	}
}

func newDevGitBrokerTest(t *testing.T) (*devGitBroker, *fakeDevGitRunner) {
	t.Helper()
	workspacePath := t.TempDir()
	workspace := Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	head := strings.Repeat("a", 40)
	runner := &fakeDevGitRunner{workspace: workspacePath, owner: "charle-z", repository: "private-repo", branch: "feature/test", head: head, remoteHead: strings.Repeat("b", 40)}
	credential := GitHubCredential{SchemaVersion: 1, Owner: runner.owner, Token: "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"}
	broker := &devGitBroker{config: DevGitBrokerConfig{StateRoot: t.TempDir(), Workspace: workspace, RuntimeID: "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpiresAt: time.Now().Add(time.Hour), Credential: credential, Runner: runner}, plans: make(map[string]devGitPublishPlan), now: time.Now}
	return broker, runner
}

func TestDevGitBrokerClonesPrivateOwnerBoundRepositoryWithoutExposingToken(t *testing.T) {
	broker, runner := newDevGitBrokerTest(t)
	response, err := broker.clone(t.Context(), DevGitRequest{WorkspaceID: broker.config.Workspace.ID, Repository: runner.repository, Directory: "project", Branch: runner.branch})
	if err != nil || response.Status != "ok" || response.Head != runner.head || response.Directory != "project" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if runner.token != broker.config.Credential.Token {
		t.Fatal("runner did not receive local credential")
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), broker.config.Credential.Token) {
			t.Fatal("token appeared in Git arguments")
		}
	}
}

func TestDevGitPublishRequiresSingleUsePlanAndRevalidatesRemote(t *testing.T) {
	broker, runner := newDevGitBrokerTest(t)
	runner.status = "?? .mcp-devbox/runtime/home/.config/go/telemetry/local/weekends\n"
	dir := filepath.Join(broker.config.Workspace.Path, "project")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	preview, err := broker.preview(t.Context(), DevGitRequest{WorkspaceID: broker.config.Workspace.ID, Directory: "project", Branch: runner.branch})
	if err != nil || !devGitPlanPattern.MatchString(preview.PlanID) || preview.Head != runner.head || preview.RemoteHead != runner.remoteHead {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	published, err := broker.publish(t.Context(), DevGitRequest{WorkspaceID: broker.config.Workspace.ID, PlanID: preview.PlanID})
	if err != nil || !published.Published || published.RemoteHead != runner.head {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	if _, err := broker.publish(t.Context(), DevGitRequest{WorkspaceID: broker.config.Workspace.ID, PlanID: preview.PlanID}); err == nil {
		t.Fatal("replayed publication plan")
	}
}

func TestDevGitBrokerRejectsTraversalAndCredentialsInRequest(t *testing.T) {
	broker, runner := newDevGitBrokerTest(t)
	for _, request := range []DevGitRequest{
		{WorkspaceID: broker.config.Workspace.ID, Repository: runner.repository, Directory: "../escape", Branch: runner.branch},
		{WorkspaceID: broker.config.Workspace.ID, Repository: "https://token@github.com/x/y", Directory: "project", Branch: runner.branch},
		{WorkspaceID: broker.config.Workspace.ID, Repository: runner.repository, Directory: "project", Branch: "--force"},
	} {
		if _, err := broker.clone(context.Background(), request); err == nil {
			t.Fatalf("accepted unsafe request=%+v", request)
		}
	}
}

func TestDevGitOutputRedactionPreservesLocalOutputWithoutCredential(t *testing.T) {
	const output = "b70efb6c12fe15d7138ea40d033043092c32fc66\n"
	if got := redactDevGitCommandOutput(output, ""); got != output {
		t.Fatalf("local output corrupted: %q", got)
	}
	if got := redactDevGitCommandOutput("prefix-private-suffix", "private"); got != "prefix-[REDACTED]-suffix" {
		t.Fatalf("credential was not redacted: %q", got)
	}
}
