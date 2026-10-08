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
	"strings"
	"time"
)

// AcceptedRisk is an operator-reviewed exception, not a statement of remediation.
// Its scope remains tied to one image definition and one exact package location.
type AcceptedRisk struct {
	SchemaVersion         int      `json:"schema_version"`
	VulnerabilityIDs      []string `json:"vulnerability_ids"`
	Package               string   `json:"package"`
	Version               string   `json:"version"`
	Type                  string   `json:"type"`
	Location              string   `json:"location"`
	ImageDefinition       string   `json:"image_definition"`
	ImageDefinitionSHA256 string   `json:"image_definition_sha256"`
	ApprovedAt            string   `json:"approved_at"`
	ExpiresAt             string   `json:"expires_at"`
	Owner                 string   `json:"owner"`
	Reason                string   `json:"reason"`
}

var ErrInvalidAcceptedRisk = errors.New("grype gate: invalid or expired accepted risk")

// EvaluateAcceptedRisk preserves the full scanner report and separates accepted
// findings from blocking findings. A changed image, package, severity, location,
// fix availability, or approval window cannot inherit this approval.
func EvaluateAcceptedRisk(reader io.Reader, minimum Severity, policyReader io.Reader, definitionName string, definition []byte, imageID string, now time.Time) (blocking, accepted []Finding, risk AcceptedRisk, err error) {
	policy, readErr := io.ReadAll(io.LimitReader(policyReader, 64*1024+1))
	if readErr != nil || len(policy) > 64*1024 {
		return nil, nil, risk, fmt.Errorf("%w: unreadable or oversized approval", ErrInvalidAcceptedRisk)
	}
	decoder := json.NewDecoder(bytes.NewReader(policy))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&risk); err != nil {
		return nil, nil, risk, fmt.Errorf("%w: %v", ErrInvalidAcceptedRisk, err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return nil, nil, risk, fmt.Errorf("%w: trailing policy data", ErrInvalidAcceptedRisk)
	}
	if err = risk.validate(definitionName, definition, imageID, now); err != nil {
		return nil, nil, risk, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, 32*1024*1024+1))
	if readErr != nil || len(data) > 32*1024*1024 {
		return nil, nil, risk, fmt.Errorf("%w: unreadable or oversized report", ErrInvalidReport)
	}
	findings, evaluationErr := Evaluate(bytes.NewReader(data), minimum)
	if evaluationErr != nil && !errors.Is(evaluationErr, ErrVulnerabilitiesFound) {
		return nil, nil, risk, evaluationErr
	}
	var payload report
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, nil, risk, fmt.Errorf("%w: %v", ErrInvalidReport, err)
	}
	if payload.Matches == nil || payload.Source.Type != "image" || payload.Source.Target.ImageID != imageID {
		return nil, nil, risk, fmt.Errorf("%w: scanner image identity mismatch or missing matches", ErrInvalidAcceptedRisk)
	}
	for _, finding := range findings {
		if risk.accepts(finding) {
			accepted = append(accepted, finding)
		} else {
			blocking = append(blocking, finding)
		}
	}
	if len(blocking) > 0 {
		return blocking, accepted, risk, fmt.Errorf("%w: %d unaccepted finding(s) blocking minimum %s", ErrVulnerabilitiesFound, len(blocking), minimum)
	}
	return blocking, accepted, risk, nil
}

func (r AcceptedRisk) validate(definitionName string, definition []byte, imageID string, now time.Time) error {
	fail := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidAcceptedRisk, reason) }
	approved, err := time.Parse(time.RFC3339, r.ApprovedAt)
	if err != nil {
		return fail("invalid approval time")
	}
	expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
	if err != nil {
		return fail("invalid expiry time")
	}
	if approved.After(now) || !expires.After(now) || !expires.After(approved) || expires.Sub(approved) > 14*24*time.Hour {
		return fail("approval must be current and at most 14 days")
	}
	if r.SchemaVersion != 1 || r.Owner != "charle-z" || strings.TrimSpace(r.Reason) == "" {
		return fail("missing reviewed approval metadata")
	}
	// This implementation permits only the expressly approved exception. New
	// advisories need their own reviewed change; no wildcard or severity override.
	if len(r.VulnerabilityIDs) != 2 || r.VulnerabilityIDs[0] != "CVE-2026-93748" || r.VulnerabilityIDs[1] != "GHSA-ch52-4w7c-c8xp" ||
		r.Package != "http-cache-semantics" || r.Version != "4.2.0" || r.Type != "npm" ||
		r.Location != "/usr/lib/node_modules/npm/node_modules/http-cache-semantics/package.json" ||
		r.ImageDefinition != "Dockerfile.sandbox-workcell" {
		return fail("policy is outside the approved workcell package scope")
	}
	if filepath.Base(definitionName) != r.ImageDefinition || fmt.Sprintf("%x", sha256.Sum256(definition)) != r.ImageDefinitionSHA256 {
		return fail("image definition changed; review is required")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(imageID, "sha256:"))
	if err != nil || !strings.HasPrefix(imageID, "sha256:") || len(decoded) != 32 {
		return fail("a verified OCI image configuration identity is required")
	}
	return nil
}

func (r AcceptedRisk) accepts(f Finding) bool {
	if f.Severity != SeverityHigh || f.Package != r.Package || f.Version != r.Version || f.Type != r.Type || f.FixedIn != "" || f.FixState == "fixed" || f.FixAvailable || len(f.Locations) == 0 {
		return false
	}
	for _, location := range f.Locations {
		if location != r.Location {
			return false
		}
	}
	for _, id := range r.VulnerabilityIDs {
		if f.ID == id {
			return true
		}
	}
	return false
}
