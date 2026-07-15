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
	StateRoot     string
	SocketRoot    string
	OpenCodePath  string
	ProviderPath  string
	IntegrityPath string
	StopPath      string
	ToolPath      string
	OutputLimit   int64
	Heartbeat     time.Duration
	HTTPClient    *http.Client
	Workspaces    *WorkspaceRegistry
	Journal       *OpenCodeRuntimeJournal
}

type OpenCodeLaunchResult struct {
	RuntimeID       string
	WorkspaceID     string
	State           OpenCodeLocalState
	ExitCode        int
	OutputTruncated bool
}

type OpenCodeLauncher struct {
	config           OpenCodeLauncherConfig
	remoteFactory    func(ModelRuntimeLease) (OpenCodeRemoteTransport, error)
	runProcess       func(context.Context, openCodeProcessSpec) openCodeProcessResult
	resolveWorkspace func(string) (string, error)
	effectiveUID     func() int
	now              func() time.Time
	allowRootTest    bool
}

type openCodeProcessSpec struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	Stdout     io.Writer
	Stderr     io.Writer
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
	config.ProviderPath = filepath.Clean(strings.TrimSpace(config.ProviderPath))
	config.IntegrityPath = filepath.Clean(strings.TrimSpace(config.IntegrityPath))
	if !filepath.IsAbs(config.StateRoot) || !filepath.IsAbs(config.SocketRoot) || !filepath.IsAbs(config.OpenCodePath) || !filepath.IsAbs(config.ProviderPath) || !filepath.IsAbs(config.IntegrityPath) {
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
		config.ToolPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
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
	launcher.resolveWorkspace = config.Workspaces.Resolve
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
	entry, created, err := l.config.Journal.Begin(ctx, lease.RuntimeID, lease.WorkspaceID, lease.GoalDigest, lease.ProviderProfile)
	if err != nil {
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
			return result, ErrOpenCodeInterrupted
		}
	}
	failLocal := func(state OpenCodeLocalState, exitCode int, truncated bool) {
		_ = l.config.Journal.Finish(context.Background(), lease.RuntimeID, state, exitCode, truncated)
		result.State = state
		result.ExitCode = exitCode
		result.OutputTruncated = truncated
	}
	remote, err := l.remoteFactory(lease)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		return result, err
	}
	defer remote.Close()
	workspace, err := l.resolveWorkspace(lease.WorkspaceID)
	if err != nil {
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	if err := l.verifyLocalInstallation(ctx); err != nil {
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
	driverDone := make(chan error, 1)
	go func() { driverDone <- modelturn.ServeDriverTransport(runCtx, socketPath, remote, nil) }()
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
	spec, err := l.processSpec(runtimeDir, workspace, socketPath, lease, stdout, stderr)
	if err != nil {
		cancel()
		<-driverDone
		failLocal(OpenCodeLocalFailed, -1, false)
		_, _ = remote.Failed(context.Background(), "")
		return result, err
	}
	processDone := make(chan openCodeProcessResult, 1)
	go func() { processDone <- l.runProcess(runCtx, spec) }()
	heartbeatDone := make(chan modelturn.Runtime, 1)
	heartbeatErr := make(chan error, 1)
	go l.monitorRuntime(runCtx, cancel, remote, heartbeatDone, heartbeatErr)

	processResult := <-processDone
	runContextErr := runCtx.Err()
	cancel()
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
	truncated := stdout.Truncated() || stderr.Truncated()
	if terminalRuntime.State == modelturn.RuntimeStateCancelled {
		failLocal(OpenCodeLocalCancelled, processResult.ExitCode, truncated)
		return result, context.Canceled
	}
	if l.killSwitchActive() {
		failLocal(OpenCodeLocalCancelled, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, ErrKillSwitch
	}
	if errors.Is(runContextErr, context.DeadlineExceeded) {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, context.DeadlineExceeded
	}
	if processResult.Err != nil || processResult.ExitCode != 0 {
		failLocal(OpenCodeLocalFailed, processResult.ExitCode, truncated)
		_, _ = remote.Failed(context.Background(), "")
		return result, errors.New("OpenCode terminated unexpectedly")
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

func (l *OpenCodeLauncher) processSpec(runtimeDir, workspace, socketPath string, lease ModelRuntimeLease, stdout, stderr io.Writer) (openCodeProcessSpec, error) {
	config, err := json.Marshal(map[string]any{
		"provider": map[string]any{
			"bridge": map[string]any{
				"npm":     "file://" + l.config.ProviderPath,
				"name":    "MCP Devbox External Driver",
				"options": map[string]any{"socketPath": socketPath, "runtimeID": lease.RuntimeID, "ttlMs": int64(lease.TimeoutSeconds) * 1000, "timeoutMs": int64(lease.TimeoutSeconds) * 1000},
				"models":  map[string]any{"external-model": map[string]any{"name": "External Model Turn"}},
			},
		},
		"permission": map[string]any{"*": "allow"},
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
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"OPENCODE_TEST_HOME=" + home,
		"OPENCODE_CONFIG_CONTENT=" + string(config),
		"OPENCODE_AUTH_CONTENT={}",
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_PURE=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_AUTOCOMPACT=1",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
		"OPENCODE_DISABLE_SHARE=1",
	}
	args := []string{"run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", workspace, lease.Goal}
	return openCodeProcessSpec{Executable: l.config.OpenCodePath, Args: args, Dir: workspace, Env: env, Stdout: stdout, Stderr: stderr}, nil
}

func (l *OpenCodeLauncher) verifyLocalInstallation(ctx context.Context) error {
	resolved, err := filepath.EvalSymlinks(l.config.OpenCodePath)
	if err != nil {
		return errors.New("pinned OpenCode executable is unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("pinned OpenCode executable is unsafe")
	}
	if err := verifyOpenCodeLock(l.config.IntegrityPath); err != nil {
		return err
	}
	if err := verifyProviderPackage(l.config.ProviderPath); err != nil {
		return err
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output := newBoundedCapture(4096)
	cmd := exec.CommandContext(versionCtx, l.config.OpenCodePath, "--version")
	cmd.Env = []string{"PATH=" + l.config.ToolPath, "HOME=" + l.config.StateRoot, "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1"}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || output.Truncated() || strings.TrimSpace(output.String()) != PinnedOpenCodeVersion {
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
			if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByUID(info, uid) {
				return errors.New("OpenCode model-turn socket is not private")
			}
			parent, parentErr := os.Lstat(filepath.Dir(path))
			if parentErr != nil || !parent.IsDir() || parent.Mode().Perm() != 0o700 || !ownedByUID(parent, uid) {
				return errors.New("OpenCode model-turn socket directory is not private")
			}
			return nil
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
	return len(payload), nil
}

func (b *boundedSink) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func (r OpenCodeLaunchResult) String() string {
	return fmt.Sprintf("runtime=%s workspace=%s state=%s exit=%d truncated=%t", r.RuntimeID, r.WorkspaceID, r.State, r.ExitCode, r.OutputTruncated)
}
