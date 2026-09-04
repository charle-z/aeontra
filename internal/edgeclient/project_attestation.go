package edgeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ProjectAttestationFingerprint is deliberately based on filesystem identity
// and Git metadata identity, not mutable source contents or status output.
// This lets a registry-only resolution detect a repository swap while treating
// ordinary edits, build outputs and untracked files as dirty-but-authorized.
func ProjectAttestationFingerprint(path string) (string, error) {
	return projectAttestationFingerprint(path)
}

func projectAttestationFingerprint(path string) (string, error) {
	return projectAttestationFingerprintWithRoots(path, nil)
}

// projectAttestationFingerprintWithRoots applies the administrator-owned Git
// metadata roots used by registered workcells. The exported helper remains
// useful for generic callers/tests that only need a filesystem fingerprint;
// authority-bearing registry paths must use this rooted variant.
func projectAttestationFingerprintWithRoots(path string, roots *WorkspaceRoots) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("project attestation path is invalid")
	}
	return projectAttestationFingerprintPlatform(path, roots)
}

func hashProjectAttestation(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// resolveGitCommonDirectory reads Git's optional commondir marker without
// treating malformed metadata as if the marker were absent. The marker is
// registry metadata, not source content: it must be a regular, non-symlink
// file and all other read errors fail closed.
func resolveGitCommonDirectory(gitDir string) (string, bool, error) {
	marker := filepath.Join(gitDir, "commondir")
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return gitDir, false, nil
	}
	if err != nil {
		return "", false, errors.New("git common directory attestation is unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, errors.New("git common directory attestation is invalid")
	}
	content, err := os.ReadFile(marker)
	if err != nil || len(content) == 0 || len(content) > 4<<10 {
		return "", false, errors.New("git common directory attestation is unavailable")
	}
	commonName := strings.TrimSpace(string(content))
	if commonName == "" {
		return "", false, errors.New("git common directory attestation is invalid")
	}
	if !filepath.IsAbs(commonName) {
		commonName = filepath.Join(gitDir, commonName)
	}
	return filepath.Clean(commonName), true, nil
}
