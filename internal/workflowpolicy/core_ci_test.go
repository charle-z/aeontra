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
		"responsive-graph:",
		"name: Responsive Brain graph",
		"pnpm --dir web/console exec playwright install --with-deps chromium",
		"pnpm console:test:graph",
		"brain-graph-responsive-${{ github.sha }}",
		"go test ./... -coverprofile=coverage.out -covermode=atomic -count=1",
		"go run ./cmd/coverage-gate --profile coverage.out",
		"go vet ./...",
		"go build ./...",
		"CGO_ENABLED: \"1\"",
		"go test -race ./... -count=1",
		"XDG_CACHE_HOME: ${{ runner.temp }}/staticcheck-cache",
		"honnef.co/go/tools/cmd/staticcheck@v0.7.0",
		"golang.org/x/vuln/cmd/govulncheck@v1.6.0",
		"github.com/rhysd/actionlint/cmd/actionlint@v1.7.12",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("ci.yml does not contain %q", required)
		}
	}

	if strings.Contains(text, "continue-on-error: true") {
		t.Error("core CI jobs must remain blocking")
	}
	if strings.Contains(text, "staticcheck:\n    name: Staticcheck\n    runs-on: ubuntu-latest\n    timeout-minutes: 20\n    env:") {
		t.Error("runner.temp is invalid in job-level env; staticcheck cache must be step-scoped")
	}
	if got := strings.Count(text, "timeout-minutes:"); got != 5 {
		t.Fatalf("timeout count = %d, want 5 blocking jobs", got)
	}
	if got := strings.Count(text, "uses: actions/checkout@v5"); got != 5 {
		t.Fatalf("checkout count = %d, want 5", got)
	}
	if got := strings.Count(text, "uses: actions/setup-go@v6"); got != 4 {
		t.Fatalf("setup-go count = %d, want 4", got)
	}
}
