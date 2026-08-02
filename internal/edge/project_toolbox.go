package edge

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxProjectToolboxOutputBytes = 24 << 10

var (
	projectToolboxIDPattern      = regexp.MustCompile(`^tb_[a-f0-9]{32}$`)
	projectToolboxStatePattern   = regexp.MustCompile(`^(created|running|stopped|unknown|removed)$`)
	projectToolboxImageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func normalizeProjectToolboxRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if !emptyProjectProcessRequestFields(request) || request.GitPlanID != "" {
		return OperationRequest{}, errors.New("project toolbox request is invalid")
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if !validProjectOperationRequestCommon(request) || request.Repository != "" {
		return OperationRequest{}, errors.New("project toolbox request is invalid")
	}
	switch kind {
	case OperationProjectToolboxStatus:
		if request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox status request is invalid")
		}
	case OperationProjectToolboxCreate, OperationProjectToolboxCleanup:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox lifecycle request is invalid")
		}
	case OperationProjectToolboxExec, OperationProjectToolboxInstall:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 3600 {
			return OperationRequest{}, errors.New("project toolbox exec request is invalid")
		}
		timeout := request.TimeoutSeconds
		request.TimeoutSeconds = 1
		normalized, err := normalizeProjectExecRequest(request)
		if err != nil {
			return OperationRequest{}, errors.New("project toolbox exec request is invalid")
		}
		normalized.TimeoutSeconds = timeout
		return normalized, nil
	default:
		return OperationRequest{}, errors.New("project toolbox operation is invalid")
	}
	return request, nil
}

func hasProjectToolboxResult(result OperationResult) bool {
	return result.ToolboxID != "" || result.ToolboxState != "" || result.ToolboxBase != "" || result.ToolboxBaseImageID != "" ||
		result.ToolboxCreatedAt != "" || result.ToolboxUpdatedAt != "" || result.ToolboxOutput != "" || result.ToolboxOutputTruncated || result.ToolboxRemoved
}

func validProjectToolboxResult(result OperationResult) bool {
	if !projectToolboxIDPattern.MatchString(result.ToolboxID) || !projectToolboxStatePattern.MatchString(result.ToolboxState) ||
		result.ToolboxBase != "debian-bookworm-slim" || !projectToolboxImageIDPattern.MatchString(result.ToolboxBaseImageID) ||
		len(result.ToolboxOutput) > MaxProjectToolboxOutputBytes || !utf8.ValidString(result.ToolboxOutput) || strings.ContainsRune(result.ToolboxOutput, 0) {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, result.ToolboxCreatedAt)
	if err != nil || created.IsZero() {
		return false
	}
	updated, err := time.Parse(time.RFC3339Nano, result.ToolboxUpdatedAt)
	if err != nil || updated.Before(created) {
		return false
	}
	if result.ToolboxRemoved != (result.ToolboxState == "removed") {
		return false
	}
	metadata := result
	metadata.ToolboxID, metadata.ToolboxState, metadata.ToolboxBase, metadata.ToolboxBaseImageID = "", "", "", ""
	metadata.ToolboxCreatedAt, metadata.ToolboxUpdatedAt, metadata.ToolboxOutput = "", "", ""
	metadata.ToolboxOutputTruncated, metadata.ToolboxRemoved = false, false
	return validProjectOperationResult(metadata)
}

func validProjectToolboxResultForKind(kind OperationKind, result OperationResult) bool {
	if !validProjectToolboxResult(result) {
		return false
	}
	hasOutput := result.ToolboxOutput != "" || result.ToolboxOutputTruncated
	switch kind {
	case OperationProjectToolboxCreate, OperationProjectToolboxStatus:
		return !hasOutput && !result.ToolboxRemoved
	case OperationProjectToolboxExec, OperationProjectToolboxInstall:
		return !result.ToolboxRemoved && result.ToolboxState == "running"
	case OperationProjectToolboxCleanup:
		return !hasOutput && result.ToolboxRemoved
	default:
		return false
	}
}
