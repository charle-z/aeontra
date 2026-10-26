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
		"security/monitored-images.json", "git show \"$revision:Dockerfile.sandbox-workcell\" > artifacts/monitored/Dockerfile.sandbox-workcell",
		"sha256sum artifacts/monitored/Dockerfile.sandbox-workcell", "docker buildx imagetools inspect",
		"test \"$committed_recipe\" = \"$expected_recipe\"",
		".platform.os == \"linux\" and .platform.architecture == \"amd64\"",
		"docker pull --platform linux/amd64", "org.opencontainers.image.revision",
		"test \"$actual_id\" = \"$expected_id\"", "docker tag \"$actual_id\" mcp-sandbox-workcell:ci",
		"GRYPE_DB_REQUIRE_UPDATE_CHECK: \"true\"", "GRYPE_DB_MAX_ALLOWED_BUILT_AGE: 24h",
		"GRYPE_DB_VALIDATE_AGE: \"true\"", "GRYPE_DB_VALIDATE_BY_HASH_ON_START: \"true\"",
		"anchore/scan-action@e1165082ffb1fe366ebaf02d8526e7c4989ea9d2",
		"vex: security/vex/sandbox-workcell-zlib.openvex.json", "cache-db: true",
		"fail-build: false", "severity-cutoff: high", "output-format: json",
		"test -n \"$VERIFIED_IMAGE_ID\"", "--annotation-file artifacts/monitored/Dockerfile.sandbox-workcell",
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
		"checked_out_recipe=", "--annotation-file Dockerfile.sandbox-workcell",
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
		w.IndexDigest != "sha256:a57995ced48e5d50c48b49934e078a4c865dba356e32a2f59ec26b7b27c15223" ||
		w.ManifestDigest != "sha256:e5368a7ff87330b4b3b42088a0927f9c5036d8e473c68b2ccc51ddf1d008186e" ||
		w.ImageID != "sha256:1106476e2a9fce831f1f5abd6aafd9cf9770b0771ba065934de4aa03a2444c3d" ||
		w.Revision != "e73ccea716e518531737db013b4caea3a4a9d298" ||
		w.RecipeSHA256 != "11ebaa13b523eac95e2471341e4289025571e903b5059446e3d61452b422111f" {
		t.Fatal("workcell identities differ from the verified protected image release")
	}
	// The watch checks the recipe in this exact revision with a full checkout.
	// Unit tests and ordinary development must also work from shallow checkouts.
	doc, err := os.ReadFile("../../docs/runbooks/security-risk-watch.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"deployed-workcell", "does not discover production", "Coolify", "no image rebuild", "24 hours", "7 days", "larger transfer", "2026-10-09-workcell-rollout.md"} {
		if !strings.Contains(strings.Join(strings.Fields(string(doc)), " "), required) {
			t.Errorf("watch runbook missing limitation %q", required)
		}
	}
}

func TestSecurityRiskWatchUsesStrictGateForUpdatedWorkcell(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/security-risk-watch.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "--accepted-risk") || strings.Contains(string(content), "--image-id") {
		t.Fatal("updated workcell watch must not inherit the historical image exception")
	}
	if !strings.Contains(string(content), "--minimum high") {
		t.Fatal("updated image lost High/Critical threshold")
	}
}
