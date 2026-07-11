package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runGitHubRemote uses the configured GitHub token only for a credential-free,
// owner-bound https://github.com remote. SSH remotes deliberately keep their existing
// SSH-agent/deploy-key behavior, and every other remote uses the normal runner.
func (s *Service) runGitHubRemote(ctx context.Context, dir, remoteURL string, args []string) (string, error) {
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_ASKPASS="+path, "GIT_TERMINAL_PROMPT=0", "MCP_DEVBOX_GIT_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
