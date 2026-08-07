//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

var ErrDirectWorkcellContract = errors.New("direct workcell command contract is invalid")

var directWorkcellOperationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
var directWorkcellEnvironmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

type DirectWorkcellCommandRequest struct {
	OperationID    string
	Workspace      Workspace
	Argv           []string
	CWD            string
	Stdin          string
	Environment    map[string]string
	TimeoutSeconds int
	Persistent     bool
}

type DirectWorkcellCommandResult struct {
	Completed       bool
	ExitCode        int
	Stdout          string
	Stderr          string
	TimedOut        bool
	StdoutTruncated bool
	StderrTruncated bool
	TimingKnown     bool
	PreflightUS     int64
	ExecutionUS     int64
	ResultUS        int64
}

type DirectWorkcellProcessSpec struct {
	Executable            string
	Args                  []string
	Dir                   string
	Env                   []string
	Stdin                 io.Reader
	Stdout                io.Writer
	Stderr                io.Writer
	PersistentProcessID   string
	PersistentStateRoot   string
	PersistentMaxLogBytes int64
	PersistentStdin       string
}

type DirectWorkcellCommandRunner interface {
	Run(context.Context, DirectWorkcellProcessSpec) (int, error)
}

type directWorkcellExecRunner struct{}

func RunDirectWorkcellCommand(ctx context.Context, request DirectWorkcellCommandRequest, runner DirectWorkcellCommandRunner) (DirectWorkcellCommandResult, error) {
	preflightStarted := time.Now()
	stdout := newBoundedCapture(edge.MaxProjectExecStreamBytes)
	stderr := newBoundedCapture(edge.MaxProjectExecStreamBytes)
	resolveExecutable := runner == nil
	if runner == nil {
		runner = directWorkcellExecRunner{}
	}
	spec, err := prepareDirectWorkcellProcessSpec(request, stdout, stderr, resolveExecutable)
	if err != nil {
		return DirectWorkcellCommandResult{}, err
	}
	preflightUS := time.Since(preflightStarted).Microseconds()
	executionCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	executionStarted := time.Now()
	exitCode, runErr := runner.Run(executionCtx, spec)
	executionUS := time.Since(executionStarted).Microseconds()
	resultStarted := time.Now()
	result := DirectWorkcellCommandResult{
		Completed: true, ExitCode: exitCode,
		Stdout:          boundedRedactedWorkcellOutput(stdout.String(), edge.MaxProjectExecStreamBytes),
		Stderr:          boundedRedactedWorkcellOutput(stderr.String(), edge.MaxProjectExecStreamBytes),
		StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated(),
		TimingKnown: true, PreflightUS: preflightUS, ExecutionUS: executionUS,
	}
	result.ResultUS = time.Since(resultStarted).Microseconds()
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, nil
	}
	if runErr != nil {
		return DirectWorkcellCommandResult{}, errors.New("direct workcell process failed")
	}
	if exitCode < 0 || exitCode > 255 {
		return DirectWorkcellCommandResult{}, errors.New("direct workcell exit status is invalid")
	}
	return result, nil
}

func prepareDirectWorkcellProcessSpec(request DirectWorkcellCommandRequest, stdout, stderr io.Writer, resolveExecutable bool) (DirectWorkcellProcessSpec, error) {
	workspace, sandboxCWD, err := validateDirectWorkcellRequest(request)
	if err != nil {
		return DirectWorkcellProcessSpec{}, err
	}
	bubblewrap := "bwrap"
	if resolveExecutable {
		bubblewrap, err = resolveDirectWorkcellBubblewrap()
		if err != nil {
			return DirectWorkcellProcessSpec{}, err
		}
	}
	if err := prepareDirectWorkcellRuntime(workspace); err != nil {
		return DirectWorkcellProcessSpec{}, err
	}
	args, environment, err := directWorkcellBubblewrapArgs(workspace, sandboxCWD, request)
	if err != nil {
		return DirectWorkcellProcessSpec{}, err
	}
	return DirectWorkcellProcessSpec{
		Executable: bubblewrap, Args: args, Dir: workspace, Env: environment,
		Stdin: strings.NewReader(request.Stdin), Stdout: stdout, Stderr: stderr,
		PersistentStdin: request.Stdin,
	}, nil
}

func validateDirectWorkcellRequest(request DirectWorkcellCommandRequest) (string, string, error) {
	if !directWorkcellOperationIDPattern.MatchString(request.OperationID) || !workspaceIDPattern.MatchString(request.Workspace.ID) ||
		request.Workspace.Profile != WorkspaceProfileLinuxWorkcell || request.Workspace.Mode != WorkspaceModeDev ||
		request.TimeoutSeconds < 1 || request.TimeoutSeconds > 120 || len(request.Argv) == 0 || len(request.Argv) > 128 ||
		len(request.Stdin) > edge.MaxProjectExecStdinBytes || !utf8.ValidString(request.Stdin) || strings.ContainsRune(request.Stdin, 0) {
		return "", "", ErrDirectWorkcellContract
	}
	workspace, err := ValidateRegisteredWorkspace(request.Workspace.Path)
	if err != nil {
		return "", "", ErrDirectWorkcellContract
	}
	argvBytes := 0
	for _, argument := range request.Argv {
		if argument == "" || len(argument) > 8192 || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) {
			return "", "", ErrDirectWorkcellContract
		}
		argvBytes += len(argument)
	}
	if argvBytes > 32<<10 || len(request.Environment) > 32 {
		return "", "", ErrDirectWorkcellContract
	}
	environmentBytes := 0
	for key, value := range request.Environment {
		if !directWorkcellEnvironmentKeyPattern.MatchString(key) || directWorkcellReservedEnvironmentKey(key) ||
			len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return "", "", ErrDirectWorkcellContract
		}
		environmentBytes += len(key) + len(value)
	}
	if environmentBytes > 16<<10 {
		return "", "", ErrDirectWorkcellContract
	}
	candidate := workspace
	sandboxCWD := "/workspace"
	if request.CWD != "" {
		if filepath.IsAbs(request.CWD) || strings.ContainsAny(request.CWD, "\\\x00") {
			return "", "", ErrDirectWorkcellContract
		}
		clean := filepath.Clean(filepath.FromSlash(request.CWD))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return "", "", ErrDirectWorkcellContract
		}
		candidate = filepath.Join(workspace, clean)
		sandboxCWD = filepath.ToSlash(filepath.Join("/workspace", clean))
	}
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", ErrDirectWorkcellContract
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(candidate) || !pathInside(workspace, resolved) && resolved != workspace {
		return "", "", ErrDirectWorkcellContract
	}
	return workspace, sandboxCWD, nil
}

func directWorkcellReservedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR",
		"DOCKER_HOST", "CONTAINER_HOST", "DOCKER_CONFIG",
		"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES",
		"CONTAINERS_STORAGE_CONF":
		return true
	}
	if strings.HasPrefix(upper, "XDG_") || strings.HasPrefix(upper, "MCP_DEVBOX_") {
		return true
	}
	for _, fragment := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE_KEY", "API_KEY"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func prepareDirectWorkcellRuntime(workspace string) error {
	for _, relative := range []string{
		".mcp-devbox", ".mcp-devbox/tools", ".mcp-devbox/tools/bin", ".mcp-devbox/tools/go",
		".mcp-devbox/tools/cargo", ".mcp-devbox/cache", ".mcp-devbox/runtime",
		".mcp-devbox/runtime/home",
	} {
		if err := ensurePrivateWorkspaceDir(workspace, filepath.Join(workspace, filepath.FromSlash(relative))); err != nil {
			return errors.New("direct workcell runtime is unavailable")
		}
	}
	return nil
}

func resolveDirectWorkcellBubblewrap() (string, error) {
	candidate, err := exec.LookPath("bwrap")
	if err != nil {
		return "", errors.New("bubblewrap executable is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", errors.New("bubblewrap executable is unsafe")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("bubblewrap executable is unsafe")
	}
	return resolved, nil
}

func directWorkcellBubblewrapArgs(workspace, sandboxCWD string, request DirectWorkcellCommandRequest) ([]string, []string, error) {
	toolPath := openCodeDefaultToolPath
	if err := validateOpenCodeToolPath(toolPath); err != nil {
		return nil, nil, err
	}
	persistentPath := strings.Join([]string{
		"/workspace/.mcp-devbox/tools/bin",
		"/workspace/.mcp-devbox/tools/go/bin",
		"/workspace/.mcp-devbox/tools/cargo/bin",
		toolPath,
	}, ":")
	// A durable command is parented by its minimal worker rather than by the Edge
	// service. Binding Bubblewrap to that worker preserves Edge-restart recovery while
	// ensuring a crashed worker cannot leave an unowned sandbox behind.
	args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--share-net", "--clearenv"}
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates", "/etc/alternatives"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	for _, target := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/services", "/etc/protocols"} {
		if source, ok := safeLinuxWorkcellSystemFile(target); ok {
			args = append(args, "--ro-bind", source, target)
		}
	}
	for _, target := range []string{"/usr/share/seclists", "/usr/share/wordlists"} {
		if source, ok := safeLinuxWorkcellReadonlyDirectory(target, "/usr/share", 0); ok {
			args = append(args, "--ro-bind", source, target)
		}
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--bind", workspace, "/workspace", "--chdir", sandboxCWD,
	)
	baseline := map[string]string{
		"PATH": persistentPath, "HOME": "/workspace/.mcp-devbox/runtime/home", "USER": "mcpedge",
		"LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TERM": "dumb", "SHELL": "/bin/sh",
		"XDG_CACHE_HOME": "/workspace/.mcp-devbox/cache", "TMPDIR": "/tmp",
		"MCP_DEVBOX_PROFILE": string(request.Workspace.Profile), "MCP_DEVBOX_MODE": string(request.Workspace.Mode),
		"MCP_DEVBOX_NETWORK_POSTURE": LinuxWorkcellNetworkPosture,
	}
	keys := make([]string, 0, len(baseline)+len(request.Environment))
	for key := range baseline {
		keys = append(keys, key)
	}
	for key := range request.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, ok := baseline[key]
		if !ok {
			value = request.Environment[key]
		}
		args = append(args, "--setenv", key, value)
	}
	args = append(args, "--")
	args = append(args, request.Argv...)
	hostEnvironment := []string{"PATH=" + toolPath, "HOME=" + workspace, "USER=mcpedge", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	return args, hostEnvironment, nil
}

func boundedRedactedWorkcellOutput(value string, limit int64) string {
	value = strings.ToValidUTF8(value, "�")
	value, _ = policy.Redact(value)
	if int64(len(value)) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (directWorkcellExecRunner) Run(ctx context.Context, spec DirectWorkcellProcessSpec) (int, error) {
	command := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	command.Dir = spec.Dir
	command.Env = append([]string(nil), spec.Env...)
	command.Stdin = spec.Stdin
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return -1, err
}
