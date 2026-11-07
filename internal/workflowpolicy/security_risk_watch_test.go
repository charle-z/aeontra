package workflowpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/grypegate"
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
		w.IndexDigest != "sha256:efa34e7c57550126e6e448dceb7a427a724aaf1ee038a39aaaac47fca2049d70" ||
		w.ManifestDigest != "sha256:3bc51e01b280e0191b4c20dbd5d28077771ddb7fc95a5a513c5dce2b945cb3f3" ||
		w.ImageID != "sha256:f873e7ce76604e02eb42615ebf013184f1e309306672621362bf6f93765c9764" ||
		w.Revision != "4ef3d3360ffd0d6334e7fec7c5426f469d43398a" ||
		w.RecipeSHA256 != "8e7beba632c9c6fbaaa8e697300fa4c6167ac759bf7e6a3559f19bd48de365db" {
		t.Fatal("workcell identities differ from the verified protected image release")
	}
	// The watch checks the recipe in this exact revision with a full checkout.
	// Unit tests and ordinary development must also work from shallow checkouts.
	doc, err := os.ReadFile("../../docs/runbooks/security-risk-watch.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"deployed-workcell", "does not discover production", "Coolify", "no image rebuild", "24 hours", "7 days", "larger transfer", "2026-10-26-workcell-rollout.md"} {
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
	if strings.Contains(string(content), "--accepted-risk") {
		t.Fatal("updated workcell watch must not inherit the historical image exception")
	}
	if !strings.Contains(string(content), "--minimum high") {
		t.Fatal("updated image lost High/Critical threshold")
	}
	// Corrected-runtime evidence is separate from historical risk acceptance and
	// remains bound to the same immutable config that was pulled and scanned.
	for _, required := range []string{".workcell.runtime_proof // empty", "--runtime-policy security/runtime-remediation.json", "--runtime-proof \"$proof_file\"", "--image-id \"$VERIFIED_IMAGE_ID\"", "security/runtime-proofs/[0-9a-f]{64}"} {
		if !strings.Contains(string(content), required) {
			t.Errorf("runtime proof lost identity binding %q", required)
		}
	}
}

func TestSecurityRiskWatchArchivedProofMatchesInventory(t *testing.T) {
	content, err := os.ReadFile("../../security/monitored-images.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Workcell struct {
			ImageID      string `json:"image_id"`
			RecipeSHA    string `json:"recipe_sha256"`
			RuntimeProof string `json:"runtime_proof"`
		} `json:"workcell"`
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		t.Fatal(err)
	}
	w := inventory.Workcell
	if w.RuntimeProof != "security/runtime-proofs/"+strings.TrimPrefix(w.ImageID, "sha256:")+".json" {
		t.Fatal("runtime proof must be archived under the verified image configuration ID")
	}
	body, err := os.ReadFile("../../" + w.RuntimeProof)
	if err != nil {
		t.Fatal(err)
	}
	var proof grypegate.RuntimeProof
	if err := json.Unmarshal(body, &proof); err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile("../../security/runtime-remediation.json")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(policy)
	if proof.ImageID != w.ImageID || proof.DefinitionSHA != w.RecipeSHA || proof.PolicySHA != hex.EncodeToString(hash[:]) {
		t.Fatal("archived proof must bind the verified image, source recipe and unchanged runtime policy")
	}
}
