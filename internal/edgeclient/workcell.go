package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

type Stage struct {
	Name string
	Argv []string
}

type CommandRunner interface {
	Run(context.Context, string, []string, int64) (int, []byte, error)
}

type Workcell struct {
	Root     string
	Commands CommandRunner
}

func ResolveWorkspace(root, name string) (string, error) {
	root, err := ValidateWorkcellRoot(root)
	if err != nil {
		return "", err
	}
	if !workspaceNamePattern.MatchString(name) {
		return "", errors.New("workspace name is invalid")
	}
	workspace := filepath.Join(root, name)
	info, err := os.Lstat(workspace)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("workspace is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(workspace) {
		return "", errors.New("workspace is unsafe")
	}
	return workspace, nil
}

func ValidateWorkcellRoot(root string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) || root == string(os.PathSeparator) || isWindowsMount(root) {
		return "", errors.New("workcell root is unsafe")
	}
	if err := rejectSymlinkPath(root); err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || rootInfo.Mode().Perm()&0o022 != 0 {
		return "", errors.New("workcell root is unavailable")
	}
	return root, nil
}

func isWindowsMount(path string) bool {
	path = filepath.ToSlash(path)
	return path == "/mnt" || strings.HasPrefix(path, "/mnt/")
}

func PlanValidation(workspace string) ([]Stage, error) {
	if regularMarker(workspace, "go.mod") {
		return []Stage{
			{Name: "go_test", Argv: []string{"go", "test", "./..."}},
			{Name: "go_vet", Argv: []string{"go", "vet", "./..."}},
			{Name: "go_build", Argv: []string{"go", "build", "./..."}},
		}, nil
	}
	if regularMarker(workspace, "package.json") {
		return planPackageScripts(workspace)
	}
	if regularMarker(workspace, "pyproject.toml") {
		return []Stage{{Name: "python_compile", Argv: []string{"python3", "-m", "compileall", "-q", "."}}}, nil
	}
	if regularMarker(workspace, "Cargo.toml") {
		return []Stage{{Name: "cargo_test", Argv: []string{"cargo", "test"}}, {Name: "cargo_check", Argv: []string{"cargo", "check"}}}, nil
	}
	return nil, errors.New("no supported local validation profile detected")
}

func planPackageScripts(workspace string) ([]Stage, error) {
	path := filepath.Join(workspace, "package.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1<<20 {
		return nil, errors.New("package manifest is unsafe")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("package manifest unavailable")
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, errors.New("package manifest is invalid")
	}
	manager := ""
	if regularMarker(workspace, "pnpm-lock.yaml") {
		manager = "pnpm"
	} else if regularMarker(workspace, "package-lock.json") {
		manager = "npm"
	} else {
		return nil, errors.New("supported package lockfile required")
	}
	stages := make([]Stage, 0, 3)
	for _, script := range []string{"check", "test", "build"} {
		if _, exists := manifest.Scripts[script]; !exists {
			continue
		}
		argv := []string{manager, "run", script}
		if script == "test" {
			argv = []string{manager, "test"}
		}
		stages = append(stages, Stage{Name: manager + "_" + script, Argv: argv})
	}
	if len(stages) == 0 {
		return nil, errors.New("no safe validation scripts declared")
	}
	return stages, nil
}

func regularMarker(root, name string) bool {
	info, err := os.Lstat(filepath.Join(root, name))
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func BubblewrapArgs(workspace string, stage Stage, network edge.NetworkPolicy) ([]string, error) {
	if network != edge.NetworkNone {
		return nil, errors.New("network policy cannot be enforced by this workcell")
	}
	if len(stage.Argv) == 0 || stage.Argv[0] == "" {
		return nil, errors.New("local stage is invalid")
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-all"}
	for _, systemPath := range []string{"/usr", "/bin", "/lib", "/lib64", "/etc"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	args = append(args,
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", workspace, "/workspace",
		"--chdir", "/workspace",
		"--setenv", "HOME", "/tmp",
		"--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin",
		"--",
	)
	return append(args, stage.Argv...), nil
}

func (w *Workcell) Execute(ctx context.Context, task edge.Task) edge.TaskResult {
	if task.Objective.Kind != edge.ObjectiveValidate || task.Restrictions.NetworkPolicy != edge.NetworkNone {
		return edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "workcell rejected unsupported objective or network policy"}
	}
	workspace, err := ResolveWorkspace(w.Root, task.Restrictions.Workspace)
	if err != nil {
		return edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "workcell rejected unsafe workspace"}
	}
	stages, err := PlanValidation(workspace)
	if err != nil {
		return edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "workcell found no supported validation profile"}
	}
	runner := w.Commands
	if runner == nil {
		runner = osCommandRunner{}
	}
	duration := time.Duration(task.Restrictions.MaxDurationSeconds) * time.Second
	executionContext, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	completed := make([]string, 0, len(stages))
	remainingOutput := task.Restrictions.MaxOutputBytes
	for _, stage := range stages {
		if executionContext.Err() != nil {
			return edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "workcell execution cancelled or timed out"}
		}
		args, err := BubblewrapArgs(workspace, stage, task.Restrictions.NetworkPolicy)
		if err != nil {
			return edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "workcell could not construct sandbox"}
		}
		exitCode, output, runErr := runner.Run(executionContext, "bwrap", args, remainingOutput)
		remainingOutput -= int64(len(output))
		if remainingOutput < 0 {
			remainingOutput = 0
		}
		if executionContext.Err() != nil {
			return edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "workcell execution cancelled or timed out"}
		}
		if runErr != nil || exitCode != 0 {
			redacted, _ := policy.Redact(string(output))
			return edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: boundedSummary(fmt.Sprintf("stage %s failed: %s", stage.Name, redacted))}
		}
		completed = append(completed, stage.Name)
	}
	return edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: boundedSummary("validation passed: " + strings.Join(completed, ", "))}
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, name string, args []string, maxOutput int64) (int, []byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return -1, nil, errors.New("required sandbox executable unavailable")
	}
	command := exec.CommandContext(ctx, name, args...)
	output := &limitedBuffer{remaining: maxOutput}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if err == nil {
		return 0, output.Bytes(), nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), output.Bytes(), nil
	}
	return -1, output.Bytes(), err
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int64
}

func (b *limitedBuffer) Write(content []byte) (int, error) {
	original := len(content)
	if b.remaining > 0 {
		write := int64(len(content))
		if write > b.remaining {
			write = b.remaining
		}
		_, _ = b.buffer.Write(content[:write])
		b.remaining -= write
	}
	return original, nil
}

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func boundedSummary(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 2000 {
		value = value[:2000]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

var workspaceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
