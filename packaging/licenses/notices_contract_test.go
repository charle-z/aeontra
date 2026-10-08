package licenses_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func TestPinnedExternalNoticesMatchReviewedSources(t *testing.T) {
	checks := map[string]string{
		"github-cli-v2.97.0.LICENSE":   "6da4adc42392c8485e40b4251c7e332fc3352df1947c9ffade71dd60b14a7a4f",
		"openai-codex-v0.147.0.NOTICE": "9d71575ecfd9a843fc1677b0efb08053c6ba9fd686a0de1a6f5382fd3c220915",
	}
	for path, want := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		got := fmt.Sprintf("%x", sha256.Sum256(content))
		if got != want {
			t.Errorf("%s hash = %s, want %s", path, got, want)
		}
	}
}

func TestReleaseWorkflowPublishesDeterministicNoticeAssets(t *testing.T) {
	workflow := read(t, "../../.github/workflows/edge-release.yml")
	generator := read(t, "generate-edge-notices.sh")
	for _, required := range []string{
		"github.com/google/go-licenses/v2@v2.0.1",
		"h1:ti+9bi5o7DKbeeg5eBb/uZTgsaPNoJaLCh93cRcXsW8=",
		"generate-edge-notices.sh",
		"third-party-notices.txt",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing %q", required)
		}
	}
	for _, required := range []string{
		"GOOS=\"$platform\"",
		"--ignore github.com/charle-z/mcp-devbox",
		"License: (Unknown|UNKNOWN|NOASSERTION)",
		"GitHub CLI 2.97.0",
		"OpenAI Codex CLI 0.147.0",
	} {
		if !strings.Contains(generator, required) {
			t.Errorf("notice generator missing %q", required)
		}
	}
}
