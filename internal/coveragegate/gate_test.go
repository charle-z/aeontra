package coveragegate

import (
	"errors"
	"strings"
	"testing"
)

func TestEvaluatePassesPackageThresholds(t *testing.T) {
	profile := `mode: atomic
example.com/project/internal/policy/a.go:1.1,2.1 8 1
example.com/project/internal/policy/b.go:3.1,4.1 2 0
example.com/project/internal/oauth/a.go:1.1,2.1 9 1
example.com/project/internal/oauth/b.go:3.1,4.1 1 0
`
	thresholds := []Threshold{
		{Package: "example.com/project/internal/policy", Minimum: 80},
		{Package: "example.com/project/internal/oauth", Minimum: 85},
	}
	results, err := Evaluate(strings.NewReader(profile), thresholds)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Package != thresholds[0].Package || results[0].Percent != 80 {
		t.Fatalf("first result = %#v", results[0])
	}
	if results[1].Package != thresholds[1].Package || results[1].Percent != 90 {
		t.Fatalf("second result = %#v", results[1])
	}
}

func TestEvaluateFailsBelowThresholdWithActionableDetails(t *testing.T) {
	profile := `mode: atomic
example.com/project/internal/policy/a.go:1.1,2.1 7 1
example.com/project/internal/policy/b.go:3.1,4.1 3 0
`
	results, err := Evaluate(strings.NewReader(profile), []Threshold{{
		Package: "example.com/project/internal/policy",
		Minimum: 80,
	}})
	if !errors.Is(err, ErrCoverageBelowMinimum) {
		t.Fatalf("error = %v, want ErrCoverageBelowMinimum", err)
	}
	if len(results) != 1 || results[0].Percent != 70 || results[0].Passed {
		t.Fatalf("results = %#v", results)
	}
	for _, required := range []string{"internal/policy", "70.0%", "80.0%"} {
		if !strings.Contains(err.Error(), required) {
			t.Fatalf("error %q does not contain %q", err, required)
		}
	}
}

func TestEvaluateFailsWhenThresholdPackageIsMissing(t *testing.T) {
	profile := `mode: atomic
example.com/project/internal/oauth/a.go:1.1,2.1 10 1
`
	results, err := Evaluate(strings.NewReader(profile), []Threshold{{
		Package: "example.com/project/internal/policy",
		Minimum: 80,
	}})
	if !errors.Is(err, ErrCoveragePackageMissing) {
		t.Fatalf("error = %v, want ErrCoveragePackageMissing", err)
	}
	if len(results) != 1 || !results[0].Missing || results[0].Passed {
		t.Fatalf("results = %#v", results)
	}
}

func TestEvaluateRejectsMalformedProfile(t *testing.T) {
	for name, profile := range map[string]string{
		"missing mode":     "example.com/project/internal/policy/a.go:1.1,2.1 1 1\n",
		"bad record":       "mode: atomic\nnot-a-record\n",
		"bad statements":   "mode: atomic\nexample.com/project/a.go:1.1,2.1 nope 1\n",
		"negative count":   "mode: atomic\nexample.com/project/a.go:1.1,2.1 1 -1\n",
		"zero statements":  "mode: atomic\nexample.com/project/a.go:1.1,2.1 0 1\n",
		"missing filename": "mode: atomic\n:1.1,2.1 1 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Evaluate(strings.NewReader(profile), nil); !errors.Is(err, ErrInvalidCoverProfile) {
				t.Fatalf("error = %v, want ErrInvalidCoverProfile", err)
			}
		})
	}
}

func TestDefaultThresholdsCoverSecurityCriticalPackages(t *testing.T) {
	want := map[string]float64{
		"github.com/charle-z/mcp-devbox/internal/policy":            80,
		"github.com/charle-z/mcp-devbox/internal/mcpserver":         80,
		"github.com/charle-z/mcp-devbox/internal/mcpserver/catalog": 80,
		"github.com/charle-z/mcp-devbox/internal/oauth":             80,
		"github.com/charle-z/mcp-devbox/internal/audit":             80,
		"github.com/charle-z/mcp-devbox/internal/observability":     70,
		"github.com/charle-z/mcp-devbox/internal/console":           80,
		"github.com/charle-z/mcp-devbox/internal/tools":             70,
		"github.com/charle-z/mcp-devbox/internal/app":               65,
		"github.com/charle-z/mcp-devbox/internal/grantadmin":        55,
	}
	got := DefaultThresholds()
	if len(got) != len(want) {
		t.Fatalf("thresholds = %d, want %d", len(got), len(want))
	}
	for _, threshold := range got {
		minimum, ok := want[threshold.Package]
		if !ok || minimum != threshold.Minimum {
			t.Fatalf("unexpected threshold %#v", threshold)
		}
		delete(want, threshold.Package)
	}
	if len(want) != 0 {
		t.Fatalf("missing thresholds: %#v", want)
	}
}
