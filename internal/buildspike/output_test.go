//go:build !windows

package buildspike

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeOutputRedactsPathsANSIAndSecretsWithinBound(t *testing.T) {
	contextPath := "/srv/workspaces/private-project"
	artifactPath := "/var/lib/mcp-devbox-builder/results/deadbeef.oci.tar"
	raw := []byte("\x1b[31mERROR\x1b[0m context=" + contextPath + " artifact=" + artifactPath + " token=ghp_abcdefghijklmnopqrstuvwxyz0123456789 NUL\x00tail")
	clean, truncated := SanitizeOutput(raw, []string{contextPath, artifactPath}, 96)
	if len(clean) > 96 || !truncated {
		t.Fatalf("len=%d truncated=%v", len(clean), truncated)
	}
	text := string(clean)
	for _, forbidden := range []string{contextPath, artifactPath, "ghp_", "\x1b", "\x00"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output leaked %q: %q", forbidden, text)
		}
	}
	if !strings.Contains(text, "<redacted>") {
		t.Fatalf("redaction marker missing: %q", text)
	}
}

func TestArtifactIdentityIsDigestBoundAndSizeLimited(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "artifact.oci.tar")
	content := bytes.Repeat([]byte("a"), 1024)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := IdentifyArtifact(path, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Bytes != int64(len(content)) || !strings.HasPrefix(identity.Digest, "sha256:") || len(identity.Digest) != len("sha256:")+64 {
		t.Fatalf("identity=%+v", identity)
	}
	if _, err := IdentifyArtifact(path, 100); err == nil {
		t.Fatal("oversized artifact accepted")
	}
	if err := os.Symlink(path, filepath.Join(root, "link.oci.tar")); err != nil {
		t.Fatal(err)
	}
	if _, err := IdentifyArtifact(filepath.Join(root, "link.oci.tar"), 2048); err == nil {
		t.Fatal("symlinked artifact accepted")
	}
}
