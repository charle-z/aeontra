package workflowpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSecurityRiskWatchIsDailyBoundedAndIdentityVerified(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/security-risk-watch.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("security-risk-watch.yml", content); err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"schedule:", "cron: '41 11 * * *'", "workflow_dispatch:",
		"github.ref == 'refs/heads/main'", "github.repository == 'charle-z/aeontra'",
		"contents: read", "packages: read", "timeout-minutes: 15",
		"security/monitored-images.json", "git show \"$revision:Dockerfile.sandbox-workcell\"",
		"sha256sum Dockerfile.sandbox-workcell", "docker buildx imagetools inspect",
		".platform.os == \"linux\" and .platform.architecture == \"amd64\"",
		"docker pull --platform linux/amd64", "org.opencontainers.image.revision",
		"test \"$actual_id\" = \"$expected_id\"", "docker tag \"$actual_id\" mcp-sandbox-workcell:ci",
		"GRYPE_DB_REQUIRE_UPDATE_CHECK: \"true\"", "GRYPE_DB_MAX_ALLOWED_BUILT_AGE: 24h",
		"GRYPE_DB_VALIDATE_AGE: \"true\"", "GRYPE_DB_VALIDATE_BY_HASH_ON_START: \"true\"",
		"anchore/scan-action@e1165082ffb1fe366ebaf02d8526e7c4989ea9d2",
		"vex: security/vex/sandbox-workcell-zlib.openvex.json", "cache-db: true",
		"fail-build: false", "severity-cutoff: high", "output-format: json",
		"--accepted-risk security/accepted-risks/http-cache-semantics-20261003.json",
		"--image-id \"$VERIFIED_IMAGE_ID\"", "--annotation-file Dockerfile.sandbox-workcell",
		"db-provenance.json", "if: always()", "retention-days: 7", "if-no-files-found: error",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("security risk watch missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"pull_request:", "pull_request_target:", "push:", "continue-on-error:",
		"docker build ", "docker buildx build", "docker run ", ":latest", "only-fixed: true",
		"packages: write", "contents: write", "secrets.",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("security risk watch contains forbidden %q", forbidden)
		}
	}
	if strings.Count(text, "timeout-minutes:") != 1 {
		t.Error("daily watch must remain one bounded job")
	}
	pull := strings.Index(text, "docker pull --platform linux/amd64")
	verify := strings.Index(text, "test \"$actual_id\" = \"$expected_id\"")
	tag := strings.Index(text, "docker tag \"$actual_id\" mcp-sandbox-workcell:ci")
	scan := strings.Index(text, "anchore/scan-action@")
	if pull < 0 || verify <= pull || tag <= verify || scan <= tag {
		t.Error("pull, verify, retag and scan must occur in that order")
	}
}

func TestSecurityRiskWatchInventoryMatchesVerifiedWorkcell(t *testing.T) {
	content, err := os.ReadFile("../../security/monitored-images.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		SchemaVersion int    `json:"schema_version"`
		Coverage      string `json:"coverage"`
		Workcell      struct {
			Repository         string `json:"repository"`
			IndexDigest        string `json:"index_digest"`
			ManifestDigest     string `json:"manifest_digest"`
			ImageID            string `json:"image_id"`
			Revision           string `json:"revision"`
			RecipeSHA256       string `json:"recipe_sha256"`
			DeploymentVerified bool   `json:"deployment_verified"`
		} `json:"workcell"`
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != 1 || inventory.Coverage != "deployed-workcell" || !inventory.Workcell.DeploymentVerified {
		t.Fatal("inventory must identify the verified workcell, not claim all-production coverage")
	}
	w := inventory.Workcell
	if w.Repository != "ghcr.io/charle-z/aeontra-sandbox-workcell" ||
		w.IndexDigest != "sha256:a342917779194d6c455f25f760b13d7a1a1f4e88eafec0f231bf099aaa776d41" ||
		w.ManifestDigest != "sha256:bb0691ef833195e08f583bc4787e00cbc0d0bd84f9438a1a76c568a757dbb54f" ||
		w.ImageID != "sha256:0b6e0933cfc57d10fcbfdd805e632c638d2eb1071a5e06e86c4f8526e452a1da" ||
		w.Revision != "f8f641e2d65d696f3ef057c1dda8f99ba5a8cf21" ||
		w.RecipeSHA256 != "cc5cb19e289aeb0bf82076da5807b7efcdee56bcb2fd3e1adb7cb74c9fc74ccc" {
		t.Fatal("workcell identities differ from the verified protected image release")
	}
	recipe, err := os.ReadFile("../../Dockerfile.sandbox-workcell")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(recipe)
	if hex.EncodeToString(digest[:]) != w.RecipeSHA256 {
		t.Fatal("monitored image recipe differs from the checked-out approved recipe")
	}
	doc, err := os.ReadFile("../../docs/runbooks/security-risk-watch.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"deployed-workcell", "does not discover production", "Coolify", "no image rebuild", "24 hours", "7 days", "larger transfer", "2026-10-03-workcell-rollout.md"} {
		if !strings.Contains(strings.Join(strings.Fields(string(doc)), " "), required) {
			t.Errorf("watch runbook missing limitation %q", required)
		}
	}
}
