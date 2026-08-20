package workflowpolicy

import (
	"errors"
	"strings"
	"testing"
)

func validWorkflow() string {
	return `name: CI
on:
  pull_request:
  push:
    branches: [main]
permissions:
  contents: read
jobs:
  verify:
    timeout-minutes: 20
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09
      - run: go test ./... -count=1
`
}

func TestValidateAcceptsLeastPrivilegeWorkflow(t *testing.T) {
	if err := Validate("ci.yml", []byte(validWorkflow())); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptsRepositoryLocalAction(t *testing.T) {
	workflow := strings.Replace(validWorkflow(), "actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "./.github/actions/setup", 1)
	if err := Validate("ci.yml", []byte(workflow)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsDangerousTrigger(t *testing.T) {
	workflow := strings.Replace(validWorkflow(), "  pull_request:\n", "  pull_request_target:\n", 1)
	if err := Validate("ci.yml", []byte(workflow)); !errors.Is(err, ErrForbiddenTrigger) {
		t.Fatalf("error = %v, want ErrForbiddenTrigger", err)
	}
}

func TestValidateRejectsBroadWritePermissions(t *testing.T) {
	for name, workflow := range map[string]string{
		"write-all":      strings.Replace(validWorkflow(), "permissions:\n  contents: read", "permissions: write-all", 1),
		"contents-write": strings.Replace(validWorkflow(), "contents: read", "contents: write", 1),
		"id-token":       strings.Replace(validWorkflow(), "contents: read", "contents: read\n  id-token: write", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate("ci.yml", []byte(workflow)); !errors.Is(err, ErrForbiddenPermission) {
				t.Fatalf("error = %v, want ErrForbiddenPermission", err)
			}
		})
	}
}

func TestValidateAllowsNarrowCodeQLPermission(t *testing.T) {
	workflow := strings.Replace(validWorkflow(), "contents: read", "contents: read\n  security-events: write", 1)
	if err := Validate("codeql.yml", []byte(workflow)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresBoundedJobTimeout(t *testing.T) {
	missing := strings.Replace(validWorkflow(), "    timeout-minutes: 20\n", "", 1)
	if err := Validate("ci.yml", []byte(missing)); !errors.Is(err, ErrMissingTimeout) {
		t.Fatalf("missing timeout error = %v", err)
	}
	unbounded := strings.Replace(validWorkflow(), "timeout-minutes: 20", "timeout-minutes: 240", 1)
	if err := Validate("ci.yml", []byte(unbounded)); !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("unbounded timeout error = %v", err)
	}
}

func TestValidateRejectsSecretsAndProductionActionsInPullRequests(t *testing.T) {
	for name, addition := range map[string]string{
		"secret":       "      - run: echo ${{ secrets.DEPLOY_TOKEN }}\n",
		"production":   "      - run: curl https://mcp-devbox-charlez.duckdns.org/healthz\n",
		"deploy":       "      - run: go run ./cmd/deploy production\n",
		"docker-login": "      - run: docker login registry.example.com\n",
	} {
		t.Run(name, func(t *testing.T) {
			workflow := strings.Replace(validWorkflow(), "      - run: go test ./... -count=1\n", addition, 1)
			if err := Validate("ci.yml", []byte(workflow)); !errors.Is(err, ErrForbiddenPRContent) {
				t.Fatalf("error = %v, want ErrForbiddenPRContent", err)
			}
		})
	}
}

func TestValidateRejectsMutableActionAndToolVersions(t *testing.T) {
	for name, workflow := range map[string]string{
		"action-tag":    strings.Replace(validWorkflow(), "actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/checkout@v5", 1),
		"action-main":   strings.Replace(validWorkflow(), "actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/checkout@main", 1),
		"action-latest": strings.Replace(validWorkflow(), "actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/checkout@latest", 1),
		"action-short":  strings.Replace(validWorkflow(), "actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/checkout@fbc6f399", 1),
		"action-not-hex": strings.Replace(
			validWorkflow(),
			"actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09",
			"actions/checkout@zbc6f3992d24b796d5a048ff273f7fcc4a7b6c09",
			1,
		),
		"go-latest": strings.Replace(validWorkflow(), "go test ./... -count=1", "go run golang.org/x/vuln/cmd/govulncheck@latest ./...", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate("ci.yml", []byte(workflow)); !errors.Is(err, ErrMutableVersion) {
				t.Fatalf("error = %v, want ErrMutableVersion", err)
			}
		})
	}
}

func TestValidateRejectsMalformedWorkflow(t *testing.T) {
	for _, workflow := range []string{"", "jobs: [", "name: no-jobs\n"} {
		if err := Validate("ci.yml", []byte(workflow)); !errors.Is(err, ErrInvalidWorkflow) {
			t.Fatalf("error = %v, want ErrInvalidWorkflow", err)
		}
	}
}
