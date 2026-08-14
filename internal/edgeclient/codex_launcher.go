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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/codexadapter"
)

const (
	PinnedCodexVersion       = "0.147.0"
	pinnedCodexTag           = "rust-v0.147.0"
	pinnedCodexSourceCommit  = "be6e8eac029b183056b7e4402879f15d2c85f61b"
	pinnedCodexReleaseURL    = "https://github.com/openai/codex/releases/tag/rust-v0.147.0"
	pinnedCodexAsset         = "codex-x86_64-unknown-linux-musl.tar.gz"
	pinnedCodexArchiveSHA256 = "0246e2e773834e07f0fb5249ed6ebad12e4591e608f8c7bb97dd6a9690544c36"
	pinnedCodexBinarySHA256  = "cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40"
	codexModelID             = "mcp-devbox-codex"
	codexSandboxExecutable   = "/mcp-codex"
)

type runtimeHarness string

const (
	runtimeHarnessOpenCode runtimeHarness = "opencode"
	runtimeHarnessCodex    runtimeHarness = "codex"
)

type CodexLauncherConfig struct {
	StateRoot            string
	SocketRoot           string
	CodexPath            string
	CodexPinPath         string
	BubblewrapPath       string
	StopPath             string
	ToolPath             string
	OutputLimit          int64
	Heartbeat            time.Duration
	RuntimeStartupBudget time.Duration
	HTTPClient           *http.Client
	Workspaces           *WorkspaceRegistry
	Journal              *OpenCodeRuntimeJournal
}

func NewCodexLauncher(config CodexLauncherConfig) (*OpenCodeLauncher, error) {
	return newRuntimeLauncher(OpenCodeLauncherConfig{
		StateRoot: config.StateRoot, SocketRoot: config.SocketRoot,
		CodexPath: config.CodexPath, CodexPinPath: config.CodexPinPath,
		BubblewrapPath: config.BubblewrapPath, StopPath: config.StopPath, ToolPath: config.ToolPath,
		OutputLimit: config.OutputLimit, Heartbeat: config.Heartbeat, RuntimeStartupBudget: config.RuntimeStartupBudget,
		HTTPClient: config.HTTPClient, Workspaces: config.Workspaces, Journal: config.Journal,
	}, runtimeHarnessCodex)
}

type codexPin struct {
	Version                string   `json:"version"`
	Tag                    string   `json:"tag"`
	SourceCommit           string   `json:"source_commit"`
	ReleaseURL             string   `json:"release_url"`
	LinuxAMD64Asset        string   `json:"linux_amd64_asset"`
	LinuxAMD64SHA256       string   `json:"linux_amd64_sha256"`
	LinuxAMD64BinarySHA256 string   `json:"linux_amd64_binary_sha256"`
	WireAPI                string   `json:"wire_api"`
	RequiresOpenAIAuth     bool     `json:"requires_openai_auth"`
	AppServerExperimental  bool     `json:"app_server_experimental"`
	AppServerTransports    []string `json:"app_server_transports"`
}

func verifyPinnedCodex(path, pinPath string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("pinned Codex executable is unsafe")
	}
	pinInfo, err := os.Lstat(pinPath)
	if err != nil || !pinInfo.Mode().IsRegular() || pinInfo.Mode()&os.ModeSymlink != 0 || pinInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("pinned Codex manifest is unsafe")
	}
	body, err := os.ReadFile(pinPath)
	if err != nil || len(body) > 64<<10 {
		return errors.New("pinned Codex manifest is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var pin codexPin
	if err := decoder.Decode(&pin); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("pinned Codex manifest is invalid")
	}
	if pin.Version != PinnedCodexVersion || pin.Tag != pinnedCodexTag || pin.SourceCommit != pinnedCodexSourceCommit || pin.ReleaseURL != pinnedCodexReleaseURL || pin.LinuxAMD64Asset != pinnedCodexAsset ||
		pin.LinuxAMD64SHA256 != pinnedCodexArchiveSHA256 || pin.LinuxAMD64BinarySHA256 != pinnedCodexBinarySHA256 ||
		pin.WireAPI != "responses" || pin.RequiresOpenAIAuth || !pin.AppServerExperimental ||
		!slices.Equal(pin.AppServerTransports, []string{"stdio", "unix", "websocket"}) {
		return errors.New("pinned Codex manifest does not match the signed harness contract")
	}
	executable, err := os.Open(path)
	if err != nil {
		return errors.New("pinned Codex executable is unavailable")
	}
	defer executable.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, executable); err != nil || hex.EncodeToString(hasher.Sum(nil)) != pin.LinuxAMD64BinarySHA256 {
		return errors.New("pinned Codex executable digest does not match the signed manifest")
	}
	return nil
}

func (l *OpenCodeLauncher) startCodexAdapter(ctx context.Context, lease ModelRuntimeLease, remote OpenCodeRemoteTransport) (string, <-chan error, error) {
	adapter, err := codexadapter.New(codexadapter.Options{RuntimeID: lease.RuntimeID, ModelID: codexModelID, Transport: remote, TTL: time.Duration(lease.TimeoutSeconds) * time.Second})
	if err != nil {
		return "", nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, errors.New("Codex loopback adapter could not bind")
	}
	server := &http.Server{
		Handler: adapter.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: time.Duration(lease.TimeoutSeconds)*time.Second + 30*time.Second,
		IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = ctx.Err()
		}
		done <- err
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	address := listener.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d/v1", address.Port), done, nil
}

func (l *OpenCodeLauncher) codexLinuxWorkcellProcessSpec(runtimeDir string, workspace Workspace, preparation LinuxWorkcellPreparation, adapterURL string, lease ModelRuntimeLease, stdout, stderr io.Writer) (openCodeProcessSpec, error) {
	if workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev || workspace.Path != preparation.Workspace.Path || workspace.ID != lease.WorkspaceID {
		return openCodeProcessSpec{}, errors.New("Codex requires the matching development Linux workcell")
	}
	if !validCodexAdapterURL(adapterURL) {
		return openCodeProcessSpec{}, errors.New("Codex adapter URL is invalid")
	}
	resolvedCodex, err := filepath.EvalSymlinks(l.config.CodexPath)
	if err != nil || !filepath.IsAbs(resolvedCodex) {
		return openCodeProcessSpec{}, errors.New("pinned Codex executable could not be resolved")
	}
	home := filepath.Join(runtimeDir, "home")
	for _, dir := range []string{home, filepath.Join(home, ".config"), filepath.Join(home, ".local", "share"), filepath.Join(home, ".local", "state"), filepath.Join(home, ".cache")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return openCodeProcessSpec{}, errors.New("Codex private runtime directory failed")
		}
	}
	persistentPath := strings.Join([]string{
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin",
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/go/bin",
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/cargo/bin",
		l.config.ToolPath,
	}, ":")
	environment := map[string]string{
		"PATH": persistentPath, "HOME": openCodeSandboxHome, "USER": "mcpedge", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TERM": "dumb", "SHELL": "/bin/sh",
		"CODEX_HOME": openCodeSandboxHome, "XDG_CONFIG_HOME": openCodeSandboxHome + "/.config", "XDG_DATA_HOME": openCodeSandboxHome + "/.local/share",
		"XDG_STATE_HOME": openCodeSandboxHome + "/.local/state", "XDG_CACHE_HOME": openCodeSandboxWorkspace + "/.mcp-devbox/cache",
		"npm_config_cache": openCodeSandboxWorkspace + "/.mcp-devbox/cache/npm", "PIP_CACHE_DIR": openCodeSandboxWorkspace + "/.mcp-devbox/cache/pip",
		"PNPM_HOME": openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin", "PIPX_HOME": openCodeSandboxWorkspace + "/.mcp-devbox/tools/pipx",
		"PIPX_BIN_DIR": openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin", "GOPATH": openCodeSandboxWorkspace + "/.mcp-devbox/tools/go",
		"GOBIN": openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin", "CARGO_HOME": openCodeSandboxWorkspace + "/.mcp-devbox/tools/cargo",
		"RUSTUP_HOME": openCodeSandboxWorkspace + "/.mcp-devbox/tools/rustup", "TMPDIR": openCodeSandboxWorkspace + "/.mcp-devbox/runtime/tmp",
		"DOCKER_CONFIG": openCodeSandboxWorkspace + "/.mcp-devbox/tools/docker", "MCP_DEVBOX_RUNTIME_ID": lease.RuntimeID,
		"MCP_DEVBOX_PROFILE": string(workspace.Profile), "MCP_DEVBOX_MODE": string(workspace.Mode), "MCP_DEVBOX_NETWORK_POSTURE": LinuxWorkcellNetworkPosture,
		"COMPOSE_PROJECT_NAME": strings.ReplaceAll(lease.RuntimeID, "-", "_"),
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--share-net", "--clearenv"}
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	for _, toolDir := range filepath.SplitList(l.config.ToolPath) {
		if pathInside(openCodeManagedToolRoot, toolDir) {
			args = append(args, "--ro-bind", toolDir, toolDir)
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
	if preparation.RootlessContainer != nil {
		args = append(args, "--bind", preparation.RootlessContainer.SocketPath, rootlessContainerSocketTarget)
		uri := "unix://" + rootlessContainerSocketTarget
		environment["DOCKER_HOST"], environment["CONTAINER_HOST"] = uri, uri
		environment["MCP_DEVBOX_CONTAINER_ENGINE"] = preparation.RootlessContainer.Engine
		environment["MCP_DEVBOX_CONTAINER_LABEL"] = rootlessRuntimeLabelKey + "=" + lease.RuntimeID
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", resolvedCodex, codexSandboxExecutable,
		"--bind", runtimeDir, openCodeSandboxRuntime,
		"--bind", workspace.Path, openCodeSandboxWorkspace,
		"--chdir", openCodeSandboxWorkspace,
	)
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, environment[key])
	}
	command := []string{
		codexSandboxExecutable, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check",
		"--sandbox", "danger-full-access", "--cd", openCodeSandboxWorkspace,
		"--config", `model="mcp-devbox-codex"`,
		"--config", `model_provider="mcp-devbox"`,
		"--config", `model_providers.mcp-devbox.name="MCP Devbox GPT Web"`,
		"--config", fmt.Sprintf("model_providers.mcp-devbox.base_url=%q", adapterURL),
		"--config", `model_providers.mcp-devbox.wire_api="responses"`,
		"--config", `model_providers.mcp-devbox.requires_openai_auth=false`,
		"--config", `model_providers.mcp-devbox.supports_websockets=false`,
		"--config", `agents.enabled=false`,
		linuxWorkcellOpenCodePrompt,
	}
	args = append(args, "--")
	args = append(args, command...)
	parsed, err := parseOpenCodeSandboxArgs(args)
	if err != nil {
		return openCodeProcessSpec{}, err
	}
	if err := validateCodexLinuxWorkcellSandbox(parsed, l.config.StateRoot, runtimeDir, workspace, resolvedCodex, l.config.ToolPath, lease, adapterURL); err != nil {
		return openCodeProcessSpec{}, err
	}
	env := []string{"PATH=" + l.config.ToolPath, "HOME=" + home, "USER=mcpedge", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	return openCodeProcessSpec{Executable: l.config.BubblewrapPath, Args: args, Dir: workspace.Path, Env: env, Stdout: stdout, Stderr: stderr, Sandbox: parsed}, nil
}

func validCodexAdapterURL(value string) bool {
	if !strings.HasPrefix(value, "http://127.0.0.1:") || !strings.HasSuffix(value, "/v1") || strings.ContainsAny(value, " \t\r\n@?#") {
		return false
	}
	var port int
	_, err := fmt.Sscanf(value, "http://127.0.0.1:%d/v1", &port)
	return err == nil && port >= 1 && port <= 65535
}

func validateCodexLinuxWorkcellSandbox(spec openCodeSandboxSpec, stateRoot, runtimeDir string, workspace Workspace, codexPath, toolPath string, lease ModelRuntimeLease, adapterURL string) error {
	if !spec.DieWithParent || !spec.NewSession || !spec.UnshareAll || !spec.ShareNetwork || !spec.ClearEnv || spec.WorkingDirectory != openCodeSandboxWorkspace {
		return errors.New("Codex workcell namespace posture is incomplete")
	}
	wantCommandPrefix := []string{codexSandboxExecutable, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "danger-full-access", "--cd", openCodeSandboxWorkspace}
	if len(spec.Command) < len(wantCommandPrefix)+1 || !slices.Equal(spec.Command[:len(wantCommandPrefix)], wantCommandPrefix) || spec.Command[len(spec.Command)-1] != linuxWorkcellOpenCodePrompt {
		return errors.New("Codex workcell command is invalid")
	}
	joined := strings.Join(spec.Command, "\x00")
	for _, required := range []string{`model="mcp-devbox-codex"`, `model_provider="mcp-devbox"`, fmt.Sprintf("model_providers.mcp-devbox.base_url=%q", adapterURL), `model_providers.mcp-devbox.wire_api="responses"`, `model_providers.mcp-devbox.requires_openai_auth=false`, `model_providers.mcp-devbox.supports_websockets=false`, `agents.enabled=false`} {
		if !strings.Contains(joined, required) {
			return errors.New("Codex workcell provider configuration is incomplete")
		}
	}
	for key := range spec.Environment {
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "OPENAI") || strings.Contains(upper, "API_KEY") || strings.Contains(upper, "TOKEN") {
			return errors.New("Codex workcell environment contains credential authority")
		}
	}
	mounts := make(map[string]openCodeSandboxMount)
	for _, mount := range spec.Mounts {
		if _, duplicate := mounts[mount.Target]; duplicate {
			return errors.New("Codex workcell mount target is duplicated")
		}
		mounts[mount.Target] = mount
		for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "/mnt/c", "/mnt/d", "/root"} {
			if mount.Target == forbidden || mount.Source == forbidden || pathInside(forbidden, mount.Target) || pathInside(forbidden, mount.Source) {
				return errors.New("Codex workcell exposes a forbidden host path")
			}
		}
		if mount.Source == stateRoot || (pathInside(stateRoot, mount.Source) && mount.Source != runtimeDir) {
			return errors.New("Codex workcell exposes private Edge state")
		}
	}
	for target, expected := range map[string]openCodeSandboxMount{
		codexSandboxExecutable:   {Source: codexPath, Target: codexSandboxExecutable, Kind: "bind"},
		openCodeSandboxRuntime:   {Source: runtimeDir, Target: openCodeSandboxRuntime, Writable: true, Kind: "bind"},
		openCodeSandboxWorkspace: {Source: workspace.Path, Target: openCodeSandboxWorkspace, Writable: true, Kind: "bind"},
	} {
		if mounts[target] != expected {
			return errors.New("Codex workcell required mount is missing or unsafe")
		}
	}
	if spec.Environment["CODEX_HOME"] != openCodeSandboxHome || spec.Environment["MCP_DEVBOX_RUNTIME_ID"] != lease.RuntimeID || spec.Environment["PATH"] == toolPath {
		return errors.New("Codex workcell environment is incomplete")
	}
	return nil
}

func (l *OpenCodeLauncher) verifyCodexSandbox(ctx context.Context, spec openCodeProcessSpec) error {
	separator := slices.Index(spec.Args, "--")
	if separator < 0 {
		return errors.New("bubblewrap command separator is missing")
	}
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stdout, stderr := newBoundedCapture(4096), newBoundedCapture(4096)
	spec.Args = append(append([]string(nil), spec.Args[:separator+1]...), codexSandboxExecutable, "--version")
	spec.Stdout, spec.Stderr = stdout, stderr
	started := time.Now()
	result := runOpenCodeProcess(versionCtx, spec)
	if result.Err != nil || result.ExitCode != 0 {
		diagnostic := classifyBubblewrapFailure(bubblewrapStageHelperExec, result.Err, stderr.String(), time.Since(started))
		return fmt.Errorf("bubblewrap verification failed (%s)", diagnostic.Code)
	}
	if stdout.Truncated() || strings.TrimSpace(stdout.String()) != "codex-cli "+PinnedCodexVersion {
		return errors.New("Codex version does not match the pinned release")
	}
	return nil
}
