package edge

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxProjectToolboxOutputBytes      = 24 << 10
	MinProjectToolboxCPUMillis        = 250
	MaxProjectToolboxCPUMillis        = 32000
	DefaultProjectToolboxCPUMillis    = 4000
	MinProjectToolboxMemoryMiB        = 512
	MaxProjectToolboxMemoryMiB        = 65536
	DefaultProjectToolboxMemoryMiB    = 8192
	MinProjectToolboxProcessLimit     = 128
	MaxProjectToolboxProcessLimit     = 8192
	DefaultProjectToolboxProcessLimit = 2048
)

var (
	projectToolboxIDPattern           = regexp.MustCompile(`^tb_[a-f0-9]{32}$`)
	projectToolboxStatePattern        = regexp.MustCompile(`^(created|running|stopped|unknown|removed)$`)
	projectToolboxImageIDPattern      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	projectToolboxServiceIDPattern    = regexp.MustCompile(`^ts_[a-f0-9]{32}$`)
	projectToolboxServiceNamePattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	projectToolboxServiceStatePattern = regexp.MustCompile(`^(starting|running|stopped)$`)
)

func normalizeProjectToolboxRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if !emptyProjectProcessRequestFields(request) || request.GitPlanID != "" {
		return OperationRequest{}, errors.New("project toolbox request is invalid")
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.ToolboxServiceID = strings.TrimSpace(request.ToolboxServiceID)
	request.ToolboxServiceName = strings.ToLower(strings.TrimSpace(request.ToolboxServiceName))
	if !validProjectOperationRequestCommon(request) || request.Repository != "" {
		return OperationRequest{}, errors.New("project toolbox request is invalid")
	}
	switch kind {
	case OperationProjectToolboxStatus:
		if request.IdempotencyKey != "" || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox status request is invalid")
		}
	case OperationProjectToolboxCreate:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox lifecycle request is invalid")
		}
		if request.ToolboxCPUMillis == 0 {
			request.ToolboxCPUMillis = DefaultProjectToolboxCPUMillis
		}
		if request.ToolboxMemoryMiB == 0 {
			request.ToolboxMemoryMiB = DefaultProjectToolboxMemoryMiB
		}
		if request.ToolboxProcessLimit == 0 {
			request.ToolboxProcessLimit = DefaultProjectToolboxProcessLimit
		}
		if !validProjectToolboxResources(request.ToolboxCPUMillis, request.ToolboxMemoryMiB, request.ToolboxProcessLimit) {
			return OperationRequest{}, errors.New("project toolbox resource limits are invalid")
		}
	case OperationProjectToolboxCleanup, OperationProjectToolboxRepair:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox lifecycle request is invalid")
		}
	case OperationProjectToolboxExec, OperationProjectToolboxInstall:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 3600 {
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
	case OperationProjectToolboxServiceStart:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.ToolboxServiceID != "" || hasProjectToolboxResourceRequest(request) ||
			!projectToolboxServiceNamePattern.MatchString(request.ToolboxServiceName) || request.TimeoutSeconds != 0 {
			return OperationRequest{}, errors.New("project toolbox service start request is invalid")
		}
		request.TimeoutSeconds = 1
		normalized, err := normalizeProjectExecRequest(request)
		if err != nil {
			return OperationRequest{}, errors.New("project toolbox service start request is invalid")
		}
		normalized.TimeoutSeconds = 0
		return normalized, nil
	case OperationProjectToolboxServiceStatus:
		if request.IdempotencyKey != "" || !projectToolboxServiceIDPattern.MatchString(request.ToolboxServiceID) || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox service status request is invalid")
		}
	case OperationProjectToolboxServiceStop:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !projectToolboxServiceIDPattern.MatchString(request.ToolboxServiceID) || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || !emptyProjectExecRequestFields(request) {
			return OperationRequest{}, errors.New("project toolbox service stop request is invalid")
		}
	default:
		return OperationRequest{}, errors.New("project toolbox operation is invalid")
	}
	return request, nil
}

func hasProjectToolboxResourceRequest(request OperationRequest) bool {
	return request.ToolboxCPUMillis != 0 || request.ToolboxMemoryMiB != 0 || request.ToolboxProcessLimit != 0
}

func validProjectToolboxResources(cpuMillis, memoryMiB, processLimit int) bool {
	return cpuMillis >= MinProjectToolboxCPUMillis && cpuMillis <= MaxProjectToolboxCPUMillis &&
		memoryMiB >= MinProjectToolboxMemoryMiB && memoryMiB <= MaxProjectToolboxMemoryMiB &&
		processLimit >= MinProjectToolboxProcessLimit && processLimit <= MaxProjectToolboxProcessLimit
}

func hasProjectToolboxResult(result OperationResult) bool {
	return result.ToolboxID != "" || result.ToolboxState != "" || result.ToolboxBase != "" || result.ToolboxBaseImageID != "" ||
		result.ToolboxCreatedAt != "" || result.ToolboxUpdatedAt != "" || result.ToolboxOutput != "" || result.ToolboxOutputTruncated || result.ToolboxRemoved
}

func hasProjectToolboxServiceResult(result OperationResult) bool {
	return result.ToolboxServiceID != "" || result.ToolboxServiceName != "" || result.ToolboxServiceState != "" ||
		result.ToolboxServiceCreatedAt != "" || result.ToolboxServiceUpdatedAt != ""
}

func validProjectToolboxResult(result OperationResult) bool {
	if !projectToolboxIDPattern.MatchString(result.ToolboxID) || !projectToolboxStatePattern.MatchString(result.ToolboxState) ||
		result.ToolboxBase != "debian-bookworm-slim" || !projectToolboxImageIDPattern.MatchString(result.ToolboxBaseImageID) ||
		!validProjectToolboxResources(result.ToolboxCPUMillis, result.ToolboxMemoryMiB, result.ToolboxProcessLimit) ||
		!result.ToolboxContainerAccess || result.ToolboxWritableBytes < 0 || result.ToolboxRootFSBytes <= 0 ||
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
	metadata.ToolboxCPUMillis, metadata.ToolboxMemoryMiB, metadata.ToolboxProcessLimit = 0, 0, 0
	metadata.ToolboxContainerAccess, metadata.ToolboxWritableBytes, metadata.ToolboxRootFSBytes = false, 0, 0
	return validProjectOperationResult(metadata)
}

func validProjectToolboxResultForKind(kind OperationKind, result OperationResult) bool {
	if !validProjectToolboxResult(result) {
		return false
	}
	hasOutput := result.ToolboxOutput != "" || result.ToolboxOutputTruncated
	switch kind {
	case OperationProjectToolboxCreate, OperationProjectToolboxStatus, OperationProjectToolboxRepair:
		return !hasOutput && !result.ToolboxRemoved
	case OperationProjectToolboxExec, OperationProjectToolboxInstall:
		return !result.ToolboxRemoved && result.ToolboxState == "running"
	case OperationProjectToolboxCleanup:
		return !hasOutput && result.ToolboxRemoved
	default:
		return false
	}
}

func validProjectToolboxServiceResultForKind(kind OperationKind, result OperationResult) bool {
	if !validProjectToolboxServiceResult(result) {
		return false
	}
	switch kind {
	case OperationProjectToolboxServiceStart:
		return result.ToolboxServiceState == "starting" || result.ToolboxServiceState == "running" || result.ToolboxServiceState == "stopped"
	case OperationProjectToolboxServiceStatus:
		return result.ToolboxServiceState == "running" || result.ToolboxServiceState == "stopped"
	case OperationProjectToolboxServiceStop:
		return result.ToolboxServiceState == "stopped"
	default:
		return false
	}
}

func validProjectToolboxServiceResult(result OperationResult) bool {
	if !projectToolboxServiceIDPattern.MatchString(result.ToolboxServiceID) ||
		!projectToolboxServiceNamePattern.MatchString(result.ToolboxServiceName) ||
		!projectToolboxServiceStatePattern.MatchString(result.ToolboxServiceState) {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, result.ToolboxServiceCreatedAt)
	if err != nil || created.IsZero() {
		return false
	}
	updated, err := time.Parse(time.RFC3339Nano, result.ToolboxServiceUpdatedAt)
	if err != nil || updated.Before(created) {
		return false
	}
	base := result
	base.ToolboxServiceID, base.ToolboxServiceName, base.ToolboxServiceState = "", "", ""
	base.ToolboxServiceCreatedAt, base.ToolboxServiceUpdatedAt = "", ""
	if !validProjectToolboxResult(base) || result.ToolboxOutput != "" || result.ToolboxOutputTruncated || result.ToolboxRemoved {
		return false
	}
	return true
}
