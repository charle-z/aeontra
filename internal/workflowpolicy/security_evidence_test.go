package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestSecurityEvidenceWorkflowContainsRequiredJobsAndActions(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	for _, required := range []string{
		"secret-scan:",
		"name: Secret scanning",
		"actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09",
		"fetch-depth: 0",
		"GITLEAKS_VERSION: \"8.30.1\"",
		"551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb",
		"gitleaks\" git --config .gitleaks.toml --redact --log-opts=\"--all\" .",
		"gitleaks\" dir --config .gitleaks.toml --redact .",
		"codeql:",
		"dependency-review:",
		"container-evidence:",
		"security-events: write",
		"github/codeql-action/init@99df26d4f13ea111d4ec1a7dddef6063f76b97e9",
		"github/codeql-action/analyze@99df26d4f13ea111d4ec1a7dddef6063f76b97e9",
		"actions/dependency-review-action@a1d282b36b6f3519aa1f3fc636f609c47dddb294",
		"fail-on-severity: moderate",
		"docker build --file Dockerfile --tag mcp-devbox:ci .",
		"docker build --file Dockerfile.front-door --tag mcp-front-door:ci .",
		"docker build --file Dockerfile.front-door-coordinator --tag mcp-front-door-coordinator:ci .",
		"docker build --file Dockerfile.validation-runner --tag mcp-validation-runner:ci .",
		"docker build --file Dockerfile.sandbox-runner --tag mcp-sandbox-runner:ci .",
		"docker build --file Dockerfile.sandbox-workcell --tag mcp-sandbox-workcell:ci .",
		"Verify private coordinator named-volume startup",
		"sh scripts/test-front-door-coordinator-volume.sh",
		"output-file: front-door-sbom.spdx.json",
		"image: mcp-front-door:ci",
		"output-file: front-door-grype.json",
		"test -s front-door-sbom.spdx.json",
		"test -s front-door-grype.json",
		"go run ./cmd/grype-gate --report front-door-grype.json --minimum high --annotation-file Dockerfile.front-door",
		"output-file: front-door-coordinator-sbom.spdx.json",
		"image: mcp-front-door-coordinator:ci",
		"output-file: front-door-coordinator-grype.json",
		"test -s front-door-coordinator-sbom.spdx.json",
		"test -s front-door-coordinator-grype.json",
		"go run ./cmd/grype-gate --report front-door-coordinator-grype.json --minimum high --annotation-file Dockerfile.front-door-coordinator",
		"output-file: validation-runner-sbom.spdx.json",
		"image: mcp-validation-runner:ci",
		"output-file: validation-runner-grype.json",
		"test -s validation-runner-sbom.spdx.json",
		"test -s validation-runner-grype.json",
		"go run ./cmd/grype-gate --report validation-runner-grype.json --minimum high --annotation-file Dockerfile.validation-runner",
		"output-file: sandbox-runner-sbom.spdx.json",
		"image: mcp-sandbox-runner:ci",
		"output-file: sandbox-runner-grype.json",
		"go run ./cmd/grype-gate --report sandbox-runner-grype.json --minimum high --annotation-file Dockerfile.sandbox-runner",
		"output-file: sandbox-workcell-sbom.spdx.json",
		"image: mcp-sandbox-workcell:ci",
		"output-file: sandbox-workcell-grype.json",
		"go run ./cmd/grype-gate --report sandbox-workcell-grype.json --minimum high --annotation-file Dockerfile.sandbox-workcell",
		"anchore/sbom-action@e22c389904149dbc22b58101806040fa8d37a610",
		"output-file: sbom.spdx.json",
		"upload-artifact: false",
		"upload-release-assets: false",
		"anchore/scan-action@e1165082ffb1fe366ebaf02d8526e7c4989ea9d2",
		"image: mcp-devbox:ci",
		"fail-build: false",
		"severity-cutoff: high",
		"output-format: json",
		"output-file: grype.json",
		"test -s sbom.spdx.json",
		"test -s grype.json",
		"go run ./cmd/grype-gate --report grype.json --minimum high --annotation-file Dockerfile",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("security.yml does not contain %q", required)
		}
	}

	for _, forbidden := range []string{
		"secrets.",
		"docker login",
		"push: true",
		"continue-on-error: true",
		"mcp-devbox-charlez.duckdns.org",
		"fail-build: true",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("security.yml contains forbidden %q", forbidden)
		}
	}
	if got := strings.Count(text, "timeout-minutes:"); got != 4 {
		t.Fatalf("timeout count = %d, want 4", got)
	}
}

func TestSandboxRunnerDockerfileCopiesBuildDependencies(t *testing.T) {
	content, err := os.ReadFile("../../Dockerfile.sandbox-runner")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"COPY cmd/mcp-sandbox-runner ./cmd/mcp-sandbox-runner",
		"COPY internal/config ./internal/config",
		"COPY internal/policy ./internal/policy",
		"COPY internal/sandboxexecutor ./internal/sandboxexecutor",
		"COPY internal/sandboxprotocol ./internal/sandboxprotocol",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Dockerfile.sandbox-runner does not contain %q", required)
		}
	}
}

func TestGitleaksPolicyKeepsSyntheticAllowlistNarrow(t *testing.T) {
	content, err := os.ReadFile("../../.gitleaks.toml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"useDefault = true",
		"condition = \"AND\"",
		"targetRules = [\"github-pat\"]",
		"targetRules = [\"generic-api-key\"]",
		"htb_checkpoint_redaction_test\\.go$",
		"ghp_" + "012345678901234567890123456789012345",
		"DB_TOKEN=" + "abcdef0123456789",
	} {
		if !strings.Contains(text, required) {
			t.Errorf(".gitleaks.toml does not contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"condition = \"OR\"",
		"'''^.*$'''",
		"'''^internal/.*'''",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf(".gitleaks.toml contains broad allowlist %q", forbidden)
		}
	}
}
