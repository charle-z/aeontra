package edge

import (
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	MaxBrowserSteps          = 32
	MaxBrowserTextBytes      = 16 << 10
	MaxBrowserArtifactChunk  = 24 << 10
	MaxBrowserArtifactBytes  = 2 << 20
	MaxBrowserSessions       = 20
	MaxBrowserStepInputBytes = 32 << 10
)

var browserSessionIDPattern = regexp.MustCompile(`^br_[a-f0-9]{32}$`)
var browserArtifactIDPattern = regexp.MustCompile(`^ba_[a-f0-9]{32}$`)
var browserSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// BrowserStep is a closed browser action. No caller-supplied JavaScript, browser
// flags, executable, filesystem path or network endpoint is accepted.
type BrowserStep struct {
	Action       string `json:"action"`
	URL          string `json:"url,omitempty"`
	Selector     string `json:"selector,omitempty"`
	SelectorType string `json:"selector_type,omitempty"`
	Text         string `json:"text,omitempty"`
	Key          string `json:"key,omitempty"`
	Value        string `json:"value,omitempty"`
	Clear        bool   `json:"clear,omitempty"`
	Milliseconds int    `json:"milliseconds,omitempty"`
}

type BrowserSessionSummary struct {
	SessionID    string `json:"session_id"`
	State        string `json:"state"`
	NetworkScope string `json:"network_scope"`
	SafeURL      string `json:"safe_url,omitempty"`
	Title        string `json:"title,omitempty"`
	Revision     uint64 `json:"revision"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func normalizeProjectBrowserRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.BrowserSessionID = strings.TrimSpace(request.BrowserSessionID)
	request.BrowserArtifactID = strings.TrimSpace(request.BrowserArtifactID)
	request.BrowserNetworkScope = strings.ToLower(strings.TrimSpace(request.BrowserNetworkScope))
	request.BrowserCapture = strings.ToLower(strings.TrimSpace(request.BrowserCapture))
	if !projectOperationAliasPattern.MatchString(request.Alias) || !projectOperationTargetPattern.MatchString(request.TargetAlias) || request.Profile != "linux-workcell" ||
		request.Repository != "" || request.Platform != "" || request.Machine != "" || request.Target != "" || request.Difficulty != "" || request.OperatingSystem != "" ||
		request.WorkspaceID != "" || request.RunUntil != "" || request.Release != "" || !emptyProjectExecRequestFields(request) || !emptyProjectProcessRequestFields(request) ||
		request.GitPlanID != "" || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) {
		return OperationRequest{}, errors.New("project browser request is invalid")
	}
	needsKey := kind == OperationProjectBrowserCreate || kind == OperationProjectBrowserRun || kind == OperationProjectBrowserClose || kind == OperationProjectBrowserCleanup
	if needsKey != projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return OperationRequest{}, errors.New("project browser idempotency is invalid")
	}
	if !needsKey && request.IdempotencyKey != "" {
		return OperationRequest{}, errors.New("project browser idempotency is invalid")
	}
	switch kind {
	case OperationProjectBrowserCreate:
		if request.BrowserSessionID != "" || request.BrowserArtifactID != "" || len(request.BrowserSteps) != 0 || request.BrowserCapture != "" || request.BrowserFullPage ||
			request.BrowserArtifactOffset != 0 || request.BrowserArtifactLimit != 0 || request.BrowserTimeoutSeconds != 0 || request.BrowserNetworkScope != "general" {
			return OperationRequest{}, errors.New("project browser create request is invalid")
		}
		if request.BrowserViewportWidth == 0 {
			request.BrowserViewportWidth = 1280
		}
		if request.BrowserViewportHeight == 0 {
			request.BrowserViewportHeight = 720
		}
		if request.BrowserViewportWidth < 320 || request.BrowserViewportWidth > 1920 || request.BrowserViewportHeight < 240 || request.BrowserViewportHeight > 1080 {
			return OperationRequest{}, errors.New("project browser viewport is invalid")
		}
		if request.BrowserInitialURL != "" {
			if err := validateBrowserTopLevelURLSyntax(request.BrowserInitialURL, request.BrowserNetworkScope); err != nil {
				return OperationRequest{}, err
			}
		}
		return request, nil
	case OperationProjectBrowserStatus:
		if !browserSessionIDPattern.MatchString(request.BrowserSessionID) || !emptyBrowserActionFields(request) {
			return OperationRequest{}, errors.New("project browser status request is invalid")
		}
		return request, nil
	case OperationProjectBrowserList:
		if request.BrowserSessionID != "" || !emptyBrowserActionFields(request) {
			return OperationRequest{}, errors.New("project browser list request is invalid")
		}
		return request, nil
	case OperationProjectBrowserRun:
		if !browserSessionIDPattern.MatchString(request.BrowserSessionID) || request.BrowserNetworkScope != "" || request.BrowserInitialURL != "" ||
			request.BrowserViewportWidth != 0 || request.BrowserViewportHeight != 0 || request.BrowserIgnoreHTTPSErrors || request.BrowserArtifactID != "" ||
			request.BrowserArtifactOffset != 0 || request.BrowserArtifactLimit != 0 || len(request.BrowserSteps) < 1 || len(request.BrowserSteps) > MaxBrowserSteps ||
			request.BrowserTimeoutSeconds < 1 || request.BrowserTimeoutSeconds > 120 ||
			(request.BrowserCapture != "none" && request.BrowserCapture != "text" && request.BrowserCapture != "screenshot" && request.BrowserCapture != "both") {
			return OperationRequest{}, errors.New("project browser run request is invalid")
		}
		totalStepBytes := 0
		for i := range request.BrowserSteps {
			step, err := normalizeBrowserStep(request.BrowserSteps[i])
			if err != nil {
				return OperationRequest{}, err
			}
			totalStepBytes += len(step.URL) + len(step.Selector) + len(step.Text) + len(step.Key) + len(step.Value)
			if totalStepBytes > MaxBrowserStepInputBytes {
				return OperationRequest{}, errors.New("project browser steps are oversized")
			}
			request.BrowserSteps[i] = step
		}
		return request, nil
	case OperationProjectBrowserArtifactRead:
		if !browserSessionIDPattern.MatchString(request.BrowserSessionID) || !browserArtifactIDPattern.MatchString(request.BrowserArtifactID) ||
			request.BrowserArtifactOffset < 0 || request.BrowserArtifactLimit < 1 || request.BrowserArtifactLimit > MaxBrowserArtifactChunk || !emptyBrowserNonArtifactFields(request) {
			return OperationRequest{}, errors.New("project browser artifact request is invalid")
		}
		return request, nil
	case OperationProjectBrowserClose:
		if !browserSessionIDPattern.MatchString(request.BrowserSessionID) || !emptyBrowserActionFields(request) {
			return OperationRequest{}, errors.New("project browser close request is invalid")
		}
		return request, nil
	case OperationProjectBrowserCleanup:
		if request.BrowserSessionID != "" && !browserSessionIDPattern.MatchString(request.BrowserSessionID) {
			return OperationRequest{}, errors.New("project browser cleanup request is invalid")
		}
		if !emptyBrowserActionFields(request) {
			return OperationRequest{}, errors.New("project browser cleanup request is invalid")
		}
		return request, nil
	default:
		return OperationRequest{}, errors.New("project browser kind is invalid")
	}
}

func normalizeBrowserStep(step BrowserStep) (BrowserStep, error) {
	step.Action = strings.ToLower(strings.TrimSpace(step.Action))
	step.URL = strings.TrimSpace(step.URL)
	step.Selector = strings.TrimSpace(step.Selector)
	step.SelectorType = strings.ToLower(strings.TrimSpace(step.SelectorType))
	step.Key = strings.TrimSpace(step.Key)
	step.Value = strings.TrimSpace(step.Value)
	if step.SelectorType == "" {
		step.SelectorType = "css"
	}
	if len(step.URL) > 2048 || len(step.Selector) > 1024 || len(step.Text) > MaxBrowserTextBytes || len(step.Value) > 4096 ||
		!utf8.ValidString(step.URL+step.Selector+step.Text+step.Value) || strings.ContainsRune(step.URL+step.Selector+step.Text+step.Value, 0) || browserSecretShaped(step.URL+"\n"+step.Text+"\n"+step.Value) {
		return BrowserStep{}, errors.New("project browser step is invalid")
	}
	selectorOK := step.Selector != "" && (step.SelectorType == "css" || step.SelectorType == "text")
	switch step.Action {
	case "navigate":
		if step.Selector != "" || step.Text != "" || step.Key != "" || step.Value != "" || step.Clear || step.Milliseconds != 0 || validateBrowserTopLevelURLSyntax(step.URL, "") != nil {
			return BrowserStep{}, errors.New("browser navigate step is invalid")
		}
	case "click":
		if !selectorOK || step.URL != "" || step.Text != "" || step.Key != "" || step.Value != "" || step.Clear || step.Milliseconds != 0 {
			return BrowserStep{}, errors.New("browser click step is invalid")
		}
	case "type":
		if !selectorOK || step.URL != "" || step.Key != "" || step.Value != "" || step.Milliseconds != 0 {
			return BrowserStep{}, errors.New("browser type step is invalid")
		}
	case "press":
		keys := map[string]bool{"Enter": true, "Tab": true, "Escape": true, "ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true, "Home": true, "End": true, "PageUp": true, "PageDown": true, "Backspace": true, "Delete": true}
		if !keys[step.Key] || step.URL != "" || step.Text != "" || step.Value != "" || step.Clear || step.Milliseconds != 0 || (step.Selector != "" && !selectorOK) {
			return BrowserStep{}, errors.New("browser press step is invalid")
		}
	case "select":
		if !selectorOK || step.Value == "" || step.URL != "" || step.Text != "" || step.Key != "" || step.Clear || step.Milliseconds != 0 {
			return BrowserStep{}, errors.New("browser select step is invalid")
		}
	case "wait":
		if step.URL != "" || step.Text != "" || step.Key != "" || step.Value != "" || step.Clear || (step.Selector == "") == (step.Milliseconds == 0) || step.Milliseconds < 0 || step.Milliseconds > 5000 || (step.Selector != "" && !selectorOK) {
			return BrowserStep{}, errors.New("browser wait step is invalid")
		}
	default:
		return BrowserStep{}, errors.New("browser action is invalid")
	}
	return step, nil
}

func validateBrowserTopLevelURLSyntax(raw, scope string) error {
	if len(raw) < 1 || len(raw) > 2048 || browserSecretShaped(raw) {
		return errors.New("browser URL is invalid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return errors.New("browser URL is invalid")
	}
	if scope != "" && scope != "general" {
		return errors.New("browser network scope is invalid")
	}
	return nil
}

func browserSecretShaped(value string) bool {
	redacted, changed := policy.Redact(value)
	return changed || redacted != value
}

func emptyBrowserActionFields(r OperationRequest) bool {
	return r.BrowserNetworkScope == "" && r.BrowserInitialURL == "" && r.BrowserViewportWidth == 0 && r.BrowserViewportHeight == 0 && !r.BrowserIgnoreHTTPSErrors && len(r.BrowserSteps) == 0 && r.BrowserCapture == "" && !r.BrowserFullPage && r.BrowserTimeoutSeconds == 0 && r.BrowserArtifactID == "" && r.BrowserArtifactOffset == 0 && r.BrowserArtifactLimit == 0
}
func emptyBrowserNonArtifactFields(r OperationRequest) bool {
	return r.BrowserNetworkScope == "" && r.BrowserInitialURL == "" && r.BrowserViewportWidth == 0 && r.BrowserViewportHeight == 0 && !r.BrowserIgnoreHTTPSErrors && len(r.BrowserSteps) == 0 && r.BrowserCapture == "" && !r.BrowserFullPage && r.BrowserTimeoutSeconds == 0
}
func emptyProjectBrowserRequestFields(r OperationRequest) bool {
	return r.BrowserSessionID == "" && emptyBrowserActionFields(r)
}

func hasProjectBrowserResult(r OperationResult) bool {
	return r.BrowserSessionID != "" || r.BrowserState != "" || r.BrowserNetworkScope != "" || r.BrowserSafeURL != "" || r.BrowserTitle != "" || r.BrowserRevision != 0 ||
		r.BrowserCreatedAt != "" || r.BrowserUpdatedAt != "" || r.BrowserText != "" || r.BrowserTextTruncated || r.BrowserArtifactID != "" || r.BrowserArtifactMediaType != "" ||
		r.BrowserArtifactBytes != 0 || r.BrowserArtifactSHA256 != "" || r.BrowserArtifactOffset != 0 || r.BrowserArtifactNext != 0 || r.BrowserArtifactEOF || r.BrowserArtifactDataBase64 != "" ||
		len(r.BrowserSessions) != 0 || r.BrowserListComplete || r.BrowserCleanupCompleted || r.BrowserCleanupRemoved != 0 || r.BrowserCleanupArtifacts != 0
}

func validProjectBrowserResultForKind(kind OperationKind, r OperationResult) bool {
	metadata := r
	clearBrowserResult(&metadata)
	if !validProjectOperationResult(metadata) {
		return false
	}
	switch kind {
	case OperationProjectBrowserCreate, OperationProjectBrowserStatus, OperationProjectBrowserRun, OperationProjectBrowserClose:
		if !validBrowserSessionSummary(BrowserSessionSummary{SessionID: r.BrowserSessionID, State: r.BrowserState, NetworkScope: r.BrowserNetworkScope, SafeURL: r.BrowserSafeURL, Title: r.BrowserTitle, Revision: r.BrowserRevision, CreatedAt: r.BrowserCreatedAt, UpdatedAt: r.BrowserUpdatedAt}) {
			return false
		}
		if kind != OperationProjectBrowserRun && (r.BrowserText != "" || r.BrowserTextTruncated || r.BrowserArtifactID != "" || r.BrowserArtifactBytes != 0 || r.BrowserArtifactSHA256 != "") {
			return false
		}
		if kind == OperationProjectBrowserRun && !validBrowserRunExtras(r) {
			return false
		}
		return len(r.BrowserSessions) == 0 && r.BrowserArtifactDataBase64 == "" && r.BrowserCleanupRemoved == 0 && r.BrowserCleanupArtifacts == 0
	case OperationProjectBrowserList:
		if !r.BrowserListComplete || r.BrowserCleanupCompleted || len(r.BrowserSessions) > MaxBrowserSessions {
			return false
		}
		for _, s := range r.BrowserSessions {
			if !validBrowserSessionSummary(s) {
				return false
			}
		}
		return r.BrowserSessionID == "" && r.BrowserArtifactID == ""
	case OperationProjectBrowserArtifactRead:
		if !browserSessionIDPattern.MatchString(r.BrowserSessionID) || !browserArtifactIDPattern.MatchString(r.BrowserArtifactID) || r.BrowserArtifactMediaType != "image/jpeg" || r.BrowserArtifactBytes < 1 || r.BrowserArtifactBytes > MaxBrowserArtifactBytes || !browserSHA256Pattern.MatchString(r.BrowserArtifactSHA256) || r.BrowserArtifactOffset < 0 || r.BrowserArtifactNext < r.BrowserArtifactOffset || r.BrowserArtifactNext > r.BrowserArtifactBytes {
			return false
		}
		decoded, err := base64.StdEncoding.DecodeString(r.BrowserArtifactDataBase64)
		return err == nil && len(decoded) <= MaxBrowserArtifactChunk && int64(len(decoded)) == r.BrowserArtifactNext-r.BrowserArtifactOffset && r.BrowserArtifactEOF == (r.BrowserArtifactNext == r.BrowserArtifactBytes)
	case OperationProjectBrowserCleanup:
		return r.BrowserCleanupCompleted && !r.BrowserListComplete && r.BrowserCleanupRemoved >= 0 && r.BrowserCleanupArtifacts >= 0 && r.BrowserSessionID == "" && len(r.BrowserSessions) == 0
	default:
		return false
	}
}

func validBrowserRunExtras(r OperationResult) bool {
	if len(r.BrowserText) > MaxBrowserTextBytes || !utf8.ValidString(r.BrowserText) || strings.ContainsRune(r.BrowserText, 0) {
		return false
	}
	if r.BrowserArtifactID == "" {
		return r.BrowserArtifactMediaType == "" && r.BrowserArtifactBytes == 0 && r.BrowserArtifactSHA256 == ""
	}
	return browserArtifactIDPattern.MatchString(r.BrowserArtifactID) && r.BrowserArtifactMediaType == "image/jpeg" && r.BrowserArtifactBytes > 0 && r.BrowserArtifactBytes <= MaxBrowserArtifactBytes && browserSHA256Pattern.MatchString(r.BrowserArtifactSHA256)
}
func validBrowserSessionSummary(s BrowserSessionSummary) bool {
	if !browserSessionIDPattern.MatchString(s.SessionID) || (s.State != "ready" && s.State != "busy" && s.State != "closed") || s.NetworkScope != "general" || s.Revision < 1 || len(s.Title) > 512 || !utf8.ValidString(s.Title) {
		return false
	}
	if s.SafeURL != "" {
		u, err := url.Parse(s.SafeURL)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return false
		}
	}
	created, err1 := time.Parse(time.RFC3339Nano, s.CreatedAt)
	updated, err2 := time.Parse(time.RFC3339Nano, s.UpdatedAt)
	return err1 == nil && err2 == nil && !updated.Before(created)
}
func clearBrowserResult(r *OperationResult) {
	r.BrowserSessionID = ""
	r.BrowserState = ""
	r.BrowserNetworkScope = ""
	r.BrowserSafeURL = ""
	r.BrowserTitle = ""
	r.BrowserRevision = 0
	r.BrowserCreatedAt = ""
	r.BrowserUpdatedAt = ""
	r.BrowserText = ""
	r.BrowserTextTruncated = false
	r.BrowserArtifactID = ""
	r.BrowserArtifactMediaType = ""
	r.BrowserArtifactBytes = 0
	r.BrowserArtifactSHA256 = ""
	r.BrowserArtifactOffset = 0
	r.BrowserArtifactNext = 0
	r.BrowserArtifactEOF = false
	r.BrowserArtifactDataBase64 = ""
	r.BrowserSessions = nil
	r.BrowserListComplete = false
	r.BrowserCleanupCompleted = false
	r.BrowserCleanupRemoved = 0
	r.BrowserCleanupArtifacts = 0
}
