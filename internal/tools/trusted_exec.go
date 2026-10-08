package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrWorkspaceExecutable     = errors.New("tools: refusing workspace-controlled executable")
	ErrUntrustedExecutablePath = errors.New("tools: executable resolved to an untrusted path")
)

const maxTrustedCommandOutputBytes = 1 << 20

type boundedCombinedOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedCombinedOutput) Write(p []byte) (int, error) {
	original := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buffer.Write(p[:remaining])
	}
	if remaining < len(p) {
		b.truncated = true
	}
	return original, nil
}

func (b *boundedCombinedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := b.buffer.String()
	if b.truncated {
		result += "\n[output truncated at 1048576 bytes]"
	}
	return result
}

type executableLookup func(string) (string, error)

func newExecRunner(roots []string) Runner {
	trustedRoots := append([]string(nil), roots...)
	return func(ctx context.Context, dir, prog string, args []string) (string, error) {
		resolved, err := resolveTrustedExecutableWith(prog, trustedRoots, exec.LookPath)
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolved, args...)
		cmd.Dir = dir
		output := &boundedCombinedOutput{limit: maxTrustedCommandOutputBytes}
		cmd.Stdout = output
		cmd.Stderr = output
		err = cmd.Run()
		return output.String(), err
	}
}

func newGitReadRunner(roots []string) Runner {
	trustedRoots := append([]string(nil), roots...)
	return func(ctx context.Context, dir, prog string, args []string) (string, error) {
		if prog != "git" {
			return "", errors.New("read-only Git runner accepts only git")
		}
		resolved, err := resolveTrustedExecutableWith(prog, trustedRoots, exec.LookPath)
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		protected := []string{
			"-c", "core.hooksPath=" + os.DevNull,
			"-c", "core.fsmonitor=false",
			"-c", "diff.external=",
			"-c", "diff.trustExitCode=false",
		}
		cmd := exec.CommandContext(ctx, resolved, append(protected, hardenGitReadArguments(args)...)...)
		cmd.Dir = dir
		cmd.Env = []string{
			"PATH=" + filepath.Dir(resolved),
			"HOME=" + os.TempDir(),
			"LANG=C", "LC_ALL=C",
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
			"GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_OPTIONAL_LOCKS=0",
			"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "PAGER=cat",
		}
		output := &boundedCombinedOutput{limit: maxTrustedCommandOutputBytes}
		cmd.Stdout, cmd.Stderr = output, output
		err = cmd.Run()
		return output.String(), err
	}
}

func hardenGitReadArguments(args []string) []string {
	if len(args) == 0 || args[0] != "diff" {
		return append([]string(nil), args...)
	}
	return append([]string{"diff", "--no-ext-diff", "--no-textconv"}, args[1:]...)
}

func resolveTrustedExecutableWith(prog string, roots []string, lookup executableLookup) (string, error) {
	resolved, err := lookup(prog)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("%w: %q", ErrUntrustedExecutablePath, resolved)
	}
	canonical := filepath.Clean(resolved)
	if real, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = filepath.Clean(real)
	}
	for _, root := range roots {
		if pathWithinRoot(canonical, root) {
			return "", fmt.Errorf("%w: %s", ErrWorkspaceExecutable, canonical)
		}
	}
	return canonical, nil
}

func pathWithinRoot(path, root string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return err == nil && rel == "."
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
