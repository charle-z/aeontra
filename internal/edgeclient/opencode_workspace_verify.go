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
	if l.harness == runtimeHarnessCodex {
		if workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev || preparation == nil {
			return errors.New("codex requires a prepared development Linux workcell")
		}
		if l.verifyCodexInstallation == nil {
			return errors.New("codex installation verification is unavailable")
		}
		if err := l.verifyCodexInstallation(l.config.CodexPath, l.config.CodexPinPath); err != nil {
			return err
		}
		verifyRuntime, err := os.MkdirTemp(l.config.SocketRoot, "verify-codex-")
		if err != nil {
			return errors.New("codex verification runtime could not be created")
		}
		defer removePrivateRuntimeDir(verifyRuntime, l.config.SocketRoot)
		if err := os.Chmod(verifyRuntime, 0o700); err != nil {
			return errors.New("codex verification runtime is unsafe")
		}
		spec, err := l.codexLinuxWorkcellProcessSpec(verifyRuntime, workspace, *preparation, "http://127.0.0.1:1/v1", lease, io.Discard, io.Discard)
		if err != nil {
			return err
		}
		if l.verifySandbox == nil {
			return errors.New("bubblewrap verification is unavailable")
		}
		return l.verifySandbox(ctx, spec)
	}
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
		return errors.New("linux workcell verification contract is invalid")
	}
	verifyRuntime, err := os.MkdirTemp(l.config.SocketRoot, "verify-linux-workcell-")
	if err != nil {
		return errors.New("linux workcell verification runtime could not be created")
	}
	defer removePrivateRuntimeDir(verifyRuntime, l.config.SocketRoot)
	if err := os.Chmod(verifyRuntime, 0o700); err != nil {
		return errors.New("linux workcell verification runtime is unsafe")
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
