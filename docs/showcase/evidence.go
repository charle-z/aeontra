// Package showcase owns the canonical, public, build-embedded evidence used by
// the MCP Devbox presentation. The JSON remains the source of truth; this package
// only validates and exposes those exact bytes.
package showcase

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion = 1

	pixelgramaRepository = "https://github.com/charle-z/pixelgrama"
	productionURL        = "https://pixelgrama.mcp-devbox-charlez.duckdns.org"
	primaryPublicRoute   = productionURL + "/wall"
	versionURL           = productionURL + "/version"
)

//go:embed pixelgrama-evidence.json
var pixelgramaEvidence []byte

var (
	shaPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	privateIDPattern = regexp.MustCompile(`\b(?:ed|ws|mr)_[a-f0-9]{32}\b`)
	forbiddenContent = []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"bearer credential", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`)},
		{"GitHub credential", regexp.MustCompile(`(?i)\b(?:ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)},
		{"provider credential", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`)},
		{"sensitive JSON field", regexp.MustCompile(`(?i)"(?:token|password|authorization|cookie|client_secret|api_key)"\s*:`)},
		{"private identifier", privateIDPattern},
		{"private filesystem path", regexp.MustCompile(`(?i)/(?:repos|state)/`)},
	}
)

// Manifest is the closed schema for one public Pixelgrama evidence document.
type Manifest struct {
	SchemaVersion         int                   `json:"schema_version"`
	Project               Project               `json:"project"`
	HistoricalExecution   HistoricalExecution   `json:"historical_execution"`
	ProductionObservation ProductionObservation `json:"production_observation"`
	AuthorityPosture      AuthorityPosture      `json:"authority_posture"`
	Operations            Operations            `json:"operations"`
	Perimeter             Perimeter             `json:"perimeter"`
}

type Project struct {
	Name               string          `json:"name"`
	Repository         string          `json:"repository"`
	BaseBranch         string          `json:"base_branch"`
	ProductionURL      string          `json:"production_url"`
	PrimaryPublicRoute string          `json:"primary_public_route"`
	VersionURL         string          `json:"version_url"`
	RequestSummary     string          `json:"request_summary"`
	PublicSessions     []PublicSession `json:"public_sessions"`
}

type PublicSession struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type HistoricalExecution struct {
	PullRequests  []PullRequest  `json:"pull_requests"`
	SourceCommits []SourceCommit `json:"source_commits"`
}

type PullRequest struct {
	Number  int     `json:"number"`
	URL     string  `json:"url"`
	HeadSHA string  `json:"head_sha"`
	Purpose string  `json:"purpose"`
	Checks  []Check `json:"checks"`
}

type Check struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

type SourceCommit struct {
	SHA  string `json:"sha"`
	Role string `json:"role"`
	URL  string `json:"url"`
}

type ProductionObservation struct {
	ObservedCommit    string          `json:"observed_commit"`
	SourceMainCommit  string          `json:"source_main_commit"`
	MatchesSourceMain bool            `json:"matches_source_main"`
	VerifiedOn        string          `json:"verified_on"`
	Routes            []ObservedRoute `json:"routes"`
	Infrastructure    Infrastructure  `json:"infrastructure"`
}

type ObservedRoute struct {
	Path           string `json:"path"`
	ObservedStatus int    `json:"observed_status"`
	FinalURL       string `json:"final_url"`
	Redirected     bool   `json:"redirected"`
	Purpose        string `json:"purpose"`
}

type Infrastructure struct {
	Provider string `json:"provider"`
	Platform string `json:"platform"`
}

type AuthorityPosture struct {
	Status    string `json:"status"`
	Statement string `json:"statement"`
}

type Operations struct {
	Direct        []OperationEvidence `json:"direct"`
	PlanProtected []OperationEvidence `json:"plan_protected"`
}

type OperationEvidence struct {
	Name         string `json:"name"`
	Verification string `json:"verification"`
	EvidenceURL  string `json:"evidence_url"`
}

type Perimeter struct {
	Statement string   `json:"statement"`
	Includes  []string `json:"includes"`
	Excludes  []string `json:"excludes"`
}

// PixelgramaEvidence returns a defensive copy of the exact embedded JSON after
// validating it. An invalid or missing resource therefore fails startup and CI.
func PixelgramaEvidence() ([]byte, error) {
	if _, err := ParsePixelgramaEvidence(pixelgramaEvidence); err != nil {
		return nil, fmt.Errorf("validate embedded Pixelgrama evidence: %w", err)
	}
	return append([]byte(nil), pixelgramaEvidence...), nil
}

// ParsePixelgramaEvidence decodes the closed schema and validates its public claims.
func ParsePixelgramaEvidence(data []byte) (Manifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Manifest{}, fmt.Errorf("evidence document is empty")
	}
	for _, forbidden := range forbiddenContent {
		if forbidden.pattern.Match(data) {
			return Manifest{}, fmt.Errorf("evidence document contains %s", forbidden.name)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode evidence document: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate enforces the recognized version, exact project identity, URL/SHA
// formats, infrastructure, and the separation of history from production truth.
func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version=%d is not recognized", manifest.SchemaVersion)
	}
	if err := validateProject(manifest.Project); err != nil {
		return err
	}
	if err := validateHistorical(manifest.HistoricalExecution); err != nil {
		return err
	}
	if err := validateProduction(manifest.ProductionObservation, manifest.HistoricalExecution); err != nil {
		return err
	}
	if manifest.AuthorityPosture.Status != "not_publicly_verified" || strings.TrimSpace(manifest.AuthorityPosture.Statement) == "" {
		return fmt.Errorf("authority_posture must explicitly remain not_publicly_verified")
	}
	if err := validateOperations(manifest.Operations); err != nil {
		return err
	}
	if strings.TrimSpace(manifest.Perimeter.Statement) == "" || len(manifest.Perimeter.Includes) == 0 || len(manifest.Perimeter.Excludes) == 0 {
		return fmt.Errorf("perimeter must include a statement, inclusions, and exclusions")
	}
	for _, value := range append(append([]string{}, manifest.Perimeter.Includes...), manifest.Perimeter.Excludes...) {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("perimeter entries must not be empty")
		}
	}
	return nil
}

func validateProject(project Project) error {
	if project.Name != "Pixelgrama" || project.Repository != pixelgramaRepository || project.BaseBranch != "main" {
		return fmt.Errorf("project identity must be Pixelgrama at charle-z/pixelgrama on main")
	}
	for name, pair := range map[string][2]string{
		"production_url":       {project.ProductionURL, productionURL},
		"primary_public_route": {project.PrimaryPublicRoute, primaryPublicRoute},
		"version_url":          {project.VersionURL, versionURL},
	} {
		if pair[0] != pair[1] {
			return fmt.Errorf("%s=%q does not match the canonical URL", name, pair[0])
		}
		if _, err := validateHTTPSURL(pair[0], false); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if summary := strings.TrimSpace(project.RequestSummary); summary == "" || len(summary) > 1000 {
		return fmt.Errorf("request_summary must contain at most 1000 bytes")
	}
	for index, session := range project.PublicSessions {
		if strings.TrimSpace(session.Label) == "" {
			return fmt.Errorf("public_sessions[%d].label is required", index)
		}
		if _, err := validateHTTPSURL(session.URL, true); err != nil {
			return fmt.Errorf("public_sessions[%d].url: %w", index, err)
		}
	}
	return nil
}

func validateHistorical(history HistoricalExecution) error {
	if len(history.PullRequests) == 0 || len(history.SourceCommits) == 0 {
		return fmt.Errorf("historical_execution requires pull requests and source commits")
	}
	prNumbers := make(map[int]struct{}, len(history.PullRequests))
	for index, pullRequest := range history.PullRequests {
		if pullRequest.Number < 1 {
			return fmt.Errorf("pull_requests[%d].number must be positive", index)
		}
		if _, exists := prNumbers[pullRequest.Number]; exists {
			return fmt.Errorf("pull request %d is duplicated", pullRequest.Number)
		}
		prNumbers[pullRequest.Number] = struct{}{}
		parsed, err := validateHTTPSURL(pullRequest.URL, false)
		if err != nil || parsed.Hostname() != "github.com" || parsed.Path != fmt.Sprintf("/charle-z/pixelgrama/pull/%d", pullRequest.Number) {
			return fmt.Errorf("pull request %d has an invalid URL", pullRequest.Number)
		}
		if !shaPattern.MatchString(pullRequest.HeadSHA) || strings.TrimSpace(pullRequest.Purpose) == "" || len(pullRequest.Checks) == 0 {
			return fmt.Errorf("pull request %d requires a valid head SHA, purpose, and checks", pullRequest.Number)
		}
		for checkIndex, check := range pullRequest.Checks {
			checkURL, checkErr := validateHTTPSURL(check.URL, false)
			if strings.TrimSpace(check.Name) == "" || check.Conclusion != "success" || checkErr != nil || checkURL.Hostname() != "github.com" || !strings.HasPrefix(checkURL.Path, "/charle-z/pixelgrama/actions/runs/") {
				return fmt.Errorf("pull request %d check %d is not successful public GitHub evidence", pullRequest.Number, checkIndex)
			}
		}
	}

	commits := make(map[string]struct{}, len(history.SourceCommits))
	roles := make(map[string]string, len(history.SourceCommits))
	for index, commit := range history.SourceCommits {
		if !shaPattern.MatchString(commit.SHA) || strings.TrimSpace(commit.Role) == "" {
			return fmt.Errorf("source_commits[%d] requires a valid SHA and role", index)
		}
		if _, exists := commits[commit.SHA]; exists {
			return fmt.Errorf("source commit %s is duplicated", commit.SHA)
		}
		if _, exists := roles[commit.Role]; exists {
			return fmt.Errorf("source commit role %q is duplicated", commit.Role)
		}
		commits[commit.SHA] = struct{}{}
		roles[commit.Role] = commit.SHA
		parsed, err := validateHTTPSURL(commit.URL, false)
		if err != nil || parsed.Hostname() != "github.com" || parsed.Path != "/charle-z/pixelgrama/commit/"+commit.SHA {
			return fmt.Errorf("source commit %s has an invalid URL", commit.SHA)
		}
	}
	if roles["current_source_main"] == "" {
		return fmt.Errorf("historical_execution must identify current_source_main")
	}
	return nil
}

func validateProduction(production ProductionObservation, history HistoricalExecution) error {
	if !shaPattern.MatchString(production.ObservedCommit) || !shaPattern.MatchString(production.SourceMainCommit) {
		return fmt.Errorf("production commits must be lowercase 40-character SHAs")
	}
	if !production.MatchesSourceMain || production.ObservedCommit != production.SourceMainCommit {
		return fmt.Errorf("production observation must explicitly match source main")
	}
	if _, err := time.Parse("2006-01-02", production.VerifiedOn); err != nil {
		return fmt.Errorf("verified_on must use YYYY-MM-DD")
	}
	currentMain := ""
	for _, commit := range history.SourceCommits {
		if commit.Role == "current_source_main" {
			currentMain = commit.SHA
			break
		}
	}
	if currentMain != production.SourceMainCommit {
		return fmt.Errorf("historical current_source_main and production source_main_commit differ")
	}
	if production.Infrastructure.Provider != "CubePath" || production.Infrastructure.Platform != "Coolify" {
		return fmt.Errorf("production infrastructure must be CubePath and Coolify")
	}

	expectedRoutes := map[string]struct {
		finalURL   string
		redirected bool
	}{
		"/":        {primaryPublicRoute, true},
		"/wall":    {primaryPublicRoute, false},
		"/version": {versionURL, false},
	}
	if len(production.Routes) != len(expectedRoutes) {
		return fmt.Errorf("production routes must contain exactly /, /wall, and /version")
	}
	seen := make(map[string]struct{}, len(production.Routes))
	for _, route := range production.Routes {
		expected, exists := expectedRoutes[route.Path]
		if !exists {
			return fmt.Errorf("production route %q is not recognized", route.Path)
		}
		if _, duplicate := seen[route.Path]; duplicate {
			return fmt.Errorf("production route %q is duplicated", route.Path)
		}
		seen[route.Path] = struct{}{}
		if route.ObservedStatus != 200 || route.FinalURL != expected.finalURL || route.Redirected != expected.redirected || strings.TrimSpace(route.Purpose) == "" {
			return fmt.Errorf("production route %q does not match the verified result", route.Path)
		}
		if _, err := validateHTTPSURL(route.FinalURL, false); err != nil {
			return fmt.Errorf("production route %q final_url: %w", route.Path, err)
		}
	}
	return nil
}

func validateOperations(operations Operations) error {
	if len(operations.Direct) == 0 || len(operations.PlanProtected) == 0 {
		return fmt.Errorf("operations require direct and plan_protected evidence")
	}
	if err := validateOperationGroup(operations.Direct, "public_process_documented"); err != nil {
		return fmt.Errorf("direct operations: %w", err)
	}
	if err := validateOperationGroup(operations.PlanProtected, "result_public_plan_artifact_private"); err != nil {
		return fmt.Errorf("plan_protected operations: %w", err)
	}
	return nil
}

func validateOperationGroup(operations []OperationEvidence, expectedVerification string) error {
	for index, operation := range operations {
		if strings.TrimSpace(operation.Name) == "" || operation.Verification != expectedVerification {
			return fmt.Errorf("operation %d has an invalid name or verification posture", index)
		}
		if _, err := validateHTTPSURL(operation.EvidenceURL, true); err != nil {
			return fmt.Errorf("operation %d evidence_url: %w", index, err)
		}
	}
	return nil
}

func validateHTTPSURL(raw string, allowQuery bool) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("URL must be absolute HTTPS without credentials")
	}
	if !allowQuery && (parsed.RawQuery != "" || parsed.Fragment != "") {
		return nil, fmt.Errorf("URL must not contain a query or fragment")
	}
	return parsed, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("evidence document contains trailing JSON data")
	}
	return nil
}
