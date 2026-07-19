//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	PinnedOpenCodeVersion         = "1.18.1"
	PinnedOpenCodePackage         = "opencode-ai"
	PinnedOpenCodeIntegrity       = "sha512-Rtp0fCJyu3Iz0MXfwQeAYdYjIsSPPUWYyJO0mf0Q9v5zTNYxlakzXUh+Van50XAmEDAhCaJvCcOJzweq2k3HMQ=="
	OpenCodeExternalDriverPackage = "@mcp-devbox/opencode-external-driver"
	openCodeModelID               = "bridge/external-model"
	openCodeRuntimeDirName        = "r"
	openCodeDriverSocketName      = "d.sock"
	openCodeDefaultOutputLimit    = int64(1 << 20)
	openCodeMaxOutputLimit        = int64(4 << 20)
	openCodeSandboxWorkspace      = "/workspace"
	openCodeSandboxRuntime        = "/runtime"
	openCodeSandboxHome           = "/runtime/home"
	openCodeSandboxSocket         = "/runtime/d.sock"
	openCodeSandboxProvider       = "/mcp-provider"
	openCodeSandboxExecutable     = "/mcp-opencode"
	openCodeDefaultToolPath       = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	openCodeManagedToolRoot       = "/srv/mcp-devbox-tools"
)

var (
	ErrOpenCodeInterrupted = errors.New("OpenCode runtime was interrupted and will not be executed twice")
	ErrOpenCodeTerminal    = errors.New("OpenCode runtime is already terminal")
)

type OpenCodeRemoteTransport interface {
	modelturn.ModelTurnTransport
	Started(context.Context) (modelturn.Runtime, error)
	Heartbeat(context.Context) (modelturn.Runtime, error)
	Failed(context.Context, string) (modelturn.Runtime, error)
	Completed(context.Context, string) (modelturn.Runtime, error)
	Close() error
}

type OpenCodeLauncherConfig struct {
	StateRoot      string
	SocketRoot     string
	OpenCodePath   string
	DriverPath     string
	ProviderPath   string
	BubblewrapPath string
	IntegrityPath  string
	StopPath       string
	ToolPath       string
	OutputLimit    int64
	Heartbeat      time.Duration
	HTTPClient     *http.Client
	Workspaces     *WorkspaceRegistry
	Journal        *OpenCodeRuntimeJournal
}

type OpenCodeLaunchResult struct {
	RuntimeID       string
	WorkspaceID     string
	State           OpenCodeLocalState
	ExitCode        int
	OutputTruncated bool
}

type OpenCodeLauncher struct {
	config                 OpenCodeLauncherConfig
	remoteFactory          func(ModelRuntimeLease) (OpenCodeRemoteTransport, error)
	runProcess             func(context.Context, openCodeProcessSpec) openCodeProcessResult
	verifySandbox          func(context.Context, openCodeProcessSpec) error
	resolveWorkspace       func(string) (string, error)
	resolveWorkspaceRecord func(string) (Workspace, error)
	linuxNetworkProbe      LinuxNetworkProbe
	rootlessEndpoint       func(int, string) (*RootlessContainerEndpoint, error)
	containerRunner        ContainerCommandRunner
	effectiveUID           func() int
	now                    func() time.Time
	allowRootTest          bool
	allowRootlessRootTest  bool
}

type openCodeProcessSpec struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	Stdout     io.Writer
	Stderr     io.Writer
	Sandbox    openCodeSandboxSpec
}

type openCodeSandboxMount struct {
	Source   string
	Target   string
	Writable bool
	Kind     string
}

type openCodeSandboxSpec struct {
	UnshareAll       bool
	ShareNetwork     bool
	ClearEnv         bool
	NewSession       bool
	DieWithParent    bool
	Mounts           []openCodeSandboxMount
	Environment      map[string]string
	WorkingDirectory string
	Command          []string
}

type openCodeProcessResult struct {
	ExitCode int
	Err      error
}

func NewOpenCodeLauncher(config OpenCodeLauncherConfig) (*OpenCodeLauncher, error) {
	config.StateRoot = filepath.Clean(strings.TrimSpace(config.StateRoot))
	if strings.TrimSpace(config.SocketRoot) == "" {
		config.SocketRoot = filepath.Join(config.StateRoot, openCodeRuntimeDirName)
	}
	config.SocketRoot = filepath.Clean(strings.TrimSpace(config.SocketRoot))
	config.OpenCodePath = filepath.Clean(strings.TrimSpace(config.OpenCodePath))
	if strings.TrimSpace(config.DriverPath) != "" {
		config.DriverPath = filepath.Clean(strings.TrimSpace(config.DriverPath))
	}
	config.ProviderPath = filepath.Clean(strings.TrimSpace(config.ProviderPath))
	config.BubblewrapPath = filepath.Clean(strings.TrimSpace(config.BubblewrapPath))
	config.IntegrityPath = filepath.Clean(strings.TrimSpace(config.IntegrityPath))
	if !filepath.IsAbs(config.StateRoot) || !filepath.IsAbs(config.SocketRoot) || !filepath.IsAbs(config.OpenCodePath) || (config.DriverPath != "" && !filepath.IsAbs(config.DriverPath)) || !filepath.IsAbs(config.ProviderPath) || !filepath.IsAbs(config.BubblewrapPath) || !filepath.IsAbs(config.IntegrityPath) {
		return nil, errors.New("OpenCode launcher paths must be absolute local paths")
	}
	if !pathInside(config.StateRoot, config.SocketRoot) {
		return nil, errors.New("OpenCode socket root must stay inside the private Edge state root")
	}
	if err := preparePrivateRoot(config.SocketRoot); err != nil {
		return nil, errors.New("OpenCode socket root is unsafe")
	}
	if config.StopPath == "" {
		config.StopPath = filepath.Join(config.StateRoot, "STOP")
	}
	config.StopPath = filepath.Clean(config.StopPath)
	if !filepath.IsAbs(config.StopPath) {
		return nil, errors.New("OpenCode kill-switch path must be absolute")
	}
	if config.ToolPath == "" {
		config.ToolPath = openCodeDefaultToolPath
	}
	if err := validateOpenCodeToolPath(config.ToolPath); err != nil {
		return nil, err
	}
	if config.OutputLimit == 0 {
		config.OutputLimit = openCodeDefaultOutputLimit
	}
	if config.OutputLimit < 4096 || config.OutputLimit > openCodeMaxOutputLimit {
		return nil, errors.New("OpenCode output limit is invalid")
	}
	if config.Heartbeat == 0 {
		config.Heartbeat = 5 * time.Second
	}
	if config.Heartbeat < time.Second || config.Heartbeat > 30*time.Second {
		return nil, errors.New("OpenCode heartbeat interval is invalid")
	}
	if config.Workspaces == nil || config.Journal == nil {
		return nil, errors.New("OpenCode launcher requires local workspace and runtime journals")
	}
	launcher := &OpenCodeLauncher{config: config, effectiveUID: os.Geteuid, now: time.Now, runProcess: runOpenCodeProcess}
	launcher.verifySandbox = launcher.verifyOpenCodeSandbox
	launcher.resolveWorkspace = config.Workspaces.Resolve
	launcher.resolveWorkspaceRecord = config.Workspaces.Get
	launcher.rootlessEndpoint = DiscoverRootlessContainerEndpoint
	launcher.remoteFactory = func(lease ModelRuntimeLease) (OpenCodeRemoteTransport, error) {
		return NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: config.StateRoot, Lease: lease, HTTPClient: config.HTTPClient})
	}
	return launcher, nil
}

func (l *OpenCodeLauncher) RunNext(ctx context.Context, wait time.Duration) (bool, OpenCodeLaunchResult, error) {
	if l == nil {
		return false, OpenCodeLaunchResult{}, errors.New("OpenCode launcher is unavailable")
	}
	transport, err := NewTransport(l.config.StateRoot, l.config.HTTPClient)
	if err != nil {
		return false, OpenCodeLaunchResult{}, err
	}
	lease, err := transport.LeaseModelRuntime(ctx, wait)
	if err != nil || lease == nil {
		return false, OpenCodeLaunchResult{}, err
	}
	result, err := l.RunLease(ctx, *lease)
	return true, result, err
}

func (l *OpenCodeLauncher) RunLease(ctx context.Context, lease ModelRuntimeLease) (OpenCodeLaunchResult, error) {
	result := OpenCodeLaunchResult{RuntimeID: lease.RuntimeID, WorkspaceID: lease.WorkspaceID}
	if l == nil || l.effectiveUID == nil {
		return result, errors.New("OpenCode launcher is unavailable")
	} else if l.effectiveUID() == 0 && !l.allowRootTest {
		return result, errors.New("OpenCode must run as the non-root Edge user")
	}
	if err := validateLauncherLease(lease); err != nil {
		return result, err
	}
	if l.killSwitchActive() {
		return result, ErrKillSwitch
	}
	remote, err := l.remoteFactory(lease)
	if err != nil {
		return result, err
	}
	defer remote.Close()
	entry, created, err := l.config.Journal.Begin(ctx, lease.RuntimeID, lease.WorkspaceID, lease.GoalDigest, lease.ProviderProfile)
	if err != nil {
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	if !created {
		result.State = entry.State
		result.ExitCode = entry.ExitCode
		result.OutputTruncated = entry.OutputTruncated
		switch entry.State {
		case OpenCodeLocalCompleted, OpenCodeLocalFailed, OpenCodeLocalCancelled:
			return result, ErrOpenCodeTerminal
		default:
			_ = l.config.Journal.Finish(context.Background(), lease.RuntimeID, OpenCodeLocalFailed, -1, entry.OutputTruncated)
			result.State = OpenCodeLocalFailed
			result.ExitCode = -1
			_, _ = remote.Failed(context.Background(), "")
			return result, ErrOpenCodeInterrupted
		}
	}
	failLocal := func(state OpenCodeLocalState, exitCode int, truncated bool) {
		_ = l.config.Journal.Finish(context.Background(), lease.RuntimeID, state, exitCode, truncated)
		result.State = state
		result.ExitCode = exitCode
		result.OutputTruncated = truncated
	}
	workspaceRecord, err := l.resolveWorkspaceRecord(lease.WorkspaceID)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	workspace, err := l.resolveWorkspace(lease.WorkspaceID)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	if workspace != workspaceRecord.Path {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("workspace path changed during local resolution")
	}
	var preparation *LinuxWorkcellPreparation
	if workspaceRecord.Profile == WorkspaceProfileLinuxWorkcell {
		prepared, prepareErr := PrepareLinuxWorkcellWithToolPath(ctx, workspaceRecord, lease, l.config.ToolPath, l.linuxNetworkProbe)
		if prepareErr != nil {
			failLocal(OpenCodeLocalFailed, -1, false)
			_, _ = remote.Failed(context.Background(), "")
			return result, prepareErr
		}
		uid := l.effectiveUID()
		if l.rootlessEndpoint != nil && (uid > 0 || l.allowRootlessRootTest) {
			endpoint, endpointErr := l.rootlessEndpoint(uid, l.config.ToolPath)
			if endpointErr != nil {
				failLocal(OpenCodeLocalFailed, -1, false)
				_, _ = remote.Failed(context.Background(), "")
				return result, endpointErr
			}
			prepared.RootlessContainer = endpoint
			if l.allowRootTest {
				l.effectiveUID = os.Geteuid
			}
		}
		preparation = &prepared
	}
	if err := l.verifyLocalInstallationForWorkspace(ctx, workspaceRecord, preparation, lease); err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	workspaceAfter, err := l.resolveWorkspaceRecord(lease.WorkspaceID)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	workspace, err = l.resolveWorkspace(lease.WorkspaceID)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	if workspace != workspaceAfter.Path || !sameWorkspaceRuntimeContract(workspaceRecord, workspaceAfter) {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("workspace contract changed during local preflight")
	}
	workspaceRecord = workspaceAfter
	if _, err := remote.Started(ctx); err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		return result, err
	}

	runtimeDir := openCodeRuntimeDir(l.config.SocketRoot, lease.RuntimeID)
	if err := preparePrivateRoot(runtimeDir); err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	defer removePrivateRuntimeDir(runtimeDir, l.config.SocketRoot)
	socketPath := filepath.Join(runtimeDir, openCodeDriverSocketName)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(lease.TimeoutSeconds)*time.Second)
	defer cancel()
	var labBrokerDone <-chan error
	var labBrokerCancel context.CancelFunc
	if preparation != nil && workspaceRecord.Mode == WorkspaceModeHTBLinux {
		brokerCtx, brokerCancel := context.WithCancel(runCtx)
		labBrokerCancel = brokerCancel
		labBrokerDone, err = StartHTBLabBroker(brokerCtx, HTBLabBrokerConfig{
			SocketPath: filepath.Join(runtimeDir, HTBLabBrokerSocketName),
			StateRoot:  l.config.StateRoot, Workspace: workspaceRecord, RuntimeID: lease.RuntimeID, ToolPath: l.config.ToolPath,
		})
		if err != nil {
			brokerCancel()
			failLocal(OpenCodeLocalFailed, -1, false)
			_, _ = remote.Failed(context.Background(), "")
			return result, err
		}
		defer func() {
			if labBrokerCancel != nil {
				labBrokerCancel()
			}
			if labBrokerDone != nil {
				<-labBrokerDone
			}
		}()
	}
	driverDone, err := l.startDriver(runCtx, socketPath, lease, remote)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	driverExited, err := waitForPrivateDriverSocketOrExit(runCtx, socketPath, l.effectiveUID(), driverDone)
	if err != nil {
		cancel()
		if !driverExited {
			<-driverDone
		}
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	if err := l.config.Journal.MarkRunning(ctx, lease.RuntimeID); err != nil {
		cancel()
		<-driverDone
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}

	stdout := newBoundedSink(l.config.OutputLimit)
	stderr := newBoundedSink(l.config.OutputLimit)
	spec, err := l.processSpecForWorkspace(runtimeDir, workspaceRecord, preparation, socketPath, lease, stdout, stderr)
	if err != nil {
		cancel()
		<-driverDone
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	processDone := make(chan openCodeProcessResult, 1)
	processStarted := time.Now()
	go func() { processDone <- l.runProcess(runCtx, spec) }()
	heartbeatDone := make(chan modelturn.Runtime, 1)
	heartbeatErr := make(chan error, 1)
	go l.monitorRuntime(runCtx, cancel, remote, heartbeatDone, heartbeatErr)

	processResult := openCodeProcessResult{}
	var labBrokerErr error
	if labBrokerDone == nil {
		processResult = <-processDone
	} else {
		select {
		case processResult = <-processDone:
		case labBrokerErr = <-labBrokerDone:
			labBrokerDone = nil
			cancel()
			processResult = <-processDone
		}
	}
	runContextErr := runCtx.Err()
	cancel()
	if labBrokerCancel != nil {
		labBrokerCancel()
	}
	if labBrokerDone != nil {
		labBrokerErr = <-labBrokerDone
		labBrokerDone = nil
	}
	driverErr := <-driverDone
	terminalRuntime := modelturn.Runtime{}
	select {
	case terminalRuntime = <-heartbeatDone:
	default:
	}
	select {
	case <-heartbeatErr:
	default:
	}
	var cleanupErr error
	if preparation != nil {
		cleanupErr = CleanupRootlessContainerResources(context.Background(), preparation.RootlessContainer, lease.RuntimeID, l.config.ToolPath, l.containerRunner)
	}
	cleanupState := LinuxWorkcellContainerCleanupState(preparation, cleanupErr)
	terminalCheckpoint := "failed"
	if preparation != nil {
		defer func() {
			_ = RecordLinuxWorkcellTerminalState(preparation, terminalCheckpoint, cleanupState)
		}()
	}
	if labBrokerErr != nil && !errors.Is(labBrokerErr, context.Canceled) {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, stdout.Truncated() || stderr.Truncated())
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("HTB lab broker terminated unexpectedly")
	}
	if cleanupErr != nil {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, stdout.Truncated() || stderr.Truncated())
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("linux workcell rootless container cleanup failed")
	}
	truncated := stdout.Truncated() || stderr.Truncated()
	if terminalRuntime.State == modelturn.RuntimeStateCancelled {
		terminalCheckpoint = "cancelled"
		failLocal(OpenCodeLocalCancelled, processResult.ExitCode, truncated)
		return result, context.Canceled
	}
	if l.killSwitchActive() {
		terminalCheckpoint = "cancelled: kill switch"
		failLocal(OpenCodeLocalCancelled, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, ErrKillSwitch
	}
	if errors.Is(runContextErr, context.DeadlineExceeded) {
		terminalCheckpoint = "timeout"
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, context.DeadlineExceeded
	}
	if processResult.Err != nil || processResult.ExitCode != 0 {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		if stderr.ContainsFold("bwrap:") {
			diagnostic := classifyBubblewrapFailure(bubblewrapStageHelperExec, processResult.Err, stderr.TailString(), time.Since(processStarted))
			return result, fmt.Errorf("OpenCode sandbox failed (%s)", diagnostic.Code)
		}
		signal := stderr.FailureSignal()
		if signal == "unknown" {
			signal = stdout.FailureSignal()
		}
		return result, fmt.Errorf("OpenCode terminated unexpectedly (%s)", signal)
	}
	if driverErr != nil && !errors.Is(driverErr, context.Canceled) {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("OpenCode model-turn driver terminated unexpectedly")
	}
	if _, err := remote.Completed(context.Background(), ""); err != nil {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		return result, err
	}
	terminalCheckpoint = "completed"
	failLocal(OpenCodeLocalCompleted, processResult.ExitCode, truncated)
	return result, nil
}

func (l *OpenCodeLauncher) monitorRuntime(ctx context.Context, cancel context.CancelFunc, remote OpenCodeRemoteTransport, terminal chan<- modelturn.Runtime, failed chan<- error) {
	ticker := time.NewTicker(l.config.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if l.killSwitchActive() {
				cancel()
				return
			}
			runtime, err := remote.Heartbeat(ctx)
			if err != nil {
				select {
				case failed <- err:
				default:
				}
				cancel()
				return
			}
			switch runtime.State {
			case modelturn.RuntimeStateCancelled, modelturn.RuntimeStateFailed, modelturn.RuntimeStateExpired, modelturn.RuntimeStateCompleted:
				select {
				case terminal <- runtime:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (l *OpenCodeLauncher) startDriver(ctx context.Context, socketPath string, lease ModelRuntimeLease, remote OpenCodeRemoteTransport) (<-chan error, error) {
	done := make(chan error, 1)
	if l.config.DriverPath == "" {
		go func() { done <- modelturn.ServeDriverTransport(ctx, socketPath, remote, nil) }()
		return done, nil
	}
	payload, err := json.Marshal(lease)
	if err != nil {
		return nil, errors.New("OpenCode remote driver lease encoding failed")
	}
	cmd := exec.CommandContext(ctx, l.config.DriverPath, "--remote", "--state-root", l.config.StateRoot, "--socket", socketPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = []string{
		"PATH=" + l.config.ToolPath,
		"HOME=" + l.config.StateRoot,
		"USER=mcpedge",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	if caPath := localTLSCAPath(); caPath != "" {
		cmd.Env = append(cmd.Env, "SSL_CERT_FILE="+caPath)
	}
	cmd.Stdout = newBoundedSink(64 << 10)
	cmd.Stderr = newBoundedSink(64 << 10)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, errors.New("OpenCode remote driver process could not start")
	}
	go func() { done <- waitExternalDriver(ctx, cmd) }()
	return done, nil
}

func waitExternalDriver(ctx context.Context, cmd *exec.Cmd) error {
	err := cmd.Wait()
	if err == nil || ctx.Err() == nil {
		return err
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return err
	}
	return ctx.Err()
}

func (l *OpenCodeLauncher) processSpec(runtimeDir, workspace, socketPath string, lease ModelRuntimeLease, stdout, stderr io.Writer) (openCodeProcessSpec, error) {
	if filepath.Clean(socketPath) != filepath.Join(filepath.Clean(runtimeDir), openCodeDriverSocketName) {
		return openCodeProcessSpec{}, errors.New("OpenCode driver socket escaped the private runtime")
	}
	resolvedOpenCode, err := filepath.EvalSymlinks(l.config.OpenCodePath)
	if err != nil || !filepath.IsAbs(resolvedOpenCode) {
		return openCodeProcessSpec{}, errors.New("OpenCode executable could not be resolved")
	}
	config, err := json.Marshal(map[string]any{
		"provider": map[string]any{
			"bridge": map[string]any{
				"npm":     "file://" + openCodeSandboxProvider,
				"name":    "MCP Devbox External Driver",
				"options": map[string]any{"socketPath": openCodeSandboxSocket, "runtimeID": lease.RuntimeID, "ttlMs": int64(lease.TimeoutSeconds) * 1000, "timeoutMs": int64(lease.TimeoutSeconds) * 1000},
				"models":  map[string]any{"external-model": map[string]any{"name": "External Model Turn"}},
			},
		},
		"permission": map[string]any{
			"*":                  "allow",
			"external_directory": "deny",
			"webfetch":           "deny",
			"websearch":          "deny",
		},
		"agent":      map[string]any{"title": map[string]any{"disable": true}},
		"autoupdate": false,
	})
	if err != nil {
		return openCodeProcessSpec{}, errors.New("OpenCode local configuration failed")
	}
	home := filepath.Join(runtimeDir, "home")
	for _, dir := range []string{home, filepath.Join(home, ".config"), filepath.Join(home, ".local", "share"), filepath.Join(home, ".local", "state"), filepath.Join(home, ".cache")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return openCodeProcessSpec{}, errors.New("OpenCode private runtime directory failed")
		}
	}
	env := []string{
		"PATH=" + l.config.ToolPath,
		"HOME=" + home,
		"USER=mcpedge",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"TERM=dumb",
		"SHELL=/bin/sh",
	}
	args, err := openCodeBubblewrapArgs(resolvedOpenCode, l.config.ProviderPath, runtimeDir, workspace, lease, string(config), l.config.ToolPath)
	if err != nil {
		return openCodeProcessSpec{}, err
	}
	sandbox, err := parseOpenCodeSandboxArgs(args)
	if err != nil {
		return openCodeProcessSpec{}, err
	}
	if err := validateOpenCodeSandboxSpec(sandbox, l.config.StateRoot, runtimeDir, workspace, l.config.ProviderPath, resolvedOpenCode, l.config.ToolPath, lease); err != nil {
		return openCodeProcessSpec{}, err
	}
	return openCodeProcessSpec{Executable: l.config.BubblewrapPath, Args: args, Dir: workspace, Env: env, Stdout: stdout, Stderr: stderr, Sandbox: sandbox}, nil
}

func openCodeBubblewrapArgs(openCodePath, providerPath, runtimeDir, workspace string, lease ModelRuntimeLease, configJSON, toolPath string) ([]string, error) {
	for _, path := range []string{openCodePath, providerPath, runtimeDir, workspace} {
		if !filepath.IsAbs(path) {
			return nil, errors.New("OpenCode sandbox path is not absolute")
		}
	}
	mountSources := []string{runtimeDir, workspace, providerPath, openCodePath}
	for _, toolDir := range filepath.SplitList(toolPath) {
		if pathInside(openCodeManagedToolRoot, toolDir) {
			mountSources = append(mountSources, toolDir)
		}
	}
	for left := 0; left < len(mountSources); left++ {
		for right := left + 1; right < len(mountSources); right++ {
			if pathInside(mountSources[left], mountSources[right]) || pathInside(mountSources[right], mountSources[left]) {
				return nil, errors.New("OpenCode sandbox mounts overlap")
			}
		}
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--clearenv"}
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	for _, toolDir := range filepath.SplitList(toolPath) {
		if pathInside(openCodeManagedToolRoot, toolDir) {
			args = append(args, "--ro-bind", toolDir, toolDir)
		}
	}
	args = append(args,
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", openCodePath, openCodeSandboxExecutable,
		"--ro-bind", providerPath, openCodeSandboxProvider,
		"--bind", runtimeDir, openCodeSandboxRuntime,
		"--bind", workspace, openCodeSandboxWorkspace,
		"--chdir", openCodeSandboxWorkspace,
		"--setenv", "PATH", toolPath,
		"--setenv", "HOME", openCodeSandboxHome,
		"--setenv", "USER", "mcpedge",
		"--setenv", "LANG", "C.UTF-8",
		"--setenv", "LC_ALL", "C.UTF-8",
		"--setenv", "TERM", "dumb",
		"--setenv", "SHELL", "/bin/sh",
		"--setenv", "XDG_CONFIG_HOME", openCodeSandboxHome+"/.config",
		"--setenv", "XDG_DATA_HOME", openCodeSandboxHome+"/.local/share",
		"--setenv", "XDG_STATE_HOME", openCodeSandboxHome+"/.local/state",
		"--setenv", "XDG_CACHE_HOME", openCodeSandboxHome+"/.cache",
		"--setenv", "OPENCODE_TEST_HOME", openCodeSandboxHome,
		"--setenv", "OPENCODE_CONFIG_CONTENT", configJSON,
		"--setenv", "OPENCODE_AUTH_CONTENT", "{}",
		"--setenv", "OPENCODE_DISABLE_PROJECT_CONFIG", "1",
		"--setenv", "OPENCODE_PURE", "1",
		"--setenv", "OPENCODE_DISABLE_AUTOUPDATE", "1",
		"--setenv", "OPENCODE_DISABLE_AUTOCOMPACT", "1",
		"--setenv", "OPENCODE_DISABLE_MODELS_FETCH", "1",
		"--setenv", "OPENCODE_DISABLE_LSP_DOWNLOAD", "1",
		"--setenv", "OPENCODE_DISABLE_DEFAULT_PLUGINS", "1",
		"--setenv", "OPENCODE_DISABLE_EXTERNAL_SKILLS", "1",
		"--setenv", "OPENCODE_DISABLE_SHARE", "1",
		"--",
		openCodeSandboxExecutable,
		"run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", openCodeSandboxWorkspace, lease.Goal,
	)
	return args, nil
}

func parseOpenCodeSandboxArgs(args []string) (openCodeSandboxSpec, error) {
	spec := openCodeSandboxSpec{Environment: make(map[string]string)}
	for index := 0; index < len(args); {
		switch args[index] {
		case "--die-with-parent":
			spec.DieWithParent = true
			index++
		case "--new-session":
			spec.NewSession = true
			index++
		case "--unshare-all":
			spec.UnshareAll = true
			index++
		case "--share-net":
			spec.ShareNetwork = true
			index++
		case "--clearenv":
			spec.ClearEnv = true
			index++
		case "--ro-bind", "--bind":
			if index+2 >= len(args) {
				return openCodeSandboxSpec{}, errors.New("OpenCode sandbox mount is incomplete")
			}
			spec.Mounts = append(spec.Mounts, openCodeSandboxMount{
				Source: args[index+1], Target: args[index+2],
				Writable: args[index] == "--bind", Kind: "bind",
			})
			index += 3
		case "--proc", "--dev", "--tmpfs":
			if index+1 >= len(args) {
				return openCodeSandboxSpec{}, errors.New("OpenCode sandbox virtual mount is incomplete")
			}
			spec.Mounts = append(spec.Mounts, openCodeSandboxMount{
				Target: args[index+1], Writable: true, Kind: strings.TrimPrefix(args[index], "--"),
			})
			index += 2
		case "--chdir":
			if index+1 >= len(args) {
				return openCodeSandboxSpec{}, errors.New("OpenCode sandbox working directory is missing")
			}
			spec.WorkingDirectory = args[index+1]
			index += 2
		case "--setenv":
			if index+2 >= len(args) {
				return openCodeSandboxSpec{}, errors.New("OpenCode sandbox environment is incomplete")
			}
			if _, duplicate := spec.Environment[args[index+1]]; duplicate {
				return openCodeSandboxSpec{}, errors.New("OpenCode sandbox environment contains duplicate keys")
			}
			spec.Environment[args[index+1]] = args[index+2]
			index += 3
		case "--":
			spec.Command = append([]string(nil), args[index+1:]...)
			index = len(args)
		default:
			return openCodeSandboxSpec{}, errors.New("OpenCode sandbox contains an unsupported Bubblewrap argument")
		}
	}
	return spec, nil
}

func validateOpenCodeSandboxSpec(spec openCodeSandboxSpec, stateRoot, runtimeDir, workspace, providerPath, openCodePath, toolPath string, lease ModelRuntimeLease) error {
	if !spec.DieWithParent || !spec.NewSession || !spec.UnshareAll || spec.ShareNetwork || !spec.ClearEnv {
		return errors.New("OpenCode sandbox isolation flags are incomplete")
	}
	expectedCommand := []string{openCodeSandboxExecutable, "run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", openCodeSandboxWorkspace, lease.Goal}
	if spec.WorkingDirectory != openCodeSandboxWorkspace || !slices.Equal(spec.Command, expectedCommand) {
		return errors.New("OpenCode sandbox command is invalid")
	}
	requiredEnv := map[string]string{
		"PATH": toolPath, "HOME": openCodeSandboxHome, "USER": "mcpedge",
		"LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TERM": "dumb", "SHELL": "/bin/sh",
		"XDG_CONFIG_HOME":                  openCodeSandboxHome + "/.config",
		"XDG_DATA_HOME":                    openCodeSandboxHome + "/.local/share",
		"XDG_STATE_HOME":                   openCodeSandboxHome + "/.local/state",
		"XDG_CACHE_HOME":                   openCodeSandboxHome + "/.cache",
		"OPENCODE_TEST_HOME":               openCodeSandboxHome,
		"OPENCODE_AUTH_CONTENT":            "{}",
		"OPENCODE_DISABLE_PROJECT_CONFIG":  "1",
		"OPENCODE_PURE":                    "1",
		"OPENCODE_DISABLE_AUTOUPDATE":      "1",
		"OPENCODE_DISABLE_AUTOCOMPACT":     "1",
		"OPENCODE_DISABLE_MODELS_FETCH":    "1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD":    "1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS": "1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS": "1",
		"OPENCODE_DISABLE_SHARE":           "1",
	}
	for key, value := range requiredEnv {
		if spec.Environment[key] != value {
			return errors.New("OpenCode sandbox clean environment is incomplete")
		}
	}
	if len(spec.Environment) != len(requiredEnv)+1 {
		return errors.New("OpenCode sandbox environment contains unexpected values")
	}
	if err := validateOpenCodeSandboxConfig(spec.Environment["OPENCODE_CONFIG_CONTENT"], lease); err != nil {
		return err
	}
	mounts := make(map[string]openCodeSandboxMount)
	for _, mount := range spec.Mounts {
		if mount.Target == "" {
			return errors.New("OpenCode sandbox mount target is empty")
		}
		if _, duplicate := mounts[mount.Target]; duplicate {
			return errors.New("OpenCode sandbox mount target is duplicated")
		}
		mounts[mount.Target] = mount
		for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "/mnt/c", "/mnt/d", "/root"} {
			if mount.Target == forbidden || mount.Source == forbidden || pathInside(forbidden, mount.Target) || pathInside(forbidden, mount.Source) {
				return errors.New("OpenCode sandbox exposes a forbidden host path")
			}
		}
	}
	requiredMounts := map[string]openCodeSandboxMount{
		openCodeSandboxWorkspace:  {Source: workspace, Target: openCodeSandboxWorkspace, Writable: true, Kind: "bind"},
		openCodeSandboxRuntime:    {Source: runtimeDir, Target: openCodeSandboxRuntime, Writable: true, Kind: "bind"},
		openCodeSandboxProvider:   {Source: providerPath, Target: openCodeSandboxProvider, Writable: false, Kind: "bind"},
		openCodeSandboxExecutable: {Source: openCodePath, Target: openCodeSandboxExecutable, Writable: false, Kind: "bind"},
		"/proc":                   {Target: "/proc", Writable: true, Kind: "proc"},
		"/dev":                    {Target: "/dev", Writable: true, Kind: "dev"},
		"/tmp":                    {Target: "/tmp", Writable: true, Kind: "tmpfs"},
	}
	for target, expected := range requiredMounts {
		if mounts[target] != expected {
			return errors.New("OpenCode sandbox required mount is missing or has wrong permissions")
		}
	}
	for _, mount := range spec.Mounts {
		if mount.Source == stateRoot || (pathInside(stateRoot, mount.Source) && mount.Source != runtimeDir) {
			return errors.New("OpenCode sandbox exposes private Edge state")
		}
		if mount.Target != openCodeSandboxWorkspace && mount.Target != openCodeSandboxRuntime && mount.Writable && mount.Kind == "bind" {
			return errors.New("OpenCode sandbox exposes an unexpected writable bind mount")
		}
	}
	return nil
}

func validateOpenCodeSandboxConfig(configJSON string, lease ModelRuntimeLease) error {
	var root map[string]any
	if err := json.Unmarshal([]byte(configJSON), &root); err != nil || !hasExactJSONKeys(root, "provider", "permission", "agent", "autoupdate") {
		return errors.New("OpenCode sandbox configuration is invalid")
	}
	provider, ok := root["provider"].(map[string]any)
	if !ok || !hasExactJSONKeys(provider, "bridge") {
		return errors.New("OpenCode sandbox provider configuration is unsafe")
	}
	bridge, ok := provider["bridge"].(map[string]any)
	if !ok || !hasExactJSONKeys(bridge, "npm", "name", "options", "models") || bridge["npm"] != "file:///mcp-provider" || bridge["name"] != "MCP Devbox External Driver" {
		return errors.New("OpenCode sandbox provider configuration is unsafe")
	}
	options, ok := bridge["options"].(map[string]any)
	if !ok || !hasExactJSONKeys(options, "socketPath", "runtimeID", "ttlMs", "timeoutMs") || options["socketPath"] != openCodeSandboxSocket {
		return errors.New("OpenCode sandbox provider options are unsafe")
	}
	runtimeID, runtimeOK := options["runtimeID"].(string)
	ttl, ttlOK := options["ttlMs"].(float64)
	timeout, timeoutOK := options["timeoutMs"].(float64)
	if !runtimeOK || runtimeID != lease.RuntimeID || !ttlOK || !timeoutOK || ttl != float64(lease.TimeoutSeconds)*1000 || timeout != ttl {
		return errors.New("OpenCode sandbox provider options are invalid")
	}
	models, ok := bridge["models"].(map[string]any)
	if !ok || !hasExactJSONKeys(models, "external-model") {
		return errors.New("OpenCode sandbox model configuration is unsafe")
	}
	model, ok := models["external-model"].(map[string]any)
	if !ok || !hasExactJSONKeys(model, "name") || model["name"] != "External Model Turn" {
		return errors.New("OpenCode sandbox model configuration is unsafe")
	}
	permission, ok := root["permission"].(map[string]any)
	if !ok || !hasExactJSONKeys(permission, "*", "external_directory", "webfetch", "websearch") ||
		permission["*"] != "allow" || permission["external_directory"] != "deny" || permission["webfetch"] != "deny" || permission["websearch"] != "deny" {
		return errors.New("OpenCode sandbox permissions are unsafe")
	}
	agent, ok := root["agent"].(map[string]any)
	if !ok || !hasExactJSONKeys(agent, "title") {
		return errors.New("OpenCode sandbox agent configuration is unsafe")
	}
	title, ok := agent["title"].(map[string]any)
	if !ok || !hasExactJSONKeys(title, "disable") || title["disable"] != true || root["autoupdate"] != false {
		return errors.New("OpenCode sandbox agent configuration is unsafe")
	}
	return nil
}

func hasExactJSONKeys(value map[string]any, expected ...string) bool {
	if len(value) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

func (l *OpenCodeLauncher) verifyLocalInstallation(ctx context.Context, workspace string, lease ModelRuntimeLease) error {
	resolved, err := filepath.EvalSymlinks(l.config.OpenCodePath)
	if err != nil {
		return errors.New("pinned OpenCode executable is unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("pinned OpenCode executable is unsafe")
	}
	if l.config.DriverPath != "" {
		info, err := os.Lstat(l.config.DriverPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("model-turn driver executable is unsafe")
		}
	}
	bubblewrapInfo, err := os.Lstat(l.config.BubblewrapPath)
	if err != nil || !bubblewrapInfo.Mode().IsRegular() || bubblewrapInfo.Mode()&os.ModeSymlink != 0 || bubblewrapInfo.Mode().Perm()&0o111 == 0 || bubblewrapInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("bubblewrap executable is unsafe")
	}
	if err := verifyOpenCodeLock(l.config.IntegrityPath); err != nil {
		return err
	}
	if err := verifyProviderPackage(l.config.ProviderPath); err != nil {
		return err
	}
	verifyRuntime, err := os.MkdirTemp(l.config.SocketRoot, "verify-")
	if err != nil {
		return errors.New("bubblewrap verification runtime could not be created")
	}
	defer removePrivateRuntimeDir(verifyRuntime, l.config.SocketRoot)
	if err := os.Chmod(verifyRuntime, 0o700); err != nil {
		return errors.New("bubblewrap verification runtime is unsafe")
	}
	spec, err := l.processSpec(verifyRuntime, workspace, filepath.Join(verifyRuntime, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		return err
	}
	if l.verifySandbox == nil {
		return errors.New("bubblewrap verification is unavailable")
	}
	return l.verifySandbox(ctx, spec)
}

func (l *OpenCodeLauncher) verifyOpenCodeSandbox(ctx context.Context, spec openCodeProcessSpec) error {
	separator := -1
	for index, arg := range spec.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return errors.New("bubblewrap command separator is missing")
	}
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stdout := newBoundedCapture(4096)
	stderr := newBoundedCapture(4096)
	spec.Args = append(append([]string(nil), spec.Args[:separator+1]...), openCodeSandboxExecutable, "--version")
	spec.Stdout = stdout
	spec.Stderr = stderr
	started := time.Now()
	result := runOpenCodeProcess(versionCtx, spec)
	if result.Err != nil || result.ExitCode != 0 {
		diagnostic := classifyBubblewrapFailure(bubblewrapStageHelperExec, result.Err, stderr.String(), time.Since(started))
		return fmt.Errorf("bubblewrap verification failed (%s)", diagnostic.Code)
	}
	if stdout.Truncated() || strings.TrimSpace(stdout.String()) != PinnedOpenCodeVersion {
		return errors.New("OpenCode version does not match the pinned release")
	}
	return nil
}

func verifyOpenCodeLock(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("OpenCode integrity lock is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) > 1<<20 {
		return errors.New("OpenCode integrity lock is unavailable")
	}
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&lock); err != nil {
		return errors.New("OpenCode integrity lock is invalid")
	}
	entry, ok := lock.Packages["node_modules/"+PinnedOpenCodePackage]
	if !ok || entry.Version != PinnedOpenCodeVersion || entry.Integrity != PinnedOpenCodeIntegrity {
		return errors.New("OpenCode integrity does not match the pinned release")
	}
	return nil
}

func verifyProviderPackage(path string) error {
	if err := rejectSymlinkPath(path); err != nil {
		return errors.New("OpenCode external driver path is unsafe")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("OpenCode external driver is unavailable")
	}
	body, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil || len(body) > 64<<10 {
		return errors.New("OpenCode external driver manifest is unavailable")
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Exports string `json:"exports"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil || manifest.Name != OpenCodeExternalDriverPackage || manifest.Version == "" || manifest.Exports != "./index.js" {
		return errors.New("OpenCode external driver manifest is invalid")
	}
	return nil
}

func validateLauncherLease(lease ModelRuntimeLease) error {
	if !remoteRuntimeIDPattern.MatchString(lease.RuntimeID) || !deviceIDPattern.MatchString(lease.DeviceID) || !workspaceIDPattern.MatchString(lease.WorkspaceID) || lease.Controller != modelturn.ControllerRemoteEdge || lease.State != modelturn.RuntimeStateStarting || lease.ProviderProfile != remoteProviderProfile || lease.TimeoutSeconds < 1 || lease.TimeoutSeconds > int(modelturn.MaxTurnTTL/time.Second) {
		return errors.New("OpenCode runtime lease is invalid")
	}
	if lease.Goal == "" || int64(len(lease.Goal)) > modelturn.MaxGoalBodyBytes || !goalDigestPattern.MatchString(lease.GoalDigest) {
		return errors.New("OpenCode runtime goal is invalid")
	}
	sum := sha256.Sum256([]byte(lease.Goal))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if digest != lease.GoalDigest {
		return errors.New("OpenCode runtime goal digest is invalid")
	}
	return nil
}

func runOpenCodeProcess(ctx context.Context, spec openCodeProcessSpec) openCodeProcessResult {
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append([]string(nil), spec.Env...)
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if err == nil {
		return openCodeProcessResult{ExitCode: 0}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return openCodeProcessResult{ExitCode: exitErr.ExitCode(), Err: err}
	}
	return openCodeProcessResult{ExitCode: -1, Err: err}
}

func localTLSCAPath() string {
	value := strings.TrimSpace(os.Getenv("SSL_CERT_FILE"))
	if value == "" || !filepath.IsAbs(value) {
		return ""
	}
	path := filepath.Clean(value)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return ""
	}
	return path
}

func waitForPrivateDriverSocketOrExit(ctx context.Context, path string, uid int, driverDone <-chan error) (bool, error) {
	ready := make(chan error, 1)
	go func() { ready <- waitForPrivateDriverSocket(ctx, path, uid) }()
	select {
	case err := <-ready:
		return false, err
	case err := <-driverDone:
		if err == nil {
			err = errors.New("OpenCode model-turn driver stopped before creating its socket")
		}
		return true, fmt.Errorf("OpenCode model-turn driver could not start: %w", err)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func waitForPrivateDriverSocket(ctx context.Context, path string, uid int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByUID(info, uid) {
				return errors.New("OpenCode model-turn socket is not private")
			}
			parent, parentErr := os.Lstat(filepath.Dir(path))
			if parentErr != nil || !parent.IsDir() || parent.Mode().Perm() != 0o700 || !ownedByUID(parent, uid) {
				return errors.New("OpenCode model-turn socket directory is not private")
			}
			if info.Mode().Perm() == 0o600 {
				return nil
			}
			err = os.ErrNotExist
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("OpenCode model-turn socket is unavailable")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ownedByUID(info os.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}

func validateOpenCodeToolPath(value string) error {
	allowedSystem := map[string]struct{}{
		"/usr/local/sbin": {}, "/usr/local/bin": {}, "/usr/sbin": {},
		"/usr/bin": {}, "/sbin": {}, "/bin": {},
	}
	seen := make(map[string]struct{})
	parts := filepath.SplitList(value)
	if len(parts) == 0 {
		return errors.New("OpenCode tool path is empty")
	}
	for _, part := range parts {
		clean := filepath.Clean(strings.TrimSpace(part))
		if clean == "." || !filepath.IsAbs(clean) {
			return errors.New("OpenCode tool path must contain only absolute local directories")
		}
		if _, duplicate := seen[clean]; duplicate {
			return errors.New("OpenCode tool path contains duplicate directories")
		}
		seen[clean] = struct{}{}
		if _, ok := allowedSystem[clean]; ok {
			continue
		}
		if !pathInside(openCodeManagedToolRoot, clean) {
			return errors.New("OpenCode tool path is outside the local managed allowlist")
		}
		if err := rejectSymlinkPath(clean); err != nil {
			return errors.New("OpenCode managed tool path is unsafe")
		}
		info, err := os.Lstat(clean)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("OpenCode managed tool path is unavailable or writable")
		}
	}
	return nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func openCodeRuntimeDir(socketRoot, runtimeID string) string {
	id := strings.TrimPrefix(runtimeID, "mr_")
	if len(id) > 16 {
		id = id[:16]
	}
	return filepath.Join(filepath.Clean(socketRoot), id)
}

func removePrivateRuntimeDir(path, socketRoot string) {
	root := filepath.Clean(socketRoot)
	relative, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return
	}
	_ = os.RemoveAll(path)
}

func (l *OpenCodeLauncher) killSwitchActive() bool {
	info, err := os.Lstat(l.config.StopPath)
	if err == nil {
		return !info.IsDir()
	}
	return !errors.Is(err, os.ErrNotExist)
}

type boundedCapture struct {
	mu        sync.Mutex
	limit     int64
	data      []byte
	truncated bool
}

func newBoundedCapture(limit int64) *boundedCapture {
	return &boundedCapture{limit: limit}
}

func (b *boundedCapture) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(len(b.data))
	if remaining > 0 {
		accepted := int64(len(payload))
		if accepted > remaining {
			accepted = remaining
		}
		b.data = append(b.data, payload[:accepted]...)
	}
	if int64(len(payload)) > remaining {
		b.truncated = true
	}
	return len(payload), nil
}

func (b *boundedCapture) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

func (b *boundedCapture) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

type boundedSink struct {
	mu        sync.Mutex
	limit     int64
	written   int64
	truncated bool
	tail      []byte
}

func newBoundedSink(limit int64) *boundedSink {
	return &boundedSink{limit: limit}
}

func (b *boundedSink) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.written
	if remaining > 0 {
		accepted := int64(len(payload))
		if accepted > remaining {
			accepted = remaining
		}
		b.written += accepted
	}
	if b.written >= b.limit && len(payload) > 0 {
		b.truncated = true
	}
	const tailLimit = 8192
	if len(payload) >= tailLimit {
		b.tail = append(b.tail[:0], payload[len(payload)-tailLimit:]...)
	} else {
		b.tail = append(b.tail, payload...)
		if len(b.tail) > tailLimit {
			b.tail = append(b.tail[:0], b.tail[len(b.tail)-tailLimit:]...)
		}
	}
	return len(payload), nil
}

func safePermissionSignal(message string) string {
	text := strings.ToLower(message)
	if !strings.Contains(text, "permission denied") && !strings.Contains(text, "eacces") && !strings.Contains(text, "eperm") && !strings.Contains(text, "operation not permitted") {
		return ""
	}
	checks := []struct {
		code    string
		phrases []string
	}{
		{"permission_ptrace", []string{"ptrace"}},
		{"permission_connect", []string{"connect", "socket"}},
		{"permission_spawn", []string{"spawn", "execve", "executable"}},
		{"permission_mkdir", []string{"mkdir", "create directory"}},
		{"permission_open", []string{"open ", "open'", "open\""}},
		{"permission_rename", []string{"rename"}},
		{"permission_remove", []string{"unlink", "remove", "rmdir"}},
		{"permission_chmod", []string{"chmod", "chown"}},
		{"permission_read_dir", []string{"scandir", "readdir", "read directory"}},
		{"permission_stat", []string{"lstat", "stat "}},
		{"permission_write", []string{"write"}},
		{"permission_read", []string{"read"}},
	}
	for _, check := range checks {
		for _, phrase := range check.phrases {
			if strings.Contains(text, phrase) {
				return check.code
			}
		}
	}
	return "permission_other"
}

func safeProviderMessageSignal(message string) string {
	text := strings.ToLower(message)
	if signal := safePermissionSignal(text); signal != "" {
		return signal
	}
	checks := []struct {
		code    string
		phrases []string
	}{
		{"cli", []string{"unknown argument", "unknown option", "invalid argument", "usage:"}},
		{"provider_load", []string{"cannot find package", "module not found", "failed to resolve", "failed to load provider", "provider not found"}},
		{"driver_connect", []string{"econnrefused", "connection refused", "connect: no such file", "socket not found", "socket hang up"}},
		{"config", []string{"invalid config", "configuration is invalid", "failed to parse config"}},
		{"model", []string{"model not found", "unknown model", "invalid model"}},
		{"not_found", []string{"enoent", "no such file or directory", "not found", "does not exist", "missing file", "missing directory"}},
		{"unknown_type", []string{"cannot read properties", "is not a function", "undefined is not", "null is not", "typeerror"}},
		{"unknown_connection", []string{"connection reset", "connection closed", "network error"}},
		{"unknown_timeout", []string{"timed out", "timeout"}},
		{"prompt_shape", []string{"prompt must be an array", "message content must be an array", "message must be an object"}},
		{"prompt_role", []string{"unsupported message role", "unsupported message part"}},
		{"tool_shape", []string{"provider tools are not supported", "duplicate tool name", "tool input schema", "tool choice"}},
		{"request_limit", []string{"canonical model request exceeds", "request exceeds the bridge limit"}},
		{"runtime_status", []string{"runtime_status"}},
		{"request_stage", []string{"request_stage"}},
		{"turn_create", []string{"turn_create"}},
		{"response_wait", []string{"response_wait"}},
		{"response_identity", []string{"response_identity"}},
		{"runtime_status", []string{"runtime status id mismatch", "runtime sequence is invalid", "runtime is terminal"}},
		{"driver_invalid_request", []string{"model turn request is invalid", "invalid_request"}},
		{"driver_status", []string{"model turn driver status", "model turn driver returned non-json"}},
		{"turn_identity", []string{"created turn identity mismatch", "created turn offered-tool set mismatch"}},
		{"response_identity", []string{"model response identity mismatch", "staged request identity mismatch"}},
		{"response_shape", []string{"model response", "model tool calls", "finish reason", "unoffered tool", "usage"}},
		{"abort", []string{"model turn aborted", "model turn timed out"}},
		{"provider", []string{"provider"}},
		{"tool_shape", []string{"tool"}},
		{"socket", []string{"enoent", "econnrefused", "econnreset", "epipe", "socket hang up"}},
	}
	for _, check := range checks {
		for _, phrase := range check.phrases {
			if strings.Contains(text, phrase) {
				return check.code
			}
		}
	}
	return ""
}

func structuredOpenCodeErrorSignal(content []byte) string {
	lines := bytes.Split(content, []byte{'\n'})
	for index := len(lines) - 1; index >= 0; index-- {
		var event struct {
			Type  string `json:"type"`
			Error struct {
				Name string                     `json:"name"`
				Data map[string]json.RawMessage `json:"data"`
			} `json:"error"`
		}
		if json.Unmarshal(lines[index], &event) != nil || event.Type != "error" {
			continue
		}
		switch strings.ToLower(event.Error.Name) {
		case "providerautherror":
			return "provider_auth"
		case "messageoutputlengtherror":
			return "output_length"
		case "unknownerror":
			if raw, ok := event.Error.Data["message"]; ok {
				var message string
				if json.Unmarshal(raw, &message) == nil {
					if signal := safeProviderMessageSignal(message); signal != "" {
						return signal
					}
				}
			}
			for _, field := range []string{"name", "code", "status", "statusCode"} {
				raw, ok := event.Error.Data[field]
				if !ok {
					continue
				}
				var value string
				if json.Unmarshal(raw, &value) == nil {
					value = strings.ToLower(value)
					switch {
					case strings.Contains(value, "typeerror"):
						return "unknown_type"
					case strings.Contains(value, "api"):
						return "unknown_api"
					case strings.Contains(value, "timeout"):
						return "unknown_timeout"
					case strings.Contains(value, "connection") || strings.Contains(value, "socket"):
						return "unknown_connection"
					}
				}
			}
			return "unknown_error"
		default:
			if event.Error.Name != "" {
				return "named_error"
			}
		}
	}
	return ""
}

// SafeOpenCodeFailureSignal classifies bounded OpenCode output without
// returning messages, paths, prompts, arguments, or response bodies.
func SafeOpenCodeFailureSignal(content []byte) string {
	if len(content) > 8192 {
		content = content[len(content)-8192:]
	}
	text := strings.ToLower(string(content))
	if signal := structuredOpenCodeErrorSignal(content); signal != "" {
		return signal
	}
	if signal := safePermissionSignal(text); signal != "" {
		return signal
	}
	checks := []struct {
		code    string
		phrases []string
	}{
		{"cli", []string{"unknown argument", "unknown option", "invalid argument", "usage:"}},
		{"provider_load", []string{"cannot find package", "module not found", "failed to resolve", "failed to load provider", "provider not found"}},
		{"driver_connect", []string{"econnrefused", "connection refused", "connect: no such file", "socket not found"}},
		{"config", []string{"invalid config", "configuration is invalid", "failed to parse config"}},
		{"model", []string{"model not found", "unknown model", "invalid model"}},
		{"provider", []string{"provider", "bridge/external-model"}},
	}
	for _, check := range checks {
		for _, phrase := range check.phrases {
			if strings.Contains(text, phrase) {
				return check.code
			}
		}
	}
	if strings.Contains(text, `"type":"error"`) || strings.Contains(text, "error:") {
		return "runtime_error"
	}
	return "unknown"
}

func (b *boundedSink) FailureSignal() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return SafeOpenCodeFailureSignal(b.tail)
}

func (b *boundedSink) TailString() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.tail...))
}

func (b *boundedSink) ContainsFold(marker string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(strings.ToLower(string(b.tail)), strings.ToLower(marker))
}

func (b *boundedSink) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func (r OpenCodeLaunchResult) String() string {
	return fmt.Sprintf("runtime=%s workspace=%s state=%s exit=%d truncated=%t", r.RuntimeID, r.WorkspaceID, r.State, r.ExitCode, r.OutputTruncated)
}
