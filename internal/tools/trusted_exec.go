package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrWorkspaceExecutable     = errors.New("tools: refusing workspace-controlled executable")
	ErrUntrustedExecutablePath = errors.New("tools: executable resolved to an untrusted path")
)

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
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
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
