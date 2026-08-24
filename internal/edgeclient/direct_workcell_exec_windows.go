//go:build windows

package edgeclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/windows"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	windowsDirectWorkcellMaxProcesses uint32        = 256
	windowsDirectWorkcellMemoryBytes  uint64        = 512 << 20
	windowsDirectWorkcellCPUTime      time.Duration = 120 * time.Second
)

var (
	ErrDirectWorkcellContract = errors.New("direct workcell command contract is invalid")

	directWorkcellOperationIDPattern      = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
	directWorkcellEnvironmentKeyPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	windowsExecutableNamePattern          = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	windowsDirectWorkcellSecretKeyPattern = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|PRIVATE_KEY|API_KEY)`)
)

type DirectWorkcellCommandRequest struct {
	OperationID string
	Workspace   Workspace
	// WindowsDevRoot must be copied from the registry-resolved Workspace;
	// callers may not derive it from the requested workspace path.
	WindowsDevRoot string
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

	workspaceHandle *WindowsWorkspace
	cwdHandle       *WindowsWorkspace
	tempHandle      *WindowsWorkspace
	codeHomeHandle  *WindowsWorkspace
}

type DirectWorkcellCommandRunner interface {
	Run(context.Context, DirectWorkcellProcessSpec) (int, error)
}

type windowsDirectWorkcellExecRunner struct{}

func RunDirectWorkcellCommand(ctx context.Context, request DirectWorkcellCommandRequest, runner DirectWorkcellCommandRunner) (DirectWorkcellCommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	preflightStarted := time.Now()
	stdout := newWindowsBoundedCapture(edge.MaxProjectExecStreamBytes)
	stderr := newWindowsBoundedCapture(edge.MaxProjectExecStreamBytes)
	resolveExecutable := runner == nil
	if runner == nil {
		runner = windowsDirectWorkcellExecRunner{}
	}
	spec, err := prepareDirectWorkcellProcessSpec(request, stdout, stderr, resolveExecutable)
	if err != nil {
		return DirectWorkcellCommandResult{}, err
	}
	defer closeWindowsDirectWorkcellSpec(spec)
	preflightUS := time.Since(preflightStarted).Microseconds()
	executionCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	executionStarted := time.Now()
	exitCode, runErr := runner.Run(executionCtx, spec)
	executionUS := time.Since(executionStarted).Microseconds()
	resultStarted := time.Now()
	result := DirectWorkcellCommandResult{
		Completed:       true,
		ExitCode:        exitCode,
		Stdout:          boundedRedactedWindowsWorkcellOutput(stdout.String(), edge.MaxProjectExecStreamBytes),
		Stderr:          boundedRedactedWindowsWorkcellOutput(stderr.String(), edge.MaxProjectExecStreamBytes),
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		TimingKnown:     true,
		PreflightUS:     preflightUS,
		ExecutionUS:     executionUS,
	}
	result.ResultUS = time.Since(resultStarted).Microseconds()
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, nil
	}
	if errors.Is(executionCtx.Err(), context.Canceled) {
		return DirectWorkcellCommandResult{}, context.Canceled
	}
	if runErr != nil {
		return DirectWorkcellCommandResult{}, errors.New("direct workcell process failed")
	}
	if exitCode < 0 || exitCode > 255 {
		return DirectWorkcellCommandResult{}, errors.New("direct workcell exit status is invalid")
	}
	return result, nil
}

func prepareDirectWorkcellProcessSpec(request DirectWorkcellCommandRequest, stdout, stderr io.Writer, _ bool) (DirectWorkcellProcessSpec, error) {
	if err := validateWindowsDirectWorkcellRequest(request); err != nil {
		return DirectWorkcellProcessSpec{}, err
	}
	if !IsWindowsLocalPath(request.WindowsDevRoot) || !WindowsPathContained(request.WindowsDevRoot, request.Workspace.Path) || strings.EqualFold(filepath.Clean(request.WindowsDevRoot), filepath.Clean(request.Workspace.Path)) {
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}
	workspace, err := OpenWindowsWorkcell(request.WindowsDevRoot, request.Workspace.Path)
	if err != nil {
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = workspace.Close()
		}
	}()

	workDir := workspace.FinalPath()
	cwd, cwdErr := OpenWindowsWorkcell(request.WindowsDevRoot, workDir)
	if cwdErr != nil {
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}
	closeCWDOnError := true
	defer func() {
		if closeCWDOnError {
			_ = cwd.Close()
		}
	}()
	if request.CWD != "" {
		candidate := filepath.Join(workDir, filepath.FromSlash(request.CWD))
		_ = cwd.Close()
		cwd, cwdErr = OpenWindowsWorkcell(request.WindowsDevRoot, candidate)
		if cwdErr != nil {
			return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
		}
		workDir = cwd.FinalPath()
	}
	tempDir, tempHandle, err := createWindowsDirectWorkcellTemp(workspace.FinalPath())
	if err != nil {
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}
	codeHomeHandle, err := prepareWindowsCodexHome(request.Environment, workspace.FinalPath())
	if err != nil {
		_ = tempHandle.Close()
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}

	systemRoot := windowsDirectSystemRoot()
	environment, pathValue, err := windowsDirectWorkcellEnvironment(request.Environment, tempDir, systemRoot)
	if err != nil {
		_ = tempHandle.Close()
		if codeHomeHandle != nil {
			_ = codeHomeHandle.Close()
		}
		return DirectWorkcellProcessSpec{}, err
	}
	executable, err := resolveWindowsDirectExecutable(request.Argv[0], pathValue)
	if err != nil {
		_ = tempHandle.Close()
		if codeHomeHandle != nil {
			_ = codeHomeHandle.Close()
		}
		return DirectWorkcellProcessSpec{}, ErrDirectWorkcellContract
	}

	closeOnError = false
	closeCWDOnError = false
	return DirectWorkcellProcessSpec{
		Executable: executable,
		Args:       append([]string(nil), request.Argv[1:]...),
		Dir:        workDir,
		Env:        environment,
		Stdin:      strings.NewReader(request.Stdin),
		Stdout:     stdout,
		Stderr:     stderr,
		tempHandle: tempHandle, codeHomeHandle: codeHomeHandle, workspaceHandle: workspace, cwdHandle: cwd,
	}, nil
}

func validateWindowsDirectWorkcellRequest(request DirectWorkcellCommandRequest) error {
	if !directWorkcellOperationIDPattern.MatchString(request.OperationID) ||
		!workspaceIDPattern.MatchString(request.Workspace.ID) ||
		request.Workspace.Profile != WorkspaceProfileWindowsWorkcell ||
		request.Workspace.Mode != WorkspaceModeDev ||
		request.Workspace.NetworkPosture != WindowsWorkcellNetworkPosture ||
		request.Persistent ||
		!IsWindowsLocalPath(request.Workspace.WindowsDevRoot) ||
		!strings.EqualFold(filepath.Clean(request.Workspace.WindowsDevRoot), filepath.Clean(request.WindowsDevRoot)) ||
		!IsWindowsLocalPath(request.WindowsDevRoot) ||
		!WindowsPathContained(request.WindowsDevRoot, request.Workspace.Path) ||
		strings.EqualFold(filepath.Clean(request.WindowsDevRoot), filepath.Clean(request.Workspace.Path)) ||
		!IsWindowsLocalPath(request.Workspace.Path) ||
		request.TimeoutSeconds < 1 || request.TimeoutSeconds > 120 ||
		len(request.Argv) == 0 || len(request.Argv) > 128 ||
		len(request.Stdin) > edge.MaxProjectExecStdinBytes ||
		!utf8.ValidString(request.Stdin) || strings.ContainsRune(request.Stdin, 0) ||
		len(request.Environment) > 32 {
		return ErrDirectWorkcellContract
	}
	argvBytes := 0
	for _, argument := range request.Argv {
		if argument == "" || len(argument) > 8192 || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) {
			return ErrDirectWorkcellContract
		}
		if windowsDirectWorkcellSecretValue(argument) {
			return ErrDirectWorkcellContract
		}
		argvBytes += len(argument)
	}
	if argvBytes > 32<<10 || windowsDirectWorkcellSecretValue(request.Stdin) {
		return ErrDirectWorkcellContract
	}
	environmentBytes := 0
	seenEnvironmentKeys := make(map[string]struct{}, len(request.Environment))
	if request.CWD != "" {
		if len(request.CWD) > 1024 || strings.ContainsAny(request.CWD, `\`+"\x00\r\n") || filepath.IsAbs(request.CWD) {
			return ErrDirectWorkcellContract
		}
		clean := filepath.Clean(filepath.FromSlash(request.CWD))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return ErrDirectWorkcellContract
		}
	}
	for key, value := range request.Environment {
		upperKey := strings.ToUpper(key)
		if _, exists := seenEnvironmentKeys[upperKey]; exists {
			return ErrDirectWorkcellContract
		}
		seenEnvironmentKeys[upperKey] = struct{}{}
		if !directWorkcellEnvironmentKeyPattern.MatchString(key) || windowsDirectWorkcellReservedEnvironmentKey(key) ||
			len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || windowsDirectWorkcellSecretValue(value) {
			return ErrDirectWorkcellContract
		}
		environmentBytes += len(key) + len(value)
	}
	if environmentBytes > 16<<10 {
		return ErrDirectWorkcellContract
	}
	return nil
}

func windowsDirectSystemRoot() string {
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Clean(root)
}

func windowsDirectWorkcellEnvironment(requested map[string]string, tempDir, systemRoot string) ([]string, string, error) {
	if !IsWindowsLocalPath(systemRoot) {
		return nil, "", ErrDirectWorkcellContract
	}
	system32 := filepath.Join(systemRoot, "System32")
	pathValue := strings.Join([]string{system32, systemRoot, filepath.Join(system32, "Wbem"), filepath.Join(system32, "WindowsPowerShell", "v1.0")}, ";")
	baseline := map[string]string{
		"ComSpec":                    filepath.Join(system32, "cmd.exe"),
		"MCP_DEVBOX_MODE":            string(WorkspaceModeDev),
		"MCP_DEVBOX_NETWORK_POSTURE": WindowsWorkcellNetworkPosture,
		"MCP_DEVBOX_PROFILE":         string(WorkspaceProfileWindowsWorkcell),
		"PATHEXT":                    ".COM;.EXE;.BAT;.CMD",
		"PATH":                       pathValue,
		"SystemRoot":                 systemRoot,
		"TEMP":                       tempDir,
		"TMP":                        tempDir,
	}
	for key, value := range requested {
		if windowsDirectWorkcellReservedEnvironmentKey(key) {
			return nil, "", ErrDirectWorkcellContract
		}
		baseline[key] = value
	}
	keys := make([]string, 0, len(baseline))
	for key := range baseline {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+baseline[key])
	}
	return environment, pathValue, nil
}

func windowsDirectWorkcellReservedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	if windowsDirectWorkcellSecretKeyPattern.MatchString(upper) {
		return true
	}
	switch upper {
	case "PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP",
		"HOME", "USER", "USERNAME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA",
		"PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "COMMONPROGRAMFILES", "COMMONPROGRAMFILES(X86)",
		"CONTAINER_HOST", "DOCKER_HOST", "DOCKER_CONFIG", "KUBECONFIG", "SSH_AUTH_SOCK":
		return true
	}
	return strings.HasPrefix(upper, "MCP_DEVBOX_")
}

func windowsDirectWorkcellSecretValue(value string) bool {
	redacted, changed := policy.Redact(value)
	return changed || redacted != value
}

func resolveWindowsDirectExecutable(value, pathValue string) (string, error) {
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", ErrDirectWorkcellContract
	}
	if filepath.IsAbs(value) {
		return validateWindowsDirectExecutablePath(value)
	}
	if strings.ContainsAny(value, `\/`) || !windowsExecutableNamePattern.MatchString(value) {
		return "", ErrDirectWorkcellContract
	}
	names := []string{value}
	if filepath.Ext(value) == "" {
		names = []string{value + ".COM", value + ".EXE", value + ".BAT", value + ".CMD"}
	}
	for _, directory := range strings.Split(pathValue, ";") {
		for _, name := range names {
			if candidate, err := validateWindowsDirectExecutablePath(filepath.Join(directory, name)); err == nil {
				return candidate, nil
			}
		}
	}
	return "", ErrDirectWorkcellContract
}

func validateWindowsDirectExecutablePath(path string) (string, error) {
	if !IsWindowsLocalPath(path) {
		return "", ErrDirectWorkcellContract
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrDirectWorkcellContract
	}
	return filepath.Clean(path), nil
}

func createWindowsDirectWorkcellTemp(workspace string) (string, *WindowsWorkspace, error) {
	root := filepath.Join(workspace, ".mcp-devbox", "runtime", "tmp")
	if err := createWindowsDirectoryTree(workspace, root); err != nil {
		return "", nil, err
	}
	rootHandle, _, err := openAndInspectWindowsDirectoryNoDelete(root)
	if err != nil {
		return "", nil, err
	}
	defer windows.CloseHandle(rootHandle)
	tempDir, err := os.MkdirTemp(root, "exec-")
	if err != nil {
		return "", nil, err
	}
	handle, err := OpenWindowsWorkcell(workspace, tempDir)
	if err != nil {
		return "", nil, err
	}
	return handle.FinalPath(), handle, nil
}

func prepareWindowsCodexHome(environment map[string]string, workspace string) (*WindowsWorkspace, error) {
	value := ""
	for key, candidate := range environment {
		if strings.EqualFold(key, "CODEX_HOME") {
			value = filepath.Clean(strings.TrimSpace(candidate))
			break
		}
	}
	if value == "" {
		return nil, nil
	}
	if !IsWindowsLocalPath(value) || !WindowsPathContained(workspace, value) || strings.EqualFold(filepath.Clean(workspace), value) {
		return nil, ErrDirectWorkcellContract
	}
	if err := createWindowsDirectoryTree(workspace, value); err != nil {
		return nil, err
	}
	handle, err := OpenWindowsWorkcell(workspace, value)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

func closeWindowsDirectWorkcellSpec(spec DirectWorkcellProcessSpec) {
	if spec.codeHomeHandle != nil {
		_ = spec.codeHomeHandle.Close()
	}
	if spec.tempHandle != nil {
		_ = spec.tempHandle.Close()
	}
	if spec.cwdHandle != nil {
		_ = spec.cwdHandle.Close()
	}
	if spec.workspaceHandle != nil {
		_ = spec.workspaceHandle.Close()
	}
}

func (windowsDirectWorkcellExecRunner) Run(ctx context.Context, spec DirectWorkcellProcessSpec) (int, error) {
	if spec.workspaceHandle == nil || spec.cwdHandle == nil || spec.tempHandle == nil || spec.Executable == "" || spec.Dir == "" {
		return -1, ErrDirectWorkcellContract
	}
	command := exec.Command(spec.Executable, spec.Args...)
	command.Dir = spec.Dir
	command.Env = append([]string(nil), spec.Env...)
	command.Stdin = spec.Stdin
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	limits := WindowsProcessTreeLimits{
		MaxProcesses: windowsDirectWorkcellMaxProcesses,
		MemoryBytes:  windowsDirectWorkcellMemoryBytes,
		CPUTime:      windowsDirectWorkcellCPUTime,
		WallTime:     120 * time.Second,
	}
	tree, err := NewWindowsProcessTree(limits)
	if err != nil {
		return -1, err
	}
	if err := spec.workspaceHandle.Revalidate(); err != nil {
		_ = tree.Close()
		return -1, err
	}
	if err := spec.cwdHandle.Revalidate(); err != nil {
		_ = tree.Close()
		return -1, err
	}
	if err := spec.tempHandle.Revalidate(); err != nil {
		_ = tree.Close()
		return -1, err
	}
	if spec.codeHomeHandle != nil {
		if err := spec.codeHomeHandle.Revalidate(); err != nil {
			_ = tree.Close()
			return -1, err
		}
	}
	if err := tree.Start(ctx, command); err != nil {
		return -1, err
	}
	waitErr := tree.Wait()
	if command.ProcessState != nil {
		if code := command.ProcessState.ExitCode(); code >= 0 {
			return code, nil
		}
	}
	if waitErr != nil {
		return -1, waitErr
	}
	return -1, errors.New("direct workcell process exit status is unavailable")
}

type windowsBoundedCapture struct {
	buffer    bytes.Buffer
	limit     int64
	truncated bool
}

func newWindowsBoundedCapture(limit int64) *windowsBoundedCapture {
	return &windowsBoundedCapture{limit: limit}
}

func (capture *windowsBoundedCapture) Write(payload []byte) (int, error) {
	originalLength := len(payload)
	if capture.limit <= int64(capture.buffer.Len()) {
		capture.truncated = true
		return originalLength, nil
	}
	remaining := capture.limit - int64(capture.buffer.Len())
	if int64(len(payload)) > remaining {
		payload = payload[:remaining]
		capture.truncated = true
	}
	_, _ = capture.buffer.Write(payload)
	return originalLength, nil
}

func (capture *windowsBoundedCapture) String() string { return capture.buffer.String() }

func (capture *windowsBoundedCapture) Truncated() bool { return capture.truncated }

func boundedRedactedWindowsWorkcellOutput(value string, limit int64) string {
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
