//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func (l *OpenCodeLauncher) verifyLocalInstallationForWorkspace(ctx context.Context, workspace Workspace, preparation *LinuxWorkcellPreparation, lease ModelRuntimeLease) error {
	if err := l.verifyLocalInstallation(ctx, workspace.Path, lease); err != nil {
		return err
	}
	if workspace.Profile == WorkspaceProfileSandbox {
		if preparation != nil {
			return errors.New("sandbox received Linux workcell preparation")
		}
		return nil
	}
	if workspace.Profile != WorkspaceProfileLinuxWorkcell || preparation == nil {
		return errors.New("Linux workcell verification contract is invalid")
	}
	verifyRuntime, err := os.MkdirTemp(l.config.SocketRoot, "verify-linux-workcell-")
	if err != nil {
		return errors.New("Linux workcell verification runtime could not be created")
	}
	defer removePrivateRuntimeDir(verifyRuntime, l.config.SocketRoot)
	if err := os.Chmod(verifyRuntime, 0o700); err != nil {
		return errors.New("Linux workcell verification runtime is unsafe")
	}
	spec, err := l.processSpecForWorkspace(verifyRuntime, workspace, preparation, filepath.Join(verifyRuntime, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		return err
	}
	if l.verifySandbox == nil {
		return errors.New("bubblewrap verification is unavailable")
	}
	return l.verifySandbox(ctx, spec)
}
