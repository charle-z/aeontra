// Package coveragegate evaluates Go coverprofiles against explicit package-specific
// thresholds. It intentionally avoids a global percentage that can hide regressions
// in security-critical packages behind well-covered trivial code.
package coveragegate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

var (
	ErrInvalidCoverProfile    = errors.New("coverage gate: invalid coverprofile")
	ErrCoverageBelowMinimum   = errors.New("coverage gate: package below minimum")
	ErrCoveragePackageMissing = errors.New("coverage gate: threshold package missing")
)

// Threshold declares the minimum statement coverage required for one import path.
type Threshold struct {
	Package string
	Minimum float64
}

// Result reports the measured and required coverage for one threshold package.
type Result struct {
	Package string
	Covered int64
	Total   int64
	Percent float64
	Minimum float64
	Passed  bool
	Missing bool
}

type packageCoverage struct {
	covered int64
	total   int64
}

// DefaultThresholds returns versioned baseline thresholds for packages that enforce
// policy, authentication, audit, protocol, execution, and application composition.
func DefaultThresholds() []Threshold {
	return []Threshold{
		{Package: "github.com/charle-z/mcp-devbox/internal/policy", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/mcpserver", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/oauth", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/audit", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/observability", Minimum: 70},
		{Package: "github.com/charle-z/mcp-devbox/internal/console", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/brain", Minimum: 80},
		{Package: "github.com/charle-z/mcp-devbox/internal/tools", Minimum: 70},
		{Package: "github.com/charle-z/mcp-devbox/internal/app", Minimum: 65},
		{Package: "github.com/charle-z/mcp-devbox/internal/grantadmin", Minimum: 55},
	}
}

// Evaluate parses one Go coverprofile and applies thresholds in their supplied order.
// It always returns per-package results, including missing or failing packages.
func Evaluate(reader io.Reader, thresholds []Threshold) ([]Result, error) {
	coverage, err := parseProfile(reader)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(thresholds))
	failures := make([]error, 0)
	for _, threshold := range thresholds {
		if strings.TrimSpace(threshold.Package) == "" || threshold.Minimum < 0 || threshold.Minimum > 100 {
			return nil, fmt.Errorf("%w: invalid threshold %#v", ErrInvalidCoverProfile, threshold)
		}
		measured, ok := coverage[threshold.Package]
		if !ok || measured.total == 0 {
			results = append(results, Result{
				Package: threshold.Package,
				Minimum: threshold.Minimum,
				Missing: true,
			})
			failures = append(failures, fmt.Errorf("%w: %s", ErrCoveragePackageMissing, threshold.Package))
			continue
		}
		percent := float64(measured.covered) * 100 / float64(measured.total)
		result := Result{
			Package: threshold.Package,
			Covered: measured.covered,
			Total:   measured.total,
			Percent: percent,
			Minimum: threshold.Minimum,
			Passed:  percent+1e-9 >= threshold.Minimum,
		}
		results = append(results, result)
		if !result.Passed {
			failures = append(failures, fmt.Errorf(
				"%w: %s %.1f%% < %.1f%%",
				ErrCoverageBelowMinimum,
				threshold.Package,
				percent,
				threshold.Minimum,
			))
		}
	}
	if len(failures) > 0 {
		return results, errors.Join(failures...)
	}
	return results, nil
}

func parseProfile(reader io.Reader) (map[string]packageCoverage, error) {
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCoverProfile, err)
		}
		return nil, fmt.Errorf("%w: empty profile", ErrInvalidCoverProfile)
	}
	if !strings.HasPrefix(strings.TrimSpace(scanner.Text()), "mode: ") {
		return nil, fmt.Errorf("%w: missing mode header", ErrInvalidCoverProfile)
	}

	coverage := make(map[string]packageCoverage)
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%w: line %d has %d fields", ErrInvalidCoverProfile, lineNumber, len(fields))
		}
		separator := strings.LastIndex(fields[0], ":")
		if separator <= 0 || separator == len(fields[0])-1 {
			return nil, fmt.Errorf("%w: line %d has invalid file range", ErrInvalidCoverProfile, lineNumber)
		}
		filename := strings.TrimSpace(fields[0][:separator])
		packagePath := path.Dir(strings.ReplaceAll(filename, "\\", "/"))
		if filename == "" || packagePath == "." || packagePath == "/" {
			return nil, fmt.Errorf("%w: line %d has invalid filename", ErrInvalidCoverProfile, lineNumber)
		}
		statements, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || statements < 0 {
			return nil, fmt.Errorf("%w: line %d has invalid statement count", ErrInvalidCoverProfile, lineNumber)
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || count < 0 || (statements == 0 && count != 0) {
			return nil, fmt.Errorf("%w: line %d has invalid execution count", ErrInvalidCoverProfile, lineNumber)
		}
		measured := coverage[packagePath]
		measured.total += statements
		if count > 0 {
			measured.covered += statements
		}
		coverage[packagePath] = measured
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCoverProfile, err)
	}
	return coverage, nil
}
