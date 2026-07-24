//go:build !windows

package buildspike

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type ArtifactIdentity struct {
	Digest string
	Bytes  int64
}

func SanitizeOutput(raw []byte, sensitivePaths []string, maximum int) ([]byte, bool) {
	if maximum < 1 {
		return nil, len(raw) > 0
	}
	text := strings.ReplaceAll(string(raw), "\x00", "")
	text = ansiPattern.ReplaceAllString(text, "")
	for _, item := range sensitivePaths {
		if item != "" {
			text = strings.ReplaceAll(text, item, "<redacted>")
		}
	}
	if redacted, changed := policy.Redact(text); changed {
		text = redacted
	}
	truncated := len(raw) > maximum || len(text) > maximum
	if len(text) > maximum {
		text = text[:maximum]
	}
	return []byte(text), truncated
}

func IdentifyArtifact(path string, maximum int64) (ArtifactIdentity, error) {
	if maximum < 1 {
		return ArtifactIdentity{}, errors.New("buildspike: artifact limit is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maximum {
		return ArtifactIdentity{}, errors.New("buildspike: artifact is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return ArtifactIdentity{}, errors.New("buildspike: artifact unavailable")
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.CopyN(hash, file, maximum+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return ArtifactIdentity{}, errors.New("buildspike: artifact hashing failed")
	}
	if written != info.Size() || written > maximum {
		return ArtifactIdentity{}, errors.New("buildspike: artifact size changed")
	}
	return ArtifactIdentity{Digest: "sha256:" + hex.EncodeToString(hash.Sum(nil)), Bytes: written}, nil
}
