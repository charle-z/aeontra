package brain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	gitOutputLimit     = 64 << 10
	gitIgnoreLimit     = 4 << 10
	gitCriticalTimeout = 15 * time.Second
	gitIgnoreLine      = "/.cache/"
)

var gitObjectPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type gitCommandRunner interface {
	Run(ctx context.Context, root string, env []string, args ...string) (string, error)
}

type execGitRunner struct {
	path string
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
	total  int
}

func (w *limitedBuffer) Write(data []byte) (int, error) {
	w.total += len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = w.buffer.Write(data[:remaining])
		} else {
			_, _ = w.buffer.Write(data)
		}
	}
	return len(data), nil
}

func (w *limitedBuffer) String() string { return w.buffer.String() }

func newExecGitRunner(root string) (*execGitRunner, error) {
	candidate, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("brain: git executable is unavailable")
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return nil, errors.New("brain: git executable path is unavailable")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(candidate); resolveErr == nil {
		candidate = resolved
	}
	relative, err := filepath.Rel(root, candidate)
	if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, errors.New("brain: git executable must be outside the Brain root")
	}
	return &execGitRunner{path: candidate}, nil
}

func (r *execGitRunner) Run(ctx context.Context, root string, extraEnv []string, args ...string) (string, error) {
	if r == nil || r.path == "" {
		return "", errors.New("brain: git runner is unavailable")
	}
	command := exec.CommandContext(ctx, r.path, args...)
	command.Dir = root
	command.Env = gitEnvironment(extraEnv)
	var output limitedBuffer
	output.limit = gitOutputLimit
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return "", errors.New("brain: local Git command failed")
	}
	if output.total > gitOutputLimit {
		return "", errors.New("brain: local Git output exceeded the safe limit")
	}
	return strings.TrimSpace(output.String()), nil
}

func gitEnvironment(extra []string) []string {
	allowed := map[string]bool{
		"PATH": true, "SystemRoot": true, "WINDIR": true,
		"TEMP": true, "TMP": true, "TMPDIR": true,
		"LANG": true, "LC_ALL": true,
	}
	environment := make([]string, 0, len(allowed)+16+len(extra))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			environment = append(environment, entry)
		}
	}
	environment = append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
	for _, entry := range extra {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" || strings.ContainsAny(name, "\x00\r\n") || strings.ContainsAny(entry, "\x00\r\n") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

func gitArgs(command string, rest ...string) []string {
	arguments := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "commit.gpgsign=false",
		"-c", "protocol.allow=never",
		"-c", "protocol.file.allow=never",
		command,
	}
	return append(arguments, rest...)
}

func gitSubcommand(arguments []string) string {
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "-c" {
			index++
			continue
		}
		if strings.HasPrefix(arguments[index], "-") {
			continue
		}
		return arguments[index]
	}
	return ""
}

type localGit struct {
	root   string
	runner gitCommandRunner
}

func (g *localGit) run(ctx context.Context, env []string, command string, args ...string) (string, error) {
	return g.runner.Run(ctx, g.root, env, gitArgs(command, args...)...)
}

func (g *localGit) initialize(ctx context.Context, now time.Time) error {
	gitPath := filepath.Join(g.root, ".git")
	info, err := os.Lstat(gitPath)
	newRepository := errors.Is(err, os.ErrNotExist)
	if err != nil && !newRepository {
		return errors.New("brain: local Git metadata is unavailable")
	}
	if !newRepository && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return errors.New("brain: local Git metadata must be a directory")
	}
	ignorePath := filepath.Join(g.root, ".gitignore")
	if newRepository {
		if err := atomicWritePrivate(ignorePath, []byte(gitIgnoreLine+"\n")); err != nil {
			return err
		}
		if _, err := g.run(ctx, nil, "init", "--quiet", "--initial-branch=main", "--template="); err != nil {
			return err
		}
	} else if err := verifyCacheIgnored(ignorePath); err != nil {
		return err
	}
	if err := secureGitDirectory(gitPath); err != nil {
		return err
	}

	top, err := g.run(ctx, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("brain: local Git repository is invalid")
	}
	resolvedTop, err := filepath.Abs(filepath.Clean(top))
	if err != nil || resolvedTop != g.root {
		return errors.New("brain: local Git repository root mismatch")
	}
	if _, err := g.run(ctx, nil, "symbolic-ref", "HEAD"); err != nil {
		return errors.New("brain: local Git repository must have an attached branch")
	}
	remotes, err := g.run(ctx, nil, "remote")
	if err != nil {
		return errors.New("brain: local Git remote state is unavailable")
	}
	if strings.TrimSpace(remotes) != "" {
		return errors.New("brain: local Git repository must not have a remote")
	}
	if _, err := g.run(ctx, nil, "rev-parse", "--verify", "HEAD"); err != nil {
		if _, err := g.commitPath(ctx, ".gitignore", AuthorOwner, "brain: initialize local store", now); err != nil {
			return err
		}
	}
	return verifyCacheIgnored(ignorePath)
}

func secureGitDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("brain: local Git metadata must be a private directory")
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(path, 0o700); err != nil {
			return errors.New("brain: local Git metadata permissions could not be secured")
		}
		info, err = os.Lstat(path)
		if err != nil || info.Mode().Perm() != 0o700 {
			return errors.New("brain: local Git metadata permissions are unsafe")
		}
	}
	return nil
}

func verifyCacheIgnored(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > gitIgnoreLimit {
		return errors.New("brain: .gitignore must be a private regular file that ignores the cache")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("brain: .gitignore must ignore the cache")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, gitIgnoreLimit+1))
	if err != nil || len(data) > gitIgnoreLimit {
		return errors.New("brain: .gitignore must ignore the cache")
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == gitIgnoreLine {
			return nil
		}
	}
	return errors.New("brain: .gitignore must ignore the cache")
}

func (g *localGit) verifyMetadata() error {
	if g == nil || g.root == "" {
		return errors.New("brain: local Git repository is unavailable")
	}
	if err := secureGitDirectory(filepath.Join(g.root, ".git")); err != nil {
		return err
	}
	return verifyCacheIgnored(filepath.Join(g.root, ".gitignore"))
}

func (g *localGit) head(ctx context.Context) (ref, commit string, exists bool, err error) {
	ref, err = g.run(ctx, nil, "symbolic-ref", "HEAD")
	if err != nil || !strings.HasPrefix(ref, "refs/heads/") || strings.ContainsAny(ref, "\x00\r\n ") {
		return "", "", false, errors.New("brain: local Git branch is invalid")
	}
	commit, err = g.run(ctx, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ref, "", false, nil
	}
	if !gitObjectPattern.MatchString(commit) {
		return "", "", false, errors.New("brain: local Git HEAD is invalid")
	}
	return ref, commit, true, nil
}

func (g *localGit) resetIndex(head string, exists bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if exists {
		_, _ = g.run(ctx, nil, "read-tree", head)
		return
	}
	_, _ = g.run(ctx, nil, "read-tree", "--empty")
}

func (g *localGit) commitPath(ctx context.Context, relative, author, message string, now time.Time) (commit string, err error) {
	if relative != ".gitignore" {
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 2 || parts[0] != WorkingDir || filepath.Ext(parts[1]) != ".md" {
			return "", errors.New("brain: local Git path is not allowed")
		}
		slug := strings.TrimSuffix(parts[1], ".md")
		if err := ValidateSlug(slug); err != nil {
			return "", err
		}
	}
	ref, head, exists, err := g.head(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			g.resetIndex(head, exists)
		}
	}()
	if exists {
		if _, err = g.run(ctx, nil, "read-tree", head); err != nil {
			return "", err
		}
	} else if _, err = g.run(ctx, nil, "read-tree", "--empty"); err != nil {
		return "", err
	}
	object, err := g.run(ctx, nil, "hash-object", "-w", "--no-filters", "--", filepath.FromSlash(relative))
	if err != nil || !gitObjectPattern.MatchString(object) {
		return "", errors.New("brain: local Git object creation failed")
	}
	cacheInfo := "100644," + object + "," + filepath.ToSlash(relative)
	if _, err = g.run(ctx, nil, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
		return "", err
	}
	tree, err := g.run(ctx, nil, "write-tree")
	if err != nil || !gitObjectPattern.MatchString(tree) {
		return "", errors.New("brain: local Git tree creation failed")
	}
	commitArgs := []string{tree}
	if exists {
		commitArgs = append(commitArgs, "-p", head)
	}
	commitArgs = append(commitArgs, "-m", message)
	environment := gitIdentityEnvironment(author, now)
	commit, err = g.run(ctx, environment, "commit-tree", commitArgs...)
	if err != nil || !gitObjectPattern.MatchString(commit) {
		return "", errors.New("brain: local Git commit creation failed")
	}
	updateArgs := []string{ref, commit}
	if exists {
		updateArgs = append(updateArgs, head)
	}
	if _, updateErr := g.run(ctx, nil, "update-ref", updateArgs...); updateErr != nil {
		verifyContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		current, verifyErr := g.run(verifyContext, nil, "rev-parse", "--verify", ref)
		if verifyErr == nil && current == commit {
			err = nil
			return commit, nil
		}
		err = updateErr
		return "", err
	}
	return commit, nil
}

func gitIdentityEnvironment(author string, now time.Time) []string {
	name := "MCP Devbox Brain"
	if strings.HasPrefix(author, "agent:") {
		name += " " + author
	}
	stamp := now.UTC().Format(time.RFC3339)
	return []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=brain@localhost",
		"GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_NAME=MCP Devbox Brain",
		"GIT_COMMITTER_EMAIL=brain@localhost",
		"GIT_COMMITTER_DATE=" + stamp,
	}
}

func atomicWritePrivate(path string, data []byte) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".brain-write-")
	if err != nil {
		return errors.New("brain: private source could not be created")
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return errors.New("brain: private source permissions could not be secured")
	}
	if _, err := io.Copy(temp, bytes.NewReader(data)); err != nil {
		cleanup()
		return errors.New("brain: private source could not be written")
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return errors.New("brain: private source could not be synchronized")
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return errors.New("brain: private source could not be closed")
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return errors.New("brain: private source could not be replaced")
	}
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}
