// Package grypegate converts Grype JSON reports into deterministic, actionable
// package findings and GitHub Actions annotations.
package grypegate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var (
	ErrInvalidReport        = errors.New("grype gate: invalid report")
	ErrInvalidSeverity      = errors.New("grype gate: invalid severity")
	ErrVulnerabilitiesFound = errors.New("grype gate: vulnerabilities found")
)

// Severity is ordered from least to most severe.
type Severity int

const (
	SeverityUnknown Severity = iota
	SeverityNegligible
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityNegligible:
		return "Negligible"
	case SeverityLow:
		return "Low"
	case SeverityMedium:
		return "Medium"
	case SeverityHigh:
		return "High"
	case SeverityCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

// ParseSeverity accepts Grype's canonical severity names case-insensitively.
func ParseSeverity(value string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "unknown":
		return SeverityUnknown, nil
	case "negligible":
		return SeverityNegligible, nil
	case "low":
		return SeverityLow, nil
	case "medium":
		return SeverityMedium, nil
	case "high":
		return SeverityHigh, nil
	case "critical":
		return SeverityCritical, nil
	default:
		return SeverityUnknown, fmt.Errorf("%w: %q", ErrInvalidSeverity, value)
	}
}

// Finding is the bounded subset of one Grype match required for remediation.
type Finding struct {
	ID       string
	Severity Severity
	Package  string
	Version  string
	Type     string
	FixedIn  string
	Location string
}

type report struct {
	Matches []match `json:"matches"`
}

type match struct {
	Vulnerability struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Fix      struct {
			Versions []string `json:"versions"`
			State    string   `json:"state"`
		} `json:"fix"`
	} `json:"vulnerability"`
	Artifact struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Type      string `json:"type"`
		Locations []struct {
			Path string `json:"path"`
		} `json:"locations"`
	} `json:"artifact"`
}

// Evaluate reads one Grype JSON document, validates every match, and returns only
// findings at or above minimum. Findings are sorted severity-first for stable output.
func Evaluate(reader io.Reader, minimum Severity) ([]Finding, error) {
	if minimum < SeverityNegligible || minimum > SeverityCritical {
		return nil, fmt.Errorf("%w: minimum %d", ErrInvalidSeverity, minimum)
	}
	decoder := json.NewDecoder(reader)
	var payload report
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidReport, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing JSON data", ErrInvalidReport)
	}

	findings := make([]Finding, 0)
	for index, item := range payload.Matches {
		id := strings.TrimSpace(item.Vulnerability.ID)
		packageName := strings.TrimSpace(item.Artifact.Name)
		if id == "" || packageName == "" {
			return nil, fmt.Errorf("%w: match %d lacks vulnerability id or package", ErrInvalidReport, index)
		}
		severity, err := ParseSeverity(item.Vulnerability.Severity)
		if err != nil {
			return nil, fmt.Errorf("%w: match %d: %v", ErrInvalidReport, index, err)
		}
		// Unknown is a canonical Grype severity. It cannot be proven below the
		// configured threshold, so preserve the finding and fail closed.
		if severity != SeverityUnknown && severity < minimum {
			continue
		}
		location := ""
		if len(item.Artifact.Locations) > 0 {
			location = strings.TrimSpace(item.Artifact.Locations[0].Path)
		}
		findings = append(findings, Finding{
			ID:       id,
			Severity: severity,
			Package:  packageName,
			Version:  strings.TrimSpace(item.Artifact.Version),
			Type:     strings.TrimSpace(item.Artifact.Type),
			FixedIn:  strings.Join(item.Vulnerability.Fix.Versions, ","),
			Location: location,
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		return findings[i].Package < findings[j].Package
	})
	if len(findings) > 0 {
		return findings, fmt.Errorf("%w: %d finding(s) blocking minimum %s", ErrVulnerabilitiesFound, len(findings), minimum)
	}
	return findings, nil
}

// GitHubAnnotation returns one bounded workflow-command error annotation. All
// untrusted report values are escaped before entering the command channel.
func (f Finding) GitHubAnnotation(file string) string {
	file = escapeProperty(limit(file, 120))
	title := escapeProperty(limit(f.ID+" "+f.Severity.String(), 120))
	message := fmt.Sprintf(
		"id=%s severity=%s package=%s version=%s type=%s fixed=%s location=%s",
		f.ID,
		f.Severity,
		f.Package,
		fallback(f.Version, "unknown"),
		fallback(f.Type, "unknown"),
		fallback(f.FixedIn, "not-listed"),
		fallback(f.Location, "not-listed"),
	)
	return fmt.Sprintf("::error file=%s,title=%s::%s", file, title, escapeMessage(limit(message, 800)))
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}
	return value
}

func limit(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "?")
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func escapeProperty(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	)
	return replacer.Replace(value)
}

func escapeMessage(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	)
	return replacer.Replace(value)
}
