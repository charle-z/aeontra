package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestCoreCIContainsBlockingVerifyRaceStaticAndVulnerabilityJobs(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	for _, required := range []string{
		"verify:",
		"race:",
		"staticcheck:",
		"govulncheck:",
		"go test ./... -coverprofile=coverage.out -covermode=atomic -count=1",
		"go run ./cmd/coverage-gate --profile coverage.out",
		"go vet ./...",
		"go build ./...",
		"CGO_ENABLED: \"1\"",
		"go test -race ./... -count=1",
		"XDG_CACHE_HOME: ${{ runner.temp }}/staticcheck-cache",
		"honnef.co/go/tools/cmd/staticcheck@v0.7.0",
		"golang.org/x/vuln/cmd/govulncheck@v1.6.0",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("ci.yml does not contain %q", required)
		}
	}

	if strings.Contains(text, "continue-on-error: true") {
		t.Error("core CI jobs must remain blocking")
	}
	if got := strings.Count(text, "timeout-minutes:"); got != 4 {
		t.Fatalf("timeout count = %d, want 4 core jobs", got)
	}
	if got := strings.Count(text, "uses: actions/checkout@v5"); got != 4 {
		t.Fatalf("checkout count = %d, want 4", got)
	}
	if got := strings.Count(text, "uses: actions/setup-go@v6"); got != 4 {
		t.Fatalf("setup-go count = %d, want 4", got)
	}
}
