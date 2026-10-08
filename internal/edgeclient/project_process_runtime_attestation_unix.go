//go:build !windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// captureProjectProcessRuntimeAttestation binds a durable worker to the
// private mutable roots selected during preparation. Source contents are not
// included: runtime caches may change while the process runs.
func captureProjectProcessRuntimeAttestation(stateRoot string, roots ProjectRuntimeRoots) (string, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
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
		if path == stateRoot || !pathInside(stateRoot, path) {
			return "", errors.New("project process runtime root is unsafe")
		}
		if err := validatePrivateRoot(path); err != nil {
			return "", errors.New("project process runtime root is unsafe")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
			return "", errors.New("project process runtime root is unavailable")
		}
		parts = append(parts, entry.name+"="+path, entry.name+"_identity="+fileAttestationIdentity(info))
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
