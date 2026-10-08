package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var githubHTTPSObjectID = regexp.MustCompile(`^[a-f0-9]{40,64}$`)

// runGitHubRemote uses the configured GitHub token only for a credential-free,
// owner-bound https://github.com remote. SSH remotes deliberately keep their existing
// SSH-agent/deploy-key behavior, and every other remote uses the normal runner.
func (s *GitCapability) runGitHubRemote(ctx context.Context, dir, remoteURL string, args []string) (string, error) {
	if s.github == nil || strings.TrimSpace(s.github.token) == "" {
		return s.run(ctx, dir, "git", args)
	}
	normalized, err := sanitizeGitHubRemoteURL(remoteURL, s.github.owner)
	if err != nil || normalized != remoteURL || !strings.HasPrefix(normalized, "https://github.com/") {
		return s.run(ctx, dir, "git", args)
	}
	return s.githubRun(ctx, dir, "git", args, s.github.token)
}

// execGitHubHTTPSRunner supplies a public GitHub username and the token through a
// short-lived askpass program. The remote stays credential-free and neither the token
// nor an Authorization header is placed in argv, output, audit, or persistent config.
func execGitHubHTTPSRunner(ctx context.Context, dir, prog string, args []string, token string) (string, error) {
	if prog != "git" || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("GitHub HTTPS authentication requires git and a token")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil || !filepath.IsAbs(gitPath) {
		return "", errors.New("trusted Git executable is unavailable")
	}
	askpass, err := os.CreateTemp("", "mcp-devbox-git-askpass-*")
	if err != nil {
		return "", fmt.Errorf("create temporary Git credential helper: %w", err)
	}
	path := askpass.Name()
	defer os.Remove(path)
	// The token remains in the child environment, not in this file. Git invokes this
	// fixed script only for its username/password prompts.
	if _, err := askpass.WriteString("#!/bin/sh\ncase \"$1\" in\n  *Username*) printf '%s' x-access-token ;;\n  *) printf '%s' \"$MCP_DEVBOX_GIT_TOKEN\" ;;\nesac\n"); err != nil {
		askpass.Close()
		return "", fmt.Errorf("write temporary Git credential helper: %w", err)
	}
	if err := askpass.Close(); err != nil {
		return "", fmt.Errorf("close temporary Git credential helper: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", fmt.Errorf("secure temporary Git credential helper: %w", err)
	}
	remoteURL, operation, err := validateGitHubHTTPSArguments(args)
	if err != nil {
		return "", err
	}
	transportRoot, err := os.MkdirTemp("", "mcp-devbox-git-transport-*")
	if err != nil {
		return "", fmt.Errorf("create isolated Git transport root: %w", err)
	}
	defer os.RemoveAll(transportRoot)
	if err := os.Chmod(transportRoot, 0o700); err != nil {
		return "", fmt.Errorf("secure isolated Git transport root: %w", err)
	}
	commandDir := transportRoot
	if operation == "push" {
		if err := prepareGitHubPushRepository(ctx, gitPath, dir, transportRoot); err != nil {
			return "", err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	protected := []string{
		"-c", "credential.helper=",
		"-c", "credential." + remoteURL + ".helper=",
		"-c", "credential.useHttpPath=true",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ext.allow=never",
		"-c", "http.proxy=",
		"-c", "http.extraHeader=",
		"-c", "http.cookieFile=",
		"-c", "http.saveCookies=false",
		"-c", "http.sslVerify=true",
		"-c", "http.followRedirects=false",
	}
	cmd := exec.CommandContext(ctx, gitPath, append(protected, args...)...)
	cmd.Dir = commandDir
	cmd.Env = []string{
		"PATH=" + filepath.Dir(gitPath), "HOME=" + transportRoot,
		"LANG=C", "LC_ALL=C",
		"GIT_ASKPASS=" + path, "GIT_TERMINAL_PROMPT=0", "MCP_DEVBOX_GIT_TOKEN=" + token,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "PAGER=cat",
	}
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(string(out), token, "[REDACTED]"), err
}

func validateGitHubHTTPSArguments(args []string) (string, string, error) {
	if len(args) != 4 {
		return "", "", errors.New("GitHub HTTPS operation is invalid")
	}
	remoteURL := args[2]
	if !strings.HasPrefix(remoteURL, "https://github.com/") || strings.ContainsAny(remoteURL, "\r\n\t @") || !strings.HasSuffix(remoteURL, ".git") {
		return "", "", errors.New("GitHub HTTPS target is invalid")
	}
	switch args[0] {
	case "ls-remote":
		if args[1] != "--heads" || !strings.HasPrefix(args[3], "refs/heads/") {
			return "", "", errors.New("GitHub HTTPS inspection is invalid")
		}
		return remoteURL, "ls-remote", nil
	case "push":
		parts := strings.Split(args[3], ":")
		if args[1] != "--porcelain" || len(parts) != 2 || !githubHTTPSObjectID.MatchString(parts[0]) || !strings.HasPrefix(parts[1], "refs/heads/") {
			return "", "", errors.New("GitHub HTTPS publication is invalid")
		}
		return remoteURL, "push", nil
	default:
		return "", "", errors.New("GitHub HTTPS operation is unsupported")
	}
}

func prepareGitHubPushRepository(ctx context.Context, prog, sourceDir, transportRoot string) error {
	objects, err := gitObjectDirectory(sourceDir)
	if err != nil {
		return err
	}
	info, err := os.Lstat(objects)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("GitHub publication object database is unsafe")
	}
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	initCommand := exec.CommandContext(initCtx, prog, "init", "--bare", "--quiet", transportRoot)
	initCommand.Dir = filepath.Dir(transportRoot)
	initCommand.Env = []string{
		"PATH=" + filepath.Dir(prog), "HOME=" + transportRoot,
		"LANG=C", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
	}
	if output, err := initCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("initialize isolated Git transport: %w: %s", err, output)
	}
	alternates := filepath.Join(transportRoot, "objects", "info", "alternates")
	if err := os.WriteFile(alternates, []byte(objects+"\n"), 0o600); err != nil {
		return errors.New("configure isolated Git object access failed")
	}
	return nil
}

func gitObjectDirectory(sourceDir string) (string, error) {
	metadata := filepath.Join(filepath.Clean(sourceDir), ".git")
	info, err := os.Lstat(metadata)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("GitHub publication metadata is unsafe")
	}
	commonDir := metadata
	if !info.IsDir() {
		if !info.Mode().IsRegular() || info.Size() < 8 || info.Size() > 4096 {
			return "", errors.New("GitHub publication worktree metadata is unsafe")
		}
		body, err := os.ReadFile(metadata)
		if err != nil {
			return "", errors.New("GitHub publication worktree metadata is unavailable")
		}
		value := strings.TrimSpace(string(body))
		if !strings.HasPrefix(value, "gitdir: ") || strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("GitHub publication worktree metadata is invalid")
		}
		gitDir := strings.TrimSpace(strings.TrimPrefix(value, "gitdir: "))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(sourceDir, gitDir)
		}
		gitDir = filepath.Clean(gitDir)
		worktreesDir := filepath.Dir(gitDir)
		if filepath.Base(worktreesDir) != "worktrees" {
			return "", errors.New("GitHub publication worktree relationship is invalid")
		}
		commonDir = filepath.Dir(worktreesDir)
		gitDirInfo, err := os.Lstat(gitDir)
		commonInfo, commonErr := os.Lstat(commonDir)
		if err != nil || commonErr != nil || !gitDirInfo.IsDir() || !commonInfo.IsDir() || gitDirInfo.Mode()&os.ModeSymlink != 0 || commonInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("GitHub publication worktree relationship is unsafe")
		}
	}
	objects := filepath.Join(commonDir, "objects")
	objectsInfo, err := os.Lstat(objects)
	if err != nil || !objectsInfo.IsDir() || objectsInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("GitHub publication object database is unsafe")
	}
	return objects, nil
}
