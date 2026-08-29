//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type execDevGitCommandRunner struct {
	stateRoot string
	toolPath  string
}

func StartDevGitBroker(ctx context.Context, config DevGitBrokerConfig) (<-chan error, error) {
	if config.Workspace.Profile != WorkspaceProfileLinuxWorkcell || config.Workspace.Mode != WorkspaceModeDev ||
		!remoteRuntimeIDPattern.MatchString(config.RuntimeID) || config.SocketPath == "" || config.StateRoot == "" || !config.ExpiresAt.After(time.Now().UTC()) ||
		config.Credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(config.Credential.Owner) || !validGitHubToken(config.Credential.Token) {
		return nil, errors.New("development Git broker configuration is invalid")
	}
	if filepath.Base(config.SocketPath) != DevGitBrokerSocketName {
		return nil, errors.New("development Git broker socket name is invalid")
	}
	if err := prepareDriverSocketParent(filepath.Dir(config.SocketPath)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(config.SocketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUID(info) {
			return nil, errors.New("existing development Git broker socket is unsafe")
		}
		_ = os.Remove(config.SocketPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("development Git broker socket is unavailable")
	}
	listener, err := net.Listen("unix", config.SocketPath)
	if err != nil {
		return nil, errors.New("development Git broker could not listen")
	}
	if err := os.Chmod(config.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(config.SocketPath)
		return nil, errors.New("development Git broker socket permissions failed")
	}
	if config.Runner == nil {
		config.Runner = execDevGitCommandRunner{stateRoot: config.StateRoot, toolPath: config.ToolPath}
	}
	broker := &devGitBroker{config: config, plans: make(map[string]devGitPublishPlan), now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/clone", broker.cloneHTTP)
	mux.HandleFunc("POST /v1/publish-preview", broker.previewHTTP)
	mux.HandleFunc("POST /v1/publish", broker.publishHTTP)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() {
		defer os.Remove(config.SocketPath)
		errCh := make(chan error, 1)
		go func() { errCh <- server.Serve(listener) }()
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
			_ = server.Close()
			done <- ctx.Err()
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				done <- nil
			} else {
				done <- err
			}
		}
	}()
	return done, nil
}

func (broker *devGitBroker) cloneHTTP(writer http.ResponseWriter, request *http.Request) {
	if !broker.acceptHTTP(request) {
		writeDevGitResponse(writer, http.StatusUnsupportedMediaType, DevGitResponse{Status: "error", ErrorCode: "clone_rejected"})
		return
	}
	input, err := decodeDevGitRequest(request.Body)
	if err == nil {
		var response DevGitResponse
		response, err = broker.clone(request.Context(), input)
		if err == nil {
			writeDevGitResponse(writer, http.StatusOK, response)
			return
		}
	}
	writeDevGitResponse(writer, http.StatusBadRequest, DevGitResponse{Status: "error", ErrorCode: "clone_rejected"})
}

func (broker *devGitBroker) previewHTTP(writer http.ResponseWriter, request *http.Request) {
	if !broker.acceptHTTP(request) {
		writeDevGitResponse(writer, http.StatusUnsupportedMediaType, DevGitResponse{Status: "error", ErrorCode: "publish_preview_rejected"})
		return
	}
	input, err := decodeDevGitRequest(request.Body)
	if err == nil {
		var response DevGitResponse
		response, err = broker.preview(request.Context(), input)
		if err == nil {
			writeDevGitResponse(writer, http.StatusOK, response)
			return
		}
	}
	writeDevGitResponse(writer, http.StatusConflict, DevGitResponse{Status: "error", ErrorCode: "publish_preview_rejected"})
}

func (broker *devGitBroker) publishHTTP(writer http.ResponseWriter, request *http.Request) {
	if !broker.acceptHTTP(request) {
		writeDevGitResponse(writer, http.StatusUnsupportedMediaType, DevGitResponse{Status: "error", ErrorCode: "publish_rejected"})
		return
	}
	input, err := decodeDevGitRequest(request.Body)
	if err == nil {
		var response DevGitResponse
		response, err = broker.publish(request.Context(), input)
		if err == nil {
			writeDevGitResponse(writer, http.StatusOK, response)
			return
		}
	}
	writeDevGitResponse(writer, http.StatusConflict, DevGitResponse{Status: "error", ErrorCode: "publish_rejected"})
}

func (broker *devGitBroker) acceptHTTP(request *http.Request) bool {
	return request.Header.Get("Content-Type") == "application/json" && broker.calls.Add(1) <= 64 && broker.config.ExpiresAt.After(broker.now().UTC())
}

func writeDevGitResponse(writer http.ResponseWriter, status int, response DevGitResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(response)
}

func (broker *devGitBroker) clone(ctx context.Context, request DevGitRequest) (DevGitResponse, error) {
	request, err := validateDevGitCloneRequest(request, broker.config.Workspace)
	if err != nil {
		return DevGitResponse{}, err
	}
	target, err := broker.repositoryPath(request.Directory)
	if err != nil {
		return DevGitResponse{}, err
	}
	remoteURL := broker.remoteURL(request.Repository)
	if _, err := os.Lstat(target); err == nil {
		return broker.existingClone(ctx, request, target, remoteURL)
	} else if !errors.Is(err, os.ErrNotExist) {
		return DevGitResponse{}, errors.New("development clone target is unavailable")
	}
	args := []string{"clone", "--single-branch", "--branch", request.Branch, "--", remoteURL, target}
	if _, err := broker.config.Runner.Run(ctx, broker.config.Workspace.Path, args, broker.config.Credential); err != nil {
		if pathInside(broker.config.Workspace.Path, target) {
			_ = os.RemoveAll(target)
		}
		return DevGitResponse{}, errors.New("development Git clone failed")
	}
	return broker.existingClone(ctx, request, target, remoteURL)
}

func (broker *devGitBroker) existingClone(ctx context.Context, request DevGitRequest, target, remoteURL string) (DevGitResponse, error) {
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return DevGitResponse{}, errors.New("development clone target is unsafe")
	}
	remote, err := broker.configuredRemoteURL(ctx, target)
	if err != nil || remote != remoteURL {
		return DevGitResponse{}, errors.New("development clone remote does not match")
	}
	branch, err := broker.git(ctx, target, "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) != request.Branch {
		return DevGitResponse{}, errors.New("development clone branch does not match")
	}
	head, err := broker.head(ctx, target)
	if err != nil {
		return DevGitResponse{}, err
	}
	return DevGitResponse{Status: "ok", WorkspaceID: request.WorkspaceID, Repository: request.Repository, Directory: request.Directory, Branch: request.Branch, Head: head}, nil
}

func (broker *devGitBroker) preview(ctx context.Context, request DevGitRequest) (DevGitResponse, error) {
	request, err := validateDevGitPreviewRequest(request, broker.config.Workspace)
	if err != nil {
		return DevGitResponse{}, err
	}
	dir, remoteURL, head, remoteHead, err := broker.revalidate(ctx, request.Directory, request.Branch)
	if err != nil {
		return DevGitResponse{}, err
	}
	_ = dir
	id, err := newDevGitPlanID()
	if err != nil {
		return DevGitResponse{}, errors.New("development publication plan generation failed")
	}
	expires := broker.now().UTC().Add(devGitPlanTTL)
	broker.mu.Lock()
	broker.plans[id] = devGitPublishPlan{ID: id, WorkspaceID: request.WorkspaceID, Directory: request.Directory, Branch: request.Branch, Head: head, RemoteHead: remoteHead, RemoteURL: remoteURL, ExpiresAt: expires}
	broker.mu.Unlock()
	return DevGitResponse{Status: "ok", WorkspaceID: request.WorkspaceID, Directory: request.Directory, Branch: request.Branch, Head: head, RemoteHead: remoteHead, PlanID: id, ExpiresAt: expires}, nil
}

func (broker *devGitBroker) publish(ctx context.Context, request DevGitRequest) (DevGitResponse, error) {
	request, err := validateDevGitPublishRequest(request, broker.config.Workspace)
	if err != nil {
		return DevGitResponse{}, err
	}
	broker.mu.Lock()
	plan, ok := broker.plans[request.PlanID]
	available := ok && !plan.Used
	if available {
		plan.Used = true
		broker.plans[request.PlanID] = plan
	}
	broker.mu.Unlock()
	if !available || plan.WorkspaceID != request.WorkspaceID || !plan.ExpiresAt.After(broker.now().UTC()) {
		return DevGitResponse{}, errors.New("development publication plan is unavailable")
	}
	dir, remoteURL, head, remoteHead, err := broker.revalidate(ctx, plan.Directory, plan.Branch)
	if err != nil || remoteURL != plan.RemoteURL || head != plan.Head || remoteHead != plan.RemoteHead {
		return DevGitResponse{}, errors.New("development publication state changed")
	}
	transport, ok := broker.config.Runner.(devGitTransportRunner)
	if !ok {
		return DevGitResponse{}, errors.New("development Git transport boundary is unavailable")
	}
	if _, err := transport.PublishCommit(ctx, dir, remoteURL, head, plan.Branch, broker.config.Credential); err != nil {
		return DevGitResponse{}, errors.New("development Git publish failed")
	}
	after, err := broker.remoteHead(ctx, dir, remoteURL, plan.Branch)
	if err != nil || after != head {
		return DevGitResponse{}, errors.New("development Git publish verification failed")
	}
	return DevGitResponse{Status: "ok", WorkspaceID: request.WorkspaceID, Directory: plan.Directory, Branch: plan.Branch, Head: head, RemoteHead: after, Published: true}, nil
}

func (broker *devGitBroker) revalidate(ctx context.Context, directory, branch string) (string, string, string, string, error) {
	dir, err := broker.repositoryPath(directory)
	if err != nil {
		return "", "", "", "", err
	}
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", "", "", errors.New("development repository metadata is unsafe")
	}
	status, err := broker.git(ctx, dir, ProjectCheckoutStatusArgs()...)
	if err != nil || !ProjectCheckoutStatusClean(status) {
		return "", "", "", "", errors.New("development repository must be clean")
	}
	current, err := broker.git(ctx, dir, "branch", "--show-current")
	if err != nil || strings.TrimSpace(current) != branch {
		return "", "", "", "", errors.New("development branch changed")
	}
	head, err := broker.head(ctx, dir)
	if err != nil {
		return "", "", "", "", err
	}
	remoteURL, err := broker.configuredRemoteURL(ctx, dir)
	if err != nil || !broker.validRemoteURL(remoteURL) {
		return "", "", "", "", errors.New("development Git remote is not owner-bound")
	}
	remoteHead, err := broker.remoteHead(ctx, dir, remoteURL, branch)
	if err != nil {
		return "", "", "", "", err
	}
	if remoteHead != "" {
		transport, ok := broker.config.Runner.(devGitTransportRunner)
		if !ok || transport.VerifyRemoteAncestor(ctx, dir, remoteURL, branch, remoteHead, head, broker.config.Credential) != nil {
			return "", "", "", "", errors.New("development branch is behind or diverged")
		}
	}
	return dir, remoteURL, head, remoteHead, nil
}

func (broker *devGitBroker) head(ctx context.Context, dir string) (string, error) {
	output, err := broker.git(ctx, dir, "rev-parse", "HEAD")
	head := strings.TrimSpace(output)
	if err != nil || !devGitCommitPattern.MatchString(head) {
		return "", errors.New("development repository HEAD is invalid")
	}
	return head, nil
}

func (broker *devGitBroker) remoteHead(ctx context.Context, dir, remoteURL, branch string) (string, error) {
	output, err := broker.git(ctx, dir, "ls-remote", "--heads", remoteURL, "refs/heads/"+branch)
	if err != nil {
		return "", errors.New("development remote state is unavailable")
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch || !devGitCommitPattern.MatchString(fields[0]) {
		return "", errors.New("development remote state is invalid")
	}
	return fields[0], nil
}

func (broker *devGitBroker) configuredRemoteURL(ctx context.Context, dir string) (string, error) {
	output, err := broker.git(ctx, dir, devGitRemoteConfigArguments...)
	if err != nil {
		return "", errors.New("development Git remote configuration is unavailable")
	}
	var remoteURL string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || (fields[0] != "remote.origin.url" && fields[0] != "remote.origin.pushurl") {
			return "", errors.New("development Git remote configuration is invalid")
		}
		if remoteURL == "" {
			remoteURL = fields[1]
		} else if remoteURL != fields[1] {
			return "", errors.New("development Git fetch and push remotes differ")
		}
	}
	if remoteURL == "" {
		return "", errors.New("development Git remote configuration is empty")
	}
	return remoteURL, nil
}

func (broker *devGitBroker) git(ctx context.Context, dir string, args ...string) (string, error) {
	return broker.config.Runner.Run(ctx, dir, args, broker.config.Credential)
}

func (broker *devGitBroker) remoteURL(repository string) string {
	return "https://github.com/" + broker.config.Credential.Owner + "/" + repository + ".git"
}

func (broker *devGitBroker) validRemoteURL(remote string) bool {
	prefix := "https://github.com/" + broker.config.Credential.Owner + "/"
	if !strings.HasPrefix(remote, prefix) || !strings.HasSuffix(remote, ".git") {
		return false
	}
	return devGitSimplePattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(remote, prefix), ".git"))
}

func (runner execDevGitCommandRunner) Run(ctx context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	gitPath, ok := findSafeLinuxTool("git", runner.toolPath)
	if !ok {
		return "", errors.New("git is unavailable")
	}
	if devGitFastForwardArguments(args) {
		if credential != (GitHubCredential{}) {
			return "", errors.New("development Git fast-forward does not accept credentials")
		}
		return runContainedDevGitFastForward(ctx, dir, gitPath, runner.toolPath, args)
	}
	remoteURL, network, err := devGitNetworkCommand(args, credential.Owner)
	if err != nil {
		return "", err
	}
	if network && (credential.SchemaVersion != 1 || !validGitHubToken(credential.Token)) {
		return "", errors.New("development Git authority is unavailable")
	}
	secretRoot := filepath.Join(runner.stateRoot, "github-runtime")
	if err := preparePrivateRoot(secretRoot); err != nil {
		return "", errors.New("GitHub runtime root is unsafe")
	}
	askpassPath := ""
	if network {
		askpass, createErr := os.CreateTemp(secretRoot, ".askpass-*")
		if createErr != nil {
			return "", errors.New("GitHub askpass staging failed")
		}
		askpassPath = askpass.Name()
		defer os.Remove(askpassPath)
		if askpass.Chmod(0o700) != nil || func() error {
			_, writeErr := askpass.WriteString("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s' x-access-token ;; *) printf '%s' \"$MCP_DEVBOX_GITHUB_TOKEN\" ;; esac\n")
			return writeErr
		}() != nil || askpass.Close() != nil {
			return "", errors.New("GitHub askpass staging failed")
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, gitPath, devGitProtectedArguments(args, remoteURL)...)
	command.Dir = dir
	if len(args) > 0 && (args[0] == "clone" || args[0] == "ls-remote") {
		command.Dir = secretRoot
	}
	command.Env = []string{
		"PATH=" + runner.toolPath, "HOME=" + secretRoot, "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_OPTIONAL_LOCKS=0",
	}
	if !devGitReadsLocalConfig(args) {
		// Executable Git operations must not inherit transport, credential,
		// hook, filter, or include directives from the checkout.
		command.Env = append(command.Env, "GIT_CONFIG=/dev/null")
	}
	if network {
		command.Env = append(command.Env, "GIT_ASKPASS="+askpassPath, "MCP_DEVBOX_GITHUB_TOKEN="+credential.Token)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := &boundedHTBLabCapture{limit: 1 << 20}
	command.Stdout = output
	command.Stderr = output
	err = command.Run()
	redactionToken := ""
	if network {
		redactionToken = credential.Token
	}
	text := redactDevGitCommandOutput(output.buffer.String(), redactionToken)
	return text, err
}

func redactDevGitCommandOutput(output, token string) string {
	if token == "" {
		return output
	}
	return strings.ReplaceAll(output, token, "[REDACTED]")
}

func (runner execDevGitCommandRunner) VerifyRemoteAncestor(ctx context.Context, dir, remoteURL, branch, remoteHead, head string, credential GitHubCredential) error {
	if !validDevGitBranch(branch) || !devGitCommitPattern.MatchString(remoteHead) || !devGitCommitPattern.MatchString(head) {
		return errors.New("development Git ancestry binding is invalid")
	}
	transportRoot, cleanup, err := createDevGitTransportRoot(runner.stateRoot, dir)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runner.Run(ctx, transportRoot, []string{"init", "--bare", "--quiet"}, credential); err != nil {
		return errors.New("development Git transport initialization failed")
	}
	if err := configureDevGitAlternates(transportRoot, dir); err != nil {
		return err
	}
	if _, err := runner.Run(ctx, transportRoot, []string{"fetch", "--no-tags", remoteURL, "refs/heads/" + branch + ":refs/remotes/origin/" + branch}, credential); err != nil {
		return errors.New("development remote state could not be fetched")
	}
	if _, err := runner.Run(ctx, transportRoot, []string{"merge-base", "--is-ancestor", remoteHead, head}, credential); err != nil {
		return errors.New("development branch is behind or diverged")
	}
	return nil
}

func (runner execDevGitCommandRunner) PublishCommit(ctx context.Context, dir, remoteURL, head, branch string, credential GitHubCredential) (string, error) {
	if !validDevGitBranch(branch) || !devGitCommitPattern.MatchString(head) {
		return "", errors.New("development Git publication binding is invalid")
	}
	transportRoot, cleanup, err := createDevGitTransportRoot(runner.stateRoot, dir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if _, err := runner.Run(ctx, transportRoot, []string{"init", "--bare", "--quiet"}, credential); err != nil {
		return "", errors.New("development Git transport initialization failed")
	}
	if err := configureDevGitAlternates(transportRoot, dir); err != nil {
		return "", err
	}
	return runner.Run(ctx, transportRoot, []string{"push", "--porcelain", remoteURL, head + ":refs/heads/" + branch}, credential)
}
