//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	htbLabResolveSSH     = findSafeLinuxTool
	htbLabSelfExecutable = os.Executable
	htbLabRunSSHProcess  = runHTBLabSSHProcess
)

type HTBLabBrokerConfig struct {
	SocketPath string
	StateRoot  string
	Workspace  Workspace
	RuntimeID  string
	ExpiresAt  time.Time
	ToolPath   string
	Probe      LinuxNetworkProbe
}

func StartHTBLabBroker(ctx context.Context, config HTBLabBrokerConfig) (<-chan error, error) {
	if config.Workspace.Profile != WorkspaceProfileLinuxWorkcell || config.Workspace.Mode != WorkspaceModeHTBLinux ||
		config.RuntimeID == "" || config.SocketPath == "" || config.StateRoot == "" || !config.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("HTB lab broker configuration is invalid")
	}
	if filepath.Base(config.SocketPath) != HTBLabBrokerSocketName {
		return nil, errors.New("HTB lab broker socket name is invalid")
	}
	if err := prepareDriverSocketParent(filepath.Dir(config.SocketPath)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(config.SocketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUID(info) {
			return nil, errors.New("existing HTB lab broker socket is unsafe")
		}
		_ = os.Remove(config.SocketPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("HTB lab broker socket is unavailable")
	}
	listener, err := net.Listen("unix", config.SocketPath)
	if err != nil {
		return nil, errors.New("HTB lab broker could not listen")
	}
	if err := os.Chmod(config.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(config.SocketPath)
		return nil, errors.New("HTB lab broker socket permissions failed")
	}
	broker := &htbLabBroker{config: config, sessions: make(map[string]htbLabSession), now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/status", broker.status)
	mux.HandleFunc("POST /v1/auth-validate", broker.authValidate)
	mux.HandleFunc("POST /v1/command", broker.command)
	mux.HandleFunc("POST /v1/command-save", broker.commandSave)
	mux.HandleFunc("POST /v1/command-credential-stdin", broker.commandCredentialStdin)
	mux.HandleFunc("POST /v1/session-close", broker.sessionClose)
	mux.HandleFunc("POST /v1/ssh-exec", broker.sshExec)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() {
		defer os.Remove(config.SocketPath)
		errCh := make(chan error, 1)
		go func() { errCh <- server.Serve(listener) }()
		select {
		case <-ctx.Done():
			broker.closeAllSessions()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
			_ = server.Close()
			done <- ctx.Err()
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				done <- nil
			} else {
				done <- err
			}
		}
	}()
	return done, nil
}

type htbLabBroker struct {
	config   HTBLabBrokerConfig
	attempts atomic.Uint32
	mu       sync.Mutex
	sessions map[string]htbLabSession
	now      func() time.Time
}

func (broker *htbLabBroker) sshExec(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		writeHTBLabBrokerError(writer, http.StatusUnsupportedMediaType, "invalid_content_type")
		return
	}
	input, err := decodeHTBLabBrokerRequest(request.Body)
	if err != nil {
		writeHTBLabBrokerError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if broker.attempts.Add(1) > 64 {
		writeHTBLabBrokerError(writer, http.StatusTooManyRequests, "attempt_limit")
		return
	}
	response, err := broker.executeSSH(request.Context(), input)
	if err != nil {
		writeHTBLabBrokerError(writer, http.StatusBadGateway, "ssh_failed")
		return
	}
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(response)
}

func (broker *htbLabBroker) executeSSH(parent context.Context, request HTBLabSSHRequest) (HTBLabSSHResponse, error) {
	validated, err := validateHTBLabSSHRequest(request)
	if err != nil {
		return HTBLabSSHResponse{}, err
	}
	request = validated
	probe := broker.config.Probe
	if probe == nil {
		probe = systemLinuxNetworkProbe{}
	}
	if _, err := preflightHTBLinux(parent, broker.config.Workspace, probe); err != nil {
		return HTBLabSSHResponse{}, err
	}
	password, err := extractHTBLabCredential(broker.config.Workspace.Path, request)
	if err != nil {
		return HTBLabSSHResponse{}, err
	}
	defer zeroHTBBytes(password)
	return broker.executeSSHWithCredential(parent, request, password)
}

func (broker *htbLabBroker) executeSSHWithCredential(parent context.Context, request HTBLabSSHRequest, password []byte) (HTBLabSSHResponse, error) {
	validated, err := validateHTBLabSSHRequest(request)
	if err != nil {
		return HTBLabSSHResponse{}, err
	}
	request = validated
	sshPath, ok := htbLabResolveSSH("ssh", broker.config.ToolPath)
	if !ok {
		return HTBLabSSHResponse{}, errors.New("OpenSSH client is unavailable")
	}
	secretDir := filepath.Join(broker.config.StateRoot, "lab-secrets")
	if err := preparePrivateRoot(secretDir); err != nil {
		return HTBLabSSHResponse{}, errors.New("lab secret root is unavailable")
	}
	secretFile, err := os.CreateTemp(secretDir, ".ssh-askpass-*")
	if err != nil {
		return HTBLabSSHResponse{}, errors.New("lab SSH credential staging failed")
	}
	secretPath := secretFile.Name()
	defer os.Remove(secretPath)
	if err := secretFile.Chmod(0o600); err != nil {
		_ = secretFile.Close()
		return HTBLabSSHResponse{}, errors.New("lab SSH credential permissions failed")
	}
	if _, err := secretFile.Write(password); err != nil {
		_ = secretFile.Close()
		return HTBLabSSHResponse{}, errors.New("lab SSH credential staging failed")
	}
	if err := secretFile.Sync(); err != nil {
		_ = secretFile.Close()
		return HTBLabSSHResponse{}, errors.New("lab SSH credential staging failed")
	}
	if err := secretFile.Close(); err != nil {
		return HTBLabSSHResponse{}, errors.New("lab SSH credential staging failed")
	}
	self, err := htbLabSelfExecutable()
	if err != nil {
		return HTBLabSSHResponse{}, errors.New("mcp-edge executable is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, htbLabTimeout(request))
	defer cancel()
	args := buildHTBLabSSHArgs(request, broker.config.Workspace.TargetIP)
	environment := []string{
		"PATH=" + broker.config.ToolPath,
		"HOME=" + secretDir,
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"DISPLAY=mcp-devbox:0",
		"SSH_ASKPASS=" + self,
		"SSH_ASKPASS_REQUIRE=force",
		"MCP_DEVBOX_ASKPASS_FILE=" + secretPath,
		"MCP_DEVBOX_ASKPASS_RUNTIME=" + broker.config.RuntimeID,
	}
	stdin := []byte(nil)
	if request.PasswordStdin {
		stdin = append(append([]byte(nil), password...), '\n')
		defer zeroHTBBytes(stdin)
	}
	stdout := &boundedHTBLabCapture{limit: htbLabSSHOutputLimit}
	stderr := &boundedHTBLabCapture{limit: 256 << 10}
	exitCode, runErr := classifyHTBLabSSHProcessResult(ctx, htbLabRunSSHProcess(ctx, sshPath, args, environment, stdin, stdout, stderr))
	if runErr != nil {
		return HTBLabSSHResponse{}, runErr
	}
	response, err := newHTBLabSSHResponse(broker.config.Workspace.Path, request, broker.config.Workspace.TargetIP, stdout.Bytes(), stderr.Bytes())
	response.ExitCode = exitCode
	response.Truncated = stdout.truncated || stderr.truncated
	if request.SaveOutput != "" {
		zeroHTBBytes(stdout.Bytes())
		zeroHTBBytes(stderr.Bytes())
	}
	return response, err
}

func classifyHTBLabSSHProcessResult(ctx context.Context, runErr error) (int, error) {
	if runErr == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return 0, errors.New("lab SSH command timed out or was cancelled")
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() <= 0 || exitErr.ExitCode() == 255 {
		return 0, errors.New("lab SSH transport failed")
	}
	return exitErr.ExitCode(), nil
}

func buildHTBLabSSHArgs(request HTBLabSSHRequest, target string) []string {
	args := []string{
		"-o", "BatchMode=no",
		"-o", "PreferredAuthentications=password,keyboard-interactive",
		"-o", "PubkeyAuthentication=no",
		"-o", "NumberOfPasswordPrompts=1",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-p", fmt.Sprintf("%d", request.Port),
	}
	if request.PTY {
		args = append(args, "-tt")
	} else {
		args = append(args, "-T")
	}
	return append(args, "--", request.Username+"@"+target, request.Command)
}

func writeHTBLabBrokerError(writer http.ResponseWriter, status int, code string) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(HTBLabSSHResponse{Status: "error", ErrorCode: code})
}

func prepareDriverSocketParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUID(info) {
		return errors.New("HTB lab broker socket directory is unsafe")
	}
	return nil
}

func ownedByCurrentUID(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

type boundedHTBLabCapture struct {
	buffer    bytes.Buffer
	limit     int64
	truncated bool
}

func (capture *boundedHTBLabCapture) Write(value []byte) (int, error) {
	if capture.truncated {
		return len(value), nil
	}
	remaining := capture.limit - int64(capture.buffer.Len())
	if remaining <= 0 {
		capture.truncated = true
		return len(value), nil
	}
	write := value
	if int64(len(write)) > remaining {
		write = write[:remaining]
		capture.truncated = true
	}
	_, _ = capture.buffer.Write(write)
	return len(value), nil
}

func (capture *boundedHTBLabCapture) Bytes() []byte {
	return capture.buffer.Bytes()
}

func ReadHTBLabBrokerResponse(reader io.Reader) (HTBLabSSHResponse, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, htbLabSSHOutputLimit*6+(512<<10)))
	decoder.DisallowUnknownFields()
	var response HTBLabSSHResponse
	if err := decoder.Decode(&response); err != nil {
		return HTBLabSSHResponse{}, errors.New("lab broker response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return HTBLabSSHResponse{}, errors.New("lab broker response has trailing data")
	}
	if response.Status != "ok" {
		return HTBLabSSHResponse{}, errors.New("lab broker rejected the SSH request")
	}
	if response.Target == "" || response.Username == "" || strings.ContainsAny(response.SavedPath, "\r\n") {
		return HTBLabSSHResponse{}, errors.New("lab broker response is invalid")
	}
	return response, nil
}

func runHTBLabSSHProcess(ctx context.Context, executable string, args, environment []string, stdin []byte, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = environment
	command.Stdin = bytes.NewReader(stdin)
	command.Stdout = stdout
	command.Stderr = stderr
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
	return command.Run()
}
