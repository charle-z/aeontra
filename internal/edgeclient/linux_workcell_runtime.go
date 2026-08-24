//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	workcellprofiles "github.com/charle-z/mcp-devbox/profiles"
)

const (
	linuxWorkcellDirName          = ".mcp-devbox"
	linuxWorkcellInstructionsFile = "instructions.md"
	linuxWorkcellStateFile        = "current-state.md"
	linuxWorkcellInventoryFile    = "tool-inventory.json"
	linuxWorkcellStateLimit       = int64(1 << 20)
)

type LinuxWorkcellPreparation struct {
	Workspace         Workspace
	LHOST             string
	InstructionsPath  string
	CurrentStatePath  string
	ToolInventoryPath string
	RootlessContainer *RootlessContainerEndpoint
	ResumeState       string
}

type systemLinuxNetworkProbe struct{}

func PrepareLinuxWorkcell(ctx context.Context, workspace Workspace, lease ModelRuntimeLease, probe LinuxNetworkProbe) (LinuxWorkcellPreparation, error) {
	return PrepareLinuxWorkcellWithToolPath(ctx, workspace, lease, openCodeDefaultToolPath, probe)
}

func PrepareLinuxWorkcellWithToolPath(ctx context.Context, workspace Workspace, lease ModelRuntimeLease, toolPath string, probe LinuxNetworkProbe) (LinuxWorkcellPreparation, error) {
	result := LinuxWorkcellPreparation{Workspace: workspace}
	if workspace.Profile != WorkspaceProfileLinuxWorkcell {
		return result, errors.New("workspace is not a trusted Linux workcell")
	}
	if lease.WorkspaceID != workspace.ID || strings.TrimSpace(lease.Goal) == "" || lease.TimeoutSeconds < 1 || lease.TimeoutSeconds > 3600 {
		return result, errors.New("linux workcell runtime contract is invalid")
	}
	if _, err := ValidateRegisteredWorkspace(workspace.Path); err != nil {
		return result, err
	}
	if workspace.Mode != WorkspaceModeDev && workspace.Mode != WorkspaceModeHTBLinux {
		return result, errors.New("linux workcell mode is invalid")
	}

	if workspace.Mode == WorkspaceModeHTBLinux {
		if probe == nil {
			probe = systemLinuxNetworkProbe{}
		}
		lhost, err := preflightHTBLinux(ctx, workspace, probe)
		if err != nil {
			return result, err
		}
		result.LHOST = lhost
	}

	controlDir := filepath.Join(workspace.Path, linuxWorkcellDirName)
	for _, path := range []string{
		controlDir,
		filepath.Join(controlDir, "tools"),
		filepath.Join(controlDir, "cache"),
		filepath.Join(controlDir, "runtime"),
	} {
		if err := ensurePrivateWorkspaceDir(workspace.Path, path); err != nil {
			return result, err
		}
	}
	if workspace.Mode == WorkspaceModeHTBLinux {
		for _, name := range []string{"scans", "loot", "scripts", "reports", "tmp", "tickets"} {
			if err := ensurePrivateWorkspaceDir(workspace.Path, filepath.Join(workspace.Path, name)); err != nil {
				return result, err
			}
		}
	}

	inventory, err := CollectLinuxToolInventory(ctx, toolPath)
	if err != nil {
		return result, err
	}
	if err := ValidateLinuxToolInventory(inventory); err != nil {
		return result, err
	}
	inventoryBody, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return result, errors.New("linux workcell tool inventory could not be encoded")
	}
	inventoryBody = append(inventoryBody, '\n')
	inventoryPath := filepath.Join(controlDir, linuxWorkcellInventoryFile)
	if err := atomicWorkspaceFile(inventoryPath, inventoryBody, 0o400); err != nil {
		return result, err
	}
	statePath := filepath.Join(controlDir, linuxWorkcellStateFile)
	resume, err := readOrCreateCurrentState(statePath, initialCurrentState(workspace))
	if err != nil {
		return result, err
	}
	instructionsPath := filepath.Join(controlDir, linuxWorkcellInstructionsFile)
	resumeForModel := resume
	if workspace.Mode == WorkspaceModeHTBLinux {
		resumeForModel = sanitizeHTBResumeForModel(resume)
	}
	instructions, err := renderLinuxWorkcellInstructions(workspace, lease, result.LHOST, resumeForModel)
	if err != nil {
		return result, err
	}
	if err := atomicWorkspaceFile(instructionsPath, []byte(instructions), 0o400); err != nil {
		return result, err
	}
	result.InstructionsPath = instructionsPath
	result.CurrentStatePath = statePath
	result.ToolInventoryPath = inventoryPath
	result.ResumeState = resume
	return result, nil
}

func preflightHTBLinux(ctx context.Context, workspace Workspace, probe LinuxNetworkProbe) (string, error) {
	if workspace.Mode != WorkspaceModeHTBLinux || workspace.TargetIP == "" || workspace.VPNInterface == "" {
		return "", errors.New("htb Linux metadata is incomplete")
	}
	ip := net.ParseIP(workspace.TargetIP)
	if ip == nil || ip.To4() == nil || strings.Contains(workspace.TargetIP, "/") {
		return "", errors.New("htb target must be one IPv4 address")
	}
	lhost, err := probe.InterfaceIPv4(ctx, workspace.VPNInterface)
	if err != nil || net.ParseIP(lhost).To4() == nil {
		return "", errors.New("htb VPN interface has no usable IPv4")
	}
	routeInterface, err := probe.RouteInterface(ctx, workspace.TargetIP)
	if err != nil || routeInterface != workspace.VPNInterface {
		return "", errors.New("htb target route does not use the configured VPN interface")
	}
	return net.ParseIP(lhost).To4().String(), nil
}

func (systemLinuxNetworkProbe) InterfaceIPv4(_ context.Context, name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", err
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		value := address.String()
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if ip := net.ParseIP(value); ip != nil && ip.To4() != nil {
			return ip.To4().String(), nil
		}
	}
	return "", errors.New("interface has no IPv4")
}

func (systemLinuxNetworkProbe) RouteInterface(ctx context.Context, target string) (string, error) {
	command := exec.CommandContext(ctx, "ip", "route", "get", target)
	output, err := command.Output()
	if err != nil || len(output) > 16<<10 {
		return "", errors.New("route lookup failed")
	}
	fields := strings.Fields(string(output))
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == "dev" {
			return fields[index+1], nil
		}
	}
	return "", errors.New("route interface was not reported")
}

func ensurePrivateWorkspaceDir(workspace, path string) error {
	workspace = filepath.Clean(workspace)
	path = filepath.Clean(path)
	if !pathInside(workspace, path) || path == workspace {
		return errors.New("linux workcell directory escaped the workspace")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("linux workcell directory is unsafe")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("linux workcell directory is unavailable")
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return errors.New("linux workcell directory could not be created")
	}
	return nil
}

func readOrCreateCurrentState(path, initial string) (string, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > linuxWorkcellStateLimit {
			return "", errors.New("linux workcell current state is unsafe")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", errors.New("linux workcell current state is unavailable")
		}
		return string(content), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("linux workcell current state is unavailable")
	}
	if err := atomicWorkspaceFile(path, []byte(initial), 0o600); err != nil {
		return "", err
	}
	return initial, nil
}

func WriteLinuxWorkcellState(path, content string) error {
	if len(content) == 0 || int64(len(content)) > linuxWorkcellStateLimit {
		return errors.New("linux workcell current state is invalid")
	}
	if filepath.Base(path) != linuxWorkcellStateFile || filepath.Base(filepath.Dir(path)) != linuxWorkcellDirName {
		return errors.New("linux workcell current state path is invalid")
	}
	return atomicWorkspaceFile(path, []byte(content), 0o600)
}

func atomicWorkspaceFile(path string, content []byte, mode os.FileMode) error {
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 || parentInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("linux workcell file parent is unsafe")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("linux workcell file target is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("linux workcell file target is unavailable")
	}
	temporary, err := os.CreateTemp(parent, ".mcp-write-*")
	if err != nil {
		return errors.New("linux workcell file could not be staged")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.New("linux workcell file staging permissions failed")
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return errors.New("linux workcell file staging failed")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("linux workcell file staging failed")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("linux workcell file staging failed")
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return errors.New("linux workcell file permissions failed")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("linux workcell file replacement failed")
	}
	return nil
}

func renderLinuxWorkcellInstructions(workspace Workspace, lease ModelRuntimeLease, lhost, resume string) (string, error) {
	var contract string
	switch workspace.Mode {
	case WorkspaceModeDev:
		contract = devWorkcellContract
	case WorkspaceModeHTBLinux:
		contract = renderHTBTemplate(workspace, lhost)
	default:
		return "", errors.New("linux workcell mode is invalid")
	}
	return fmt.Sprintf(`# TRUSTED LINUX WORKCELL

- Profile: linux-workcell
- Mode: %s
- Network posture: %s
- Runtime ID: %s
- Workspace: %s
- Evidence: %s
- Durable state: %s
- Tool prefix: %s
- Cache prefix: %s
- Runtime prefix: %s

## Goal

%s

## Resume state

Read and validate the real state before continuing. Do not blindly trust this checkpoint.

%s

## Operational contract

%s
`, workspace.Mode, LinuxWorkcellNetworkPosture, lease.RuntimeID, workspace.Path, evidenceLocation(workspace), filepath.Join(workspace.Path, linuxWorkcellDirName, linuxWorkcellStateFile), filepath.Join(workspace.Path, linuxWorkcellDirName, "tools"), filepath.Join(workspace.Path, linuxWorkcellDirName, "cache"), filepath.Join(workspace.Path, linuxWorkcellDirName, "runtime"), lease.Goal, resume, contract), nil
}

func renderHTBTemplate(workspace Workspace, lhost string) string {
	replacer := strings.NewReplacer(
		"{{MACHINE_NAME}}", workspace.MachineName,
		"{{TARGET_IP}}", workspace.TargetIP,
		"{{DIFFICULTY}}", workspace.Difficulty,
		"{{OS}}", workspace.OS,
		"{{WORKSPACE}}", workspace.Path,
		"{{TARGET}}", workspace.TargetIP,
		"{{LHOST}}", lhost,
	)
	return replacer.Replace(workcellprofiles.HTBLinuxV1)
}

func evidenceLocation(workspace Workspace) string {
	if workspace.Mode == WorkspaceModeHTBLinux {
		return filepath.Join(workspace.Path, "scans") + ", " + filepath.Join(workspace.Path, "loot") + ", " + filepath.Join(workspace.Path, "reports")
	}
	return workspace.Path
}

func initialCurrentState(workspace Workspace) string {
	if workspace.Mode == WorkspaceModeHTBLinux {
		return `# Current State — HTB Linux

- Phase: preflight complete
- Current access: none
- Credentials: none
- user.txt: pending
- root.txt: pending
- Confirmed findings: none
- Discarded branches: none
- Active processes: none
- Created artifacts: none
- Cleanup pending: none
- Next action: perform bounded initial recon
`
	}
	return `# Current State — Development

- Objective: pending execution
- Changes made: none
- Tests: not run
- Active processes: none
- Blocker: none
- Next action: inspect the workspace and validate the requested goal
`
}

const devWorkcellContract = `This is the default development mode. Work only inside the selected workspace. You may modify the repository, install user-scoped dependencies, run tests, build rootless containers, start temporary services, and debug. Do not add hacking instructions. Update .mcp-devbox/current-state.md after material progress, before completion, before cancellation, and near timeout. At completion state exactly where the durable checkpoint is stored.`
