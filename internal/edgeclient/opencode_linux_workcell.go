package edgeclient

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const linuxWorkcellOpenCodePrompt = "Read /workspace/.mcp-devbox/instructions.md and /workspace/.mcp-devbox/current-state.md before acting. Follow the rendered local contract, validate the real workspace state, perform the goal, keep current-state.md updated, and finish with a bounded summary."

func (l *OpenCodeLauncher) linuxWorkcellProcessSpec(runtimeDir string, workspace Workspace, preparation LinuxWorkcellPreparation, socketPath string, lease ModelRuntimeLease, stdout, stderr io.Writer) (openCodeProcessSpec, error) {
	if workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Path != preparation.Workspace.Path || workspace.ID != lease.WorkspaceID {
		return openCodeProcessSpec{}, errors.New("linux workcell preparation does not match the runtime")
	}
	base, err := l.processSpec(runtimeDir, workspace.Path, socketPath, lease, stdout, stderr)
	if err != nil {
		return openCodeProcessSpec{}, err
	}
	args := append([]string(nil), base.Args...)
	unshareIndex := slices.Index(args, "--unshare-all")
	if unshareIndex < 0 {
		return openCodeProcessSpec{}, errors.New("linux workcell namespace baseline is missing")
	}
	args = insertOpenCodeArgs(args, unshareIndex+1, "--share-net")

	mountIndex := slices.Index(args, "--proc")
	if mountIndex < 0 {
		return openCodeProcessSpec{}, errors.New("linux workcell mount baseline is missing")
	}
	for _, target := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/services", "/etc/protocols"} {
		source, ok := safeLinuxWorkcellSystemFile(target)
		if !ok {
			continue
		}
		args = insertOpenCodeArgs(args, mountIndex, "--ro-bind", source, target)
		mountIndex += 3
	}

	for _, target := range []string{"/usr/share/seclists", "/usr/share/wordlists"} {
		source, ok := safeLinuxWorkcellReadonlyDirectory(target, "/usr/share", 0)
		if !ok {
			continue
		}
		args = insertOpenCodeArgs(args, mountIndex, "--ro-bind", source, target)
		mountIndex += 3
	}
	if preparation.RootlessContainer != nil {
		args = insertOpenCodeArgs(args, mountIndex, "--bind", preparation.RootlessContainer.SocketPath, rootlessContainerSocketTarget)
		mountIndex += 3
	}
	persistentPath := strings.Join([]string{
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin",
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/go/bin",
		openCodeSandboxWorkspace + "/.mcp-devbox/tools/cargo/bin",
		l.config.ToolPath,
	}, ":")
	replacements := map[string]string{
		"PATH":                       persistentPath,
		"XDG_CACHE_HOME":             openCodeSandboxWorkspace + "/.mcp-devbox/cache",
		"npm_config_cache":           openCodeSandboxWorkspace + "/.mcp-devbox/cache/npm",
		"PIP_CACHE_DIR":              openCodeSandboxWorkspace + "/.mcp-devbox/cache/pip",
		"PNPM_HOME":                  openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin",
		"PIPX_HOME":                  openCodeSandboxWorkspace + "/.mcp-devbox/tools/pipx",
		"PIPX_BIN_DIR":               openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin",
		"GOPATH":                     openCodeSandboxWorkspace + "/.mcp-devbox/tools/go",
		"GOBIN":                      openCodeSandboxWorkspace + "/.mcp-devbox/tools/bin",
		"CARGO_HOME":                 openCodeSandboxWorkspace + "/.mcp-devbox/tools/cargo",
		"RUSTUP_HOME":                openCodeSandboxWorkspace + "/.mcp-devbox/tools/rustup",
		"TMPDIR":                     openCodeSandboxWorkspace + "/.mcp-devbox/runtime/tmp",
		"DOCKER_CONFIG":              openCodeSandboxWorkspace + "/.mcp-devbox/tools/docker",
		"MCP_DEVBOX_RUNTIME_ID":      lease.RuntimeID,
		"MCP_DEVBOX_PROFILE":         string(workspace.Profile),
		"MCP_DEVBOX_MODE":            string(workspace.Mode),
		"MCP_DEVBOX_NETWORK_POSTURE": LinuxWorkcellNetworkPosture,
		"COMPOSE_PROJECT_NAME":       strings.ReplaceAll(lease.RuntimeID, "-", "_"),
	}
	if preparation.RootlessContainer != nil {
		containerURI := "unix://" + rootlessContainerSocketTarget
		replacements["DOCKER_HOST"] = containerURI
		replacements["CONTAINER_HOST"] = containerURI
		replacements["MCP_DEVBOX_CONTAINER_ENGINE"] = preparation.RootlessContainer.Engine
		replacements["MCP_DEVBOX_CONTAINER_LABEL"] = rootlessRuntimeLabelKey + "=" + lease.RuntimeID
	}
	if workspace.Mode == WorkspaceModeHTBLinux {
		replacements["TARGET"] = workspace.TargetIP
		replacements["LHOST"] = preparation.LHOST
		replacements["MCP_DEVBOX_MACHINE"] = workspace.MachineName
		replacements["MCP_DEVBOX_VPN_INTERFACE"] = workspace.VPNInterface
	}
	for key, value := range replacements {
		var replaced bool
		args, replaced = replaceOpenCodeSetEnv(args, key, value)
		if !replaced {
			args, err = appendOpenCodeSetEnv(args, key, value)
			if err != nil {
				return openCodeProcessSpec{}, err
			}
		}
	}
	separator := slices.Index(args, "--")
	if separator < 0 || len(args) < separator+2 {
		return openCodeProcessSpec{}, errors.New("linux workcell command baseline is missing")
	}
	args[len(args)-1] = linuxWorkcellOpenCodePrompt
	parsed, err := parseOpenCodeSandboxArgs(args)
	if err != nil {
		return openCodeProcessSpec{}, err
	}
	resolvedOpenCode, err := filepath.EvalSymlinks(l.config.OpenCodePath)
	if err != nil {
		return openCodeProcessSpec{}, errors.New("pinned OpenCode executable is unavailable")
	}
	if err := validateLinuxWorkcellSandboxSpec(parsed, l.config.StateRoot, runtimeDir, workspace, l.config.ProviderPath, resolvedOpenCode, l.config.ToolPath, lease, replacements); err != nil {
		return openCodeProcessSpec{}, err
	}
	base.Args = args
	base.Sandbox = parsed
	return base, nil
}

func insertOpenCodeArgs(args []string, index int, values ...string) []string {
	output := make([]string, 0, len(args)+len(values))
	output = append(output, args[:index]...)
	output = append(output, values...)
	output = append(output, args[index:]...)
	return output
}

func replaceOpenCodeSetEnv(args []string, key, value string) ([]string, bool) {
	output := append([]string(nil), args...)
	for index := 0; index+2 < len(output); index++ {
		if output[index] == "--" {
			break
		}
		if output[index] == "--setenv" && output[index+1] == key {
			output[index+2] = value
			return output, true
		}
	}
	return output, false
}

func appendOpenCodeSetEnv(args []string, key, value string) ([]string, error) {
	separator := slices.Index(args, "--")
	if separator < 0 {
		return nil, errors.New("opencode command separator is missing")
	}
	return insertOpenCodeArgs(args, separator, "--setenv", key, value), nil
}

func safeLinuxWorkcellSystemFile(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || (info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular()) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	resolvedInfo, err := os.Stat(resolved)
	if err != nil || !resolvedInfo.Mode().IsRegular() || resolvedInfo.Mode().Perm()&0o022 != 0 {
		return "", false
	}
	return filepath.Clean(resolved), true
}

func validateLinuxWorkcellSandboxSpec(spec openCodeSandboxSpec, stateRoot, runtimeDir string, workspace Workspace, providerPath, openCodePath, toolPath string, lease ModelRuntimeLease, expectedEnv map[string]string) error {
	if !spec.DieWithParent || !spec.NewSession || !spec.UnshareAll || !spec.ShareNetwork || !spec.ClearEnv {
		return errors.New("linux workcell namespace posture is incomplete")
	}
	expectedCommand := []string{openCodeSandboxExecutable, "run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", openCodeSandboxWorkspace, linuxWorkcellOpenCodePrompt}
	if spec.WorkingDirectory != openCodeSandboxWorkspace || !slices.Equal(spec.Command, expectedCommand) {
		return errors.New("linux workcell OpenCode command is invalid")
	}
	for key, value := range expectedEnv {
		if spec.Environment[key] != value {
			return errors.New("linux workcell environment is incomplete")
		}
	}
	if err := validateOpenCodeSandboxConfig(spec.Environment["OPENCODE_CONFIG_CONTENT"], lease); err != nil {
		return err
	}
	mounts := make(map[string]openCodeSandboxMount)
	for _, mount := range spec.Mounts {
		if mount.Target == "" {
			return errors.New("linux workcell mount target is empty")
		}
		if _, duplicate := mounts[mount.Target]; duplicate {
			return errors.New("linux workcell mount target is duplicated")
		}
		mounts[mount.Target] = mount
		for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "/mnt/c", "/mnt/d", "/root"} {
			if mount.Target == forbidden || mount.Source == forbidden || pathInside(forbidden, mount.Target) || pathInside(forbidden, mount.Source) {
				return errors.New("linux workcell exposes a forbidden host path")
			}
		}
	}
	required := map[string]openCodeSandboxMount{
		openCodeSandboxWorkspace:  {Source: workspace.Path, Target: openCodeSandboxWorkspace, Writable: true, Kind: "bind"},
		openCodeSandboxRuntime:    {Source: runtimeDir, Target: openCodeSandboxRuntime, Writable: true, Kind: "bind"},
		openCodeSandboxProvider:   {Source: providerPath, Target: openCodeSandboxProvider, Writable: false, Kind: "bind"},
		openCodeSandboxExecutable: {Source: openCodePath, Target: openCodeSandboxExecutable, Writable: false, Kind: "bind"},
		"/proc":                   {Target: "/proc", Writable: true, Kind: "proc"},
		"/dev":                    {Target: "/dev", Writable: true, Kind: "dev"},
		"/tmp":                    {Target: "/tmp", Writable: true, Kind: "tmpfs"},
	}
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			required[systemPath] = openCodeSandboxMount{Source: systemPath, Target: systemPath, Kind: "bind"}
		}
	}
	for _, toolDir := range filepath.SplitList(toolPath) {
		if pathInside(openCodeManagedToolRoot, toolDir) {
			required[toolDir] = openCodeSandboxMount{Source: toolDir, Target: toolDir, Kind: "bind"}
		}
	}
	for _, target := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/services", "/etc/protocols"} {
		if source, ok := safeLinuxWorkcellSystemFile(target); ok {
			required[target] = openCodeSandboxMount{Source: source, Target: target, Kind: "bind"}
		}
	}
	for _, target := range []string{"/usr/share/seclists", "/usr/share/wordlists"} {
		if source, ok := safeLinuxWorkcellReadonlyDirectory(target, "/usr/share", 0); ok {
			required[target] = openCodeSandboxMount{Source: source, Target: target, Kind: "bind"}
		}
	}
	if expectedEnv["DOCKER_HOST"] != "" || expectedEnv["CONTAINER_HOST"] != "" {
		if expectedEnv["DOCKER_HOST"] != "unix://"+rootlessContainerSocketTarget || expectedEnv["CONTAINER_HOST"] != "unix://"+rootlessContainerSocketTarget {
			return errors.New("linux workcell rootless container environment is invalid")
		}
		mount, ok := mounts[rootlessContainerSocketTarget]
		if !ok || !mount.Writable || mount.Kind != "bind" || !pathInside("/run/user", mount.Source) || mount.Source == "/var/run/docker.sock" || mount.Source == "/run/docker.sock" {
			return errors.New("linux workcell rootless container socket is invalid")
		}
		required[rootlessContainerSocketTarget] = mount
	}
	if len(mounts) != len(required) {
		return errors.New("linux workcell contains an unexpected mount")
	}
	for target, expected := range required {
		if mounts[target] != expected {
			return errors.New("linux workcell required mount is missing or has wrong permissions")
		}
	}
	for _, mount := range spec.Mounts {
		if mount.Source == stateRoot || (pathInside(stateRoot, mount.Source) && mount.Source != runtimeDir) {
			return errors.New("linux workcell exposes private Edge state")
		}
		if mount.Target == rootlessContainerSocketTarget {
			continue
		}
		if mount.Target != openCodeSandboxWorkspace && mount.Target != openCodeSandboxRuntime && mount.Writable && mount.Kind == "bind" {
			return errors.New("linux workcell exposes an unexpected writable bind mount")
		}
	}
	if spec.Environment["PATH"] == toolPath {
		return errors.New("linux workcell persistent tool prefixes are missing")
	}
	return nil
}
