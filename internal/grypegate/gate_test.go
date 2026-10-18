package grypegate

import (
	"errors"
	"strings"
	"testing"
)

func sampleReport() string {
	return `{
  "matches": [
    {
      "vulnerability": {
        "id": "CVE-2026-0001",
        "severity": "High",
        "fix": {"versions": ["1.2.4"], "state": "fixed"}
      },
      "artifact": {
        "name": "libdemo",
        "version": "1.2.3",
        "type": "apk",
        "locations": [{"path": "/lib/libdemo.so"}]
      }
    },
    {
      "vulnerability": {
        "id": "CVE-2026-0002",
        "severity": "Medium",
        "fix": {"versions": [], "state": "not-fixed"}
      },
      "artifact": {
        "name": "other",
        "version": "2.0.0",
        "type": "apk",
        "locations": []
      }
    },
    {
      "vulnerability": {
        "id": "CVE-2026-0003",
        "severity": "Critical",
        "fix": {"versions": ["3.1.0", "3.2.0"], "state": "fixed"}
      },
      "artifact": {
        "name": "critical-lib",
        "version": "3.0.0",
        "type": "go-module",
        "locations": [{"path": "/usr/local/bin/app"}]
      }
    }
  ]
}`
}

func TestEvaluateReturnsOnlyFindingsAtOrAboveMinimum(t *testing.T) {
	findings, err := Evaluate(strings.NewReader(sampleReport()), SeverityHigh)
	if !errors.Is(err, ErrVulnerabilitiesFound) {
		t.Fatalf("error = %v, want ErrVulnerabilitiesFound", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}
	if findings[0].ID != "CVE-2026-0003" || findings[0].Severity != SeverityCritical {
		t.Fatalf("first finding = %#v", findings[0])
	}
	if findings[1].ID != "CVE-2026-0001" || findings[1].Package != "libdemo" {
		t.Fatalf("second finding = %#v", findings[1])
	}
	if findings[1].FixedIn != "1.2.4" || findings[1].Location != "/lib/libdemo.so" {
		t.Fatalf("high finding details = %#v", findings[1])
	}
}

func TestEvaluatePassesWhenNoFindingMeetsMinimum(t *testing.T) {
	report := `{"matches":[{"vulnerability":{"id":"CVE-LOW","severity":"Low","fix":{"versions":[]}},"artifact":{"name":"pkg","version":"1","type":"apk","locations":[]}}]}`
	findings, err := Evaluate(strings.NewReader(report), SeverityHigh)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestEvaluateRejectsMalformedOrUnknownSeverity(t *testing.T) {
	for name, report := range map[string]string{
		"malformed":        `{`,
		"unknown severity": `{"matches":[{"vulnerability":{"id":"CVE-X","severity":"Extreme"},"artifact":{"name":"pkg","version":"1"}}]}`,
		"missing id":       `{"matches":[{"vulnerability":{"severity":"High"},"artifact":{"name":"pkg","version":"1"}}]}`,
		"missing package":  `{"matches":[{"vulnerability":{"id":"CVE-X","severity":"High"},"artifact":{"version":"1"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Evaluate(strings.NewReader(report), SeverityHigh); !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("error = %v, want ErrInvalidReport", err)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	for input, want := range map[string]Severity{
		"negligible": SeverityNegligible,
		"low":        SeverityLow,
		"medium":     SeverityMedium,
		"high":       SeverityHigh,
		"critical":   SeverityCritical,
		"HIGH":       SeverityHigh,
	} {
		got, err := ParseSeverity(input)
		if err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	if _, err := ParseSeverity("unknown"); !errors.Is(err, ErrInvalidSeverity) {
		t.Fatalf("unknown severity error = %v", err)
	}
}

func TestAnnotationIsBoundedAndEscaped(t *testing.T) {
	finding := Finding{
		ID:       "CVE-2026-0001\nforged",
		Severity: SeverityHigh,
		Package:  "lib,demo",
		Version:  "1.2.3",
		Type:     "apk",
		FixedIn:  "1.2.4",
		Location: "/lib/demo.so",
	}
	annotation := finding.GitHubAnnotation("Dockerfile")
	for _, forbidden := range []string{"\nforged", "lib,demo"} {
		if strings.Contains(annotation, forbidden) {
			t.Fatalf("annotation contains unescaped %q: %s", forbidden, annotation)
		}
	}
	for _, required := range []string{"::error file=Dockerfile", "CVE-2026-0001", "severity=High", "fixed=1.2.4"} {
		if !strings.Contains(annotation, required) {
			t.Fatalf("annotation %q does not contain %q", annotation, required)
		}
	}
	if len(annotation) > 1024 {
		t.Fatalf("annotation length = %d", len(annotation))
	}
}
