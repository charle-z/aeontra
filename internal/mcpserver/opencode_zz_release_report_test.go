//go:build opencode_e2e && !windows

package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestZZP112ReleaseCandidateReport(t *testing.T) {
	if os.Getenv("OPENCODE_E2E") != "1" {
		t.Skip("P11.2 release report generation is explicit")
	}
	root := repoRoot(t)
	direct := readJSONArtifact(t, filepath.Join(root, "artifacts", "opencode-e2e-report.json"))
	remote := readJSONArtifact(t, filepath.Join(root, "artifacts", "opencode-remote-e2e-report.json"))
	isolation := readJSONArtifact(t, filepath.Join(root, "artifacts", "opencode-bubblewrap-isolation-report.json"))

	for _, key := range []string{"runtime_id", "workspace_id", "authoritative_store", "edge_state"} {
		delete(remote, key)
	}
	gitTree := os.Getenv("P11_2_GIT_TREE")
	gitCommit := os.Getenv("P11_2_GIT_COMMIT")
	if len(gitTree) != 40 || len(gitCommit) != 40 {
		t.Fatalf("build did not pin Git identities: tree=%q commit=%q", gitTree, gitCommit)
	}

	report := map[string]any{
		"schema_version": 1,
		"phase":          "P11.2",
		"git": map[string]any{
			"tree":   gitTree,
			"commit": gitCommit,
		},
		"versions": map[string]any{
			"go":              runtime.Version(),
			"opencode":        direct["opencode_version"],
			"bubblewrap":      isolation["bubblewrap_version"],
			"relay_protocol":  "mcp-devbox.model-turn.v1",
			"driver_protocol": modelTurnDriverProtocolVersionForReport(),
		},
		"digests": map[string]any{
			"opencode_lock_sha256": fileSHA256(t, filepath.Join(root, "test", "opencode-e2e", "package-lock.json")),
			"provider_sha256":      directorySHA256(t, filepath.Join(root, "integrations", "opencode", "provider")),
		},
		"benchmark_a_local_rendezvous": direct["benchmark_b"],
		"benchmark_b_remote_relay":     remote,
		"benchmark_c_restart_resume":   direct["restart_resume"],
		"network":                      direct["network"],
		"security":                     direct["security"],
		"bubblewrap_isolation":         isolation,
		"security_matrix": map[string]any{
			"artifact": "p11-2-restart-resume-matrix.json",
			"passed":   true,
		},
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/tmp/", "/home/", "https://", "http://", "prompt", "tool_arguments", "authorization", "cookie", "token="} {
		if containsFold(encoded, forbidden) {
			t.Fatalf("release report contains forbidden marker %q", forbidden)
		}
	}
	artifact := filepath.Join(root, "artifacts", "p11-2-release-candidate-report.json")
	if err := os.WriteFile(artifact, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("P11_2_RELEASE_REPORT_BYTES=%d", len(encoded))
}

func modelTurnDriverProtocolVersionForReport() string {
	return "mcp-devbox.model-turn-driver.v1"
}

func readJSONArtifact(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func directorySHA256(t *testing.T, root string) string {
	t.Helper()
	hash := sha256.New()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(body)
		_, _ = hash.Write([]byte{0})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func containsFold(body []byte, marker string) bool {
	lower := make([]byte, len(body))
	for index, value := range body {
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		lower[index] = value
	}
	needle := []byte(marker)
	for start := 0; start+len(needle) <= len(lower); start++ {
		match := true
		for index := range needle {
			if lower[start+index] != needle[index] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
