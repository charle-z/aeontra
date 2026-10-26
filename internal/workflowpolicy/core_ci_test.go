package workflowpolicy

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
	if err := Validate("ci.yml", content); err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatal(err)
	}
	root, err := documentMapping(&doc)
	if err != nil {
		t.Fatal(err)
	}
	jobs := mappingValue(root, "jobs")
	for _, name := range []string{"verify", "race", "staticcheck", "govulncheck", "responsive-graph"} {
		job := mappingValue(jobs, name)
		if job == nil {
			t.Fatalf("missing required job %s", name)
		}
		checkout, setupGo := false, false
		for _, step := range mappingValue(job, "steps").Content {
			if uses := mappingValue(step, "uses"); uses != nil {
				checkout = checkout || strings.HasPrefix(uses.Value, "actions/checkout@")
				setupGo = setupGo || strings.HasPrefix(uses.Value, "actions/setup-go@")
			}
		}
		if !checkout || (name != "responsive-graph" && !setupGo) {
			t.Errorf("job %s lacks its required setup", name)
		}
	}
}

func TestCoreCIDoesNotRepeatRedeployTestsBeforeCompleteSuite(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "-run '^TestRedeployE2E'") {
		t.Fatal("redeploy tests already run in the complete coverage suite")
	}
}

func TestContainerEvidencePreparesOneDatabaseForEveryImage(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct{ Steps []struct{ Uses, Run string } }
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	prepared := 0
	var scans string
	for _, step := range workflow.Jobs["container-evidence"].Steps {
		if strings.HasPrefix(step.Uses, "anchore/scan-action/download-grype@") {
			prepared++
		}
		scans += step.Run + "\n"
	}
	if prepared != 1 {
		t.Errorf("database preparation count=%d, want one", prepared)
	}
	for _, image := range []string{"mcp-devbox", "aeontra-site", "mcp-front-door", "mcp-front-door-coordinator", "mcp-validation-runner", "mcp-sandbox-runner", "mcp-sandbox-workcell"} {
		if !strings.Contains(scans, "\"$GRYPE\" docker:"+image+":ci") {
			t.Errorf("missing scan for %s", image)
		}
	}
}
