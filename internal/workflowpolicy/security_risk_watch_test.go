package workflowpolicy

import (
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

func TestSecurityRiskWatchInventoryDoesNotClaimProductionCoverage(t *testing.T) {
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
	if inventory.SchemaVersion != 1 || inventory.Coverage != "published-candidate" || inventory.Workcell.DeploymentVerified {
		t.Fatal("initial inventory must explicitly remain a published candidate, not deployed")
	}
	w := inventory.Workcell
	if w.Repository != "ghcr.io/charle-z/aeontra-sandbox-workcell" ||
		w.IndexDigest != "sha256:517a8eb9227dd825710e6380b30502dff4154a2d73b6bbcfe9bcb09d31c2cc7d" ||
		w.ManifestDigest != "sha256:64942257ee4ff8a5f304c2cfd7a7990ba4c362fa43fdf4724debc6c235b4ffcb" ||
		w.ImageID != "sha256:0428593270824ab41d988dff4fdd05cf7161bf4a847f6e6fc800ce7fe7443102" ||
		w.Revision != "c5422e5cf0d9e325862078ce55a54682477f33d4" ||
		w.RecipeSHA256 != "daa52ebb8ad35c0836aac7832c41eecf313f559bf34ad97c07f80f73671a32d4" {
		t.Fatal("initial workcell identities differ from the reviewed published candidate")
	}
	doc, err := os.ReadFile("../../docs/runbooks/security-risk-watch.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"published candidate", "not deployed", "Coolify", "no image rebuild", "24 hours", "7 days", "larger transfer"} {
		if !strings.Contains(string(doc), required) {
			t.Errorf("watch runbook missing limitation %q", required)
		}
	}
}
