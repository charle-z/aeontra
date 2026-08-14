//go:build !windows

package edgeclient

import (
	"errors"
	"io"
)

func (l *OpenCodeLauncher) processSpecForWorkspace(runtimeDir string, workspace Workspace, preparation *LinuxWorkcellPreparation, socketPath string, lease ModelRuntimeLease, stdout, stderr io.Writer) (openCodeProcessSpec, error) {
	if l.harness == runtimeHarnessCodex {
		if preparation == nil {
			return openCodeProcessSpec{}, errors.New("Codex requires Linux workcell preparation")
		}
		return l.codexLinuxWorkcellProcessSpec(runtimeDir, workspace, *preparation, socketPath, lease, stdout, stderr)
	}
	switch workspace.Profile {
	case WorkspaceProfileSandbox:
		if preparation != nil {
			return openCodeProcessSpec{}, errors.New("sandbox received Linux workcell preparation")
		}
		return l.processSpec(runtimeDir, workspace.Path, socketPath, lease, stdout, stderr)
	case WorkspaceProfileLinuxWorkcell:
		if preparation == nil {
			return openCodeProcessSpec{}, errors.New("linux workcell preparation is missing")
		}
		return l.linuxWorkcellProcessSpec(runtimeDir, workspace, *preparation, socketPath, lease, stdout, stderr)
	default:
		return openCodeProcessSpec{}, errors.New("workspace profile is invalid")
	}
}

func sameWorkspaceRuntimeContract(left, right Workspace) bool {
	return left.ID == right.ID && left.Path == right.Path && left.Profile == right.Profile && left.Mode == right.Mode &&
		left.MachineName == right.MachineName && left.TargetIP == right.TargetIP && left.Difficulty == right.Difficulty &&
		left.OS == right.OS && left.VPNInterface == right.VPNInterface && left.NetworkPosture == right.NetworkPosture
}
