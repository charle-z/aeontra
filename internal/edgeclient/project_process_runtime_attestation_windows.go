//go:build windows

package edgeclient

import (
	"errors"
	"path/filepath"
	"strings"
)

func captureProjectProcessRuntimeAttestation(stateRoot string, roots ProjectRuntimeRoots) (string, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if !IsWindowsLocalPath(stateRoot) {
		return "", errors.New("project process runtime state is unsafe")
	}
	if err := validatePrivateRoot(stateRoot); err != nil {
		return "", errors.New("project process runtime state is unsafe")
	}
	parts := []string{"project-process-runtime-attestation-v1", "state=" + stateRoot}
	for _, entry := range []struct {
		name string
		path string
	}{
		{name: "runtime", path: roots.Runtime},
		{name: "cache", path: roots.Cache},
		{name: "artifacts", path: roots.Artifacts},
	} {
		path := filepath.Clean(strings.TrimSpace(entry.path))
		if !IsWindowsLocalPath(path) || strings.EqualFold(path, stateRoot) || !WindowsPathContained(stateRoot, path) {
			return "", errors.New("project process runtime root is unsafe")
		}
		if err := validatePrivateRoot(path); err != nil {
			return "", errors.New("project process runtime root is unsafe")
		}
		handle, err := OpenWindowsWorkspace(stateRoot, path)
		if err != nil {
			return "", errors.New("project process runtime root is unavailable")
		}
		identity := windowsWorkspaceAttestationIdentity(handle.Identity())
		_ = handle.Close()
		parts = append(parts, entry.name+"="+path, entry.name+"_identity="+identity)
	}
	return hashProjectAttestation(parts...), nil
}

func revalidateProjectProcessRuntimeAttestation(stateRoot string, roots ProjectRuntimeRoots, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return errors.New("project process runtime attestation is missing")
	}
	observed, err := captureProjectProcessRuntimeAttestation(stateRoot, roots)
	if err != nil || observed != expected {
		return ErrProjectProcessIdentityChanged
	}
	return nil
}
