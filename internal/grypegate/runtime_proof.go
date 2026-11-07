package grypegate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// RuntimePolicy is a reviewed, expiring remediation identity, not risk acceptance.
type RuntimePolicy struct {
	SchemaVersion int       `json:"schema_version"`
	Owner         string    `json:"owner"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	GlibcSource   string    `json:"glibc_source"`
	SourceSHA     string    `json:"glibc_source_sha256"`
	GlibcPatch    string    `json:"glibc_patch"`
	PatchSHA      string    `json:"glibc_patch_sha256"`
	Recipe        string    `json:"wolfi_recipe"`
	PatchRecipe   string    `json:"wolfi_patch_recipe"`
	UpstreamPR    string    `json:"upstream_glibc_pr"`
	UpstreamIssue string    `json:"upstream_sqlite_issue"`
}

// RuntimeProof is produced by the isolated final-image verifier. Every hash and
// result must be collected anew from the scanned OCI configuration, not a rebuild.
type RuntimeProof struct {
	SchemaVersion int       `json:"schema_version"`
	ImageID       string    `json:"image_id"`
	DefinitionSHA string    `json:"definition_sha256"`
	PolicySHA     string    `json:"policy_sha256"`
	ObservedAt    time.Time `json:"observed_at"`
	Glibc         struct {
		Source        string `json:"source"`
		PatchSHA      string `json:"patch_sha256"`
		ManifestSHA   string `json:"manifest_sha256"`
		FilesVerified int    `json:"files_verified"`
		ABI           bool   `json:"abi_checked"`
		Regression    bool   `json:"regression_checked"`
		Normal        bool   `json:"normal_checked"`
	} `json:"glibc"`
	SQLite struct {
		Version        string `json:"version"`
		InventorySHA   string `json:"inventory_sha256"`
		FilesScanned   int    `json:"files_scanned"`
		CLIAbsent      bool   `json:"cli_absent"`
		RenamedControl bool   `json:"renamed_cli_control"`
		CanarySHA      string `json:"canary_sha256"`
	} `json:"sqlite"`
}

func decodeProofJSON(body []byte, value any) error {
	if len(body) == 0 || len(body) > 1<<20 {
		return fmt.Errorf("invalid proof size")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing proof data")
	}
	return nil
}

func digestBytes(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }
func validSHA(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == value
}

// EvaluateRuntimeProof preserves the original findings and returns only two
// reviewed dispositions. Unrelated, Unknown, Critical or newly fixable findings
// remain blocking. It cannot be combined with historical risk acceptance.
func EvaluateRuntimeProof(reportBody, policyBody, proofBody, definition []byte, definitionName, imageID string, now time.Time) (blocking, qualified []Finding, err error) {
	all, err := Evaluate(bytes.NewReader(reportBody), SeverityHigh)
	if err != nil && !errors.Is(err, ErrVulnerabilitiesFound) {
		return nil, nil, err
	}
	var policy RuntimePolicy
	var proof RuntimeProof
	var payload report
	if err = decodeProofJSON(policyBody, &policy); err != nil {
		return nil, nil, fmt.Errorf("runtime policy: %w", err)
	}
	if err = decodeProofJSON(proofBody, &proof); err != nil {
		return nil, nil, fmt.Errorf("runtime proof: %w", err)
	}
	if err = json.Unmarshal(reportBody, &payload); err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{"Dockerfile": true, "Dockerfile.site": true, "Dockerfile.front-door": true, "Dockerfile.front-door-coordinator": true, "Dockerfile.validation-runner": true, "Dockerfile.sandbox-workcell": true}
	if !allowed[filepath.Base(definitionName)] || policy.SchemaVersion != 1 || policy.Owner != "charle-z" || policy.CreatedAt.After(now) || !now.Before(policy.ExpiresAt) || policy.ExpiresAt.Sub(policy.CreatedAt) > 14*24*time.Hour || !policy.ExpiresAt.After(policy.CreatedAt) {
		return nil, nil, fmt.Errorf("runtime remediation expired or outside reviewed scope")
	}
	if policy.GlibcSource != "34106fee698c83eb0af71d4d0381e66b399be746" || policy.GlibcPatch != "03067f3c5e39aec5ec250a44bc1b395a1afd45e8" || policy.PatchSHA != "a9d503207f2cabfe2e0b336728202824d466170759950bcc51a8cba152e28e92" {
		return nil, nil, fmt.Errorf("unreviewed libc remediation")
	}
	if len(imageID) != 71 || imageID[:7] != "sha256:" || !validSHA(imageID[7:]) || payload.Source.Type != "image" || payload.Source.Target.ImageID != imageID || proof.SchemaVersion != 1 || proof.ImageID != imageID || proof.DefinitionSHA != digestBytes(definition) || proof.PolicySHA != digestBytes(policyBody) || proof.ObservedAt.Before(policy.CreatedAt) || proof.ObservedAt.After(now.Add(5*time.Minute)) || proof.ObservedAt.IsZero() {
		return nil, nil, fmt.Errorf("runtime proof is stale or does not describe the scanned image and recipe")
	}
	if proof.Glibc.Source != policy.GlibcSource || proof.Glibc.PatchSHA != policy.PatchSHA || !validSHA(proof.Glibc.ManifestSHA) || proof.Glibc.FilesVerified < 8 || !proof.Glibc.ABI || !proof.Glibc.Regression || !proof.Glibc.Normal || proof.SQLite.Version != "3.54.1-r0" || !validSHA(proof.SQLite.InventorySHA) || proof.SQLite.CanarySHA != "c42cdc1da4e6c59fbb90c3281c2a10a51a6fb4ce12105a7db6e0a5e68da1b2a9" || proof.SQLite.FilesScanned < 8 || !proof.SQLite.CLIAbsent || !proof.SQLite.RenamedControl {
		return nil, nil, fmt.Errorf("runtime proof is incomplete")
	}
	for _, finding := range all {
		matches := finding.Severity == SeverityHigh && finding.Type == "apk" && !finding.FixAvailable && finding.FixedIn == "" && finding.FixState != "fixed" && len(finding.Locations) == 1 && finding.Locations[0] == "/usr/lib/apk/db/installed"
		matches = matches && ((finding.ID == "CVE-2026-84243" && finding.Package == "glibc-2.44" && finding.Version == "2.44-r8") || (finding.ID == "CVE-2026-119586" && finding.Package == "sqlite-libs" && finding.Version == "3.54.1-r0"))
		if matches {
			qualified = append(qualified, finding)
		} else {
			blocking = append(blocking, finding)
		}
	}
	if len(blocking) != 0 {
		return blocking, qualified, fmt.Errorf("%w: %d unqualified finding(s)", ErrVulnerabilitiesFound, len(blocking))
	}
	return blocking, qualified, nil
}
