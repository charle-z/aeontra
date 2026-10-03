package grypegate

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func riskFixture(t *testing.T) (AcceptedRisk, []byte, string, string, time.Time) {
	t.Helper()
	definition := []byte("FROM reviewed-workcell\n")
	imageID := "sha256:" + strings.Repeat("a", 64)
	risk := AcceptedRisk{SchemaVersion: 1, VulnerabilityIDs: []string{"CVE-2026-93748", "GHSA-ch52-4w7c-c8xp"}, Package: "http-cache-semantics", Version: "4.2.0", Type: "npm", Location: "/usr/lib/node_modules/npm/node_modules/http-cache-semantics/package.json", ImageDefinition: "Dockerfile.sandbox-workcell", ImageDefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(definition)), ApprovedAt: "2026-10-03T13:24:00Z", ExpiresAt: "2026-10-17T13:24:00Z", Owner: "charle-z", Reason: "Explicit temporary approval; not fixed."}
	report := fmt.Sprintf(`{"source":{"type":"image","target":{"imageID":%q}},"matches":[{"vulnerability":{"id":"GHSA-ch52-4w7c-c8xp","severity":"High","fix":{"versions":[]}},"artifact":{"name":"http-cache-semantics","version":"4.2.0","type":"npm","locations":[{"path":%q}]}}]}`, imageID, risk.Location)
	return risk, definition, imageID, report, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
}

func evaluateRiskFixture(t *testing.T, risk AcceptedRisk, definition []byte, imageID, report string, now time.Time) ([]Finding, []Finding, error) {
	t.Helper()
	policy, err := json.Marshal(risk)
	if err != nil {
		t.Fatal(err)
	}
	blocking, accepted, _, err := EvaluateAcceptedRisk(strings.NewReader(report), SeverityHigh, strings.NewReader(string(policy)), "Dockerfile.sandbox-workcell", definition, imageID, now)
	return blocking, accepted, err
}

func TestAcceptedRiskMatchesOnlyExactApprovedFinding(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"other advisory": func(s string) string { return strings.ReplaceAll(s, "GHSA-ch52-4w7c-c8xp", "GHSA-other") },
		"other package":  func(s string) string { return strings.ReplaceAll(s, `"name":"http-cache-semantics"`, `"name":"other"`) },
		"other version":  func(s string) string { return strings.ReplaceAll(s, `"version":"4.2.0"`, `"version":"4.2.1"`) },
		"other type":     func(s string) string { return strings.ReplaceAll(s, `"type":"npm"`, `"type":"apk"`) },
		"backend path": func(s string) string {
			return strings.ReplaceAll(s, "/usr/lib/node_modules/npm/", "/usr/local/lib/node_modules/npm/")
		},
		"different copy": func(s string) string {
			return strings.ReplaceAll(s, `"locations":[`, `"locations":[{"path":"/workspace/node_modules/http-cache-semantics/package.json"},`)
		},
		"no path": func(s string) string {
			return strings.ReplaceAll(s, `"locations":[{"path":"/usr/lib/node_modules/npm/node_modules/http-cache-semantics/package.json"}]`, `"locations":[]`)
		},
		"critical":                     func(s string) string { return strings.ReplaceAll(s, `"severity":"High"`, `"severity":"Critical"`) },
		"unknown":                      func(s string) string { return strings.ReplaceAll(s, `"severity":"High"`, `"severity":"Unknown"`) },
		"official fix available":       func(s string) string { return strings.ReplaceAll(s, `"versions":[]`, `"versions":["4.2.1"]`) },
		"fixed state without versions": func(s string) string { return strings.ReplaceAll(s, `"versions":[]`, `"versions":[],"state":"fixed"`) },
		"available fix metadata": func(s string) string {
			return strings.ReplaceAll(s, `"versions":[]`, `"versions":[],"available":[{"version":"4.2.1"}]`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			risk, definition, id, report, now := riskFixture(t)
			blocking, accepted, err := evaluateRiskFixture(t, risk, definition, id, change(report), now)
			if !errors.Is(err, ErrVulnerabilitiesFound) || len(blocking) != 1 || len(accepted) != 0 {
				t.Fatalf("blocking=%v accepted=%v err=%v", blocking, accepted, err)
			}
		})
	}
	for _, advisory := range []string{"GHSA-ch52-4w7c-c8xp", "CVE-2026-93748"} {
		t.Run(advisory, func(t *testing.T) {
			risk, definition, id, report, now := riskFixture(t)
			report = strings.ReplaceAll(report, "GHSA-ch52-4w7c-c8xp", advisory)
			blocking, accepted, err := evaluateRiskFixture(t, risk, definition, id, report, now)
			if err != nil || len(blocking) != 0 || len(accepted) != 1 {
				t.Fatalf("blocking=%v accepted=%v err=%v", blocking, accepted, err)
			}
		})
	}
}

func TestAcceptedRiskDoesNotHideAnotherBlockingFinding(t *testing.T) {
	risk, definition, id, report, now := riskFixture(t)
	report = strings.Replace(report, `"matches":[`, `"matches":[{"vulnerability":{"id":"CVE-OTHER","severity":"High"},"artifact":{"name":"other","version":"1","type":"npm","locations":[]}},`, 1)
	blocking, accepted, err := evaluateRiskFixture(t, risk, definition, id, report, now)
	if !errors.Is(err, ErrVulnerabilitiesFound) || len(blocking) != 1 || blocking[0].ID != "CVE-OTHER" || len(accepted) != 1 {
		t.Fatalf("blocking=%v accepted=%v err=%v", blocking, accepted, err)
	}
}

func TestAcceptedRiskRejectsExpiredOrChangedApproval(t *testing.T) {
	for name, mutate := range map[string]func(*AcceptedRisk){
		"expired":            func(r *AcceptedRisk) { r.ExpiresAt = "2026-10-03T14:00:00Z" },
		"over 14 days":       func(r *AcceptedRisk) { r.ExpiresAt = "2026-10-18T13:24:00Z" },
		"future approval":    func(r *AcceptedRisk) { r.ApprovedAt = "2026-10-05T00:00:00Z" },
		"no expiry":          func(r *AcceptedRisk) { r.ExpiresAt = "" },
		"no owner":           func(r *AcceptedRisk) { r.Owner = "" },
		"no reason":          func(r *AcceptedRisk) { r.Reason = "" },
		"other advisory":     func(r *AcceptedRisk) { r.VulnerabilityIDs[0] = "CVE-OTHER" },
		"other image":        func(r *AcceptedRisk) { r.ImageDefinition = "Dockerfile" },
		"changed definition": func(r *AcceptedRisk) { r.ImageDefinitionSHA256 = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			risk, definition, id, report, now := riskFixture(t)
			mutate(&risk)
			_, accepted, err := evaluateRiskFixture(t, risk, definition, id, report, now)
			if !errors.Is(err, ErrInvalidAcceptedRisk) || len(accepted) != 0 {
				t.Fatalf("accepted=%v err=%v", accepted, err)
			}
		})
	}
	risk, definition, id, report, _ := riskFixture(t)
	expires, _ := time.Parse(time.RFC3339, risk.ExpiresAt)
	if _, _, err := evaluateRiskFixture(t, risk, definition, id, report, expires); !errors.Is(err, ErrInvalidAcceptedRisk) {
		t.Fatalf("exact expiry must fail: %v", err)
	}
}

func TestAcceptedRiskRejectsImageMismatchAndMalformedInput(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"image swap":     func(s string) string { return strings.ReplaceAll(s, strings.Repeat("a", 64), strings.Repeat("b", 64)) },
		"no image proof": func(s string) string { return strings.ReplaceAll(s, `"type":"image"`, `"type":"file"`) },
		"no source":      func(s string) string { return `{"matches":[]}` },
		"missing matches": func(s string) string {
			return fmt.Sprintf(`{"source":{"type":"image","target":{"imageID":%q}}}`, "sha256:"+strings.Repeat("a", 64))
		},
		"malformed": func(s string) string { return `{` },
		"trailing":  func(s string) string { return s + `{}` },
	} {
		t.Run(name, func(t *testing.T) {
			risk, definition, id, report, now := riskFixture(t)
			_, accepted, err := evaluateRiskFixture(t, risk, definition, id, change(report), now)
			if err == nil || len(accepted) != 0 {
				t.Fatalf("accepted=%v err=%v", accepted, err)
			}
		})
	}
}
