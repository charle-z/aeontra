package edge

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxProjectProcessReadBytes = 24 << 10

var backgroundProcessIDPattern = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
var backgroundProcessStatePattern = regexp.MustCompile(`^(starting|running|stopping|exited|failed|stopped)$`)
var backgroundProcessSignalPattern = regexp.MustCompile(`^(interrupt|terminate|kill)$`)
var backgroundProcessReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

func normalizeProjectProcessRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	switch kind {
	case OperationProjectProcessStart:
		if !emptyProjectProcessRequestFields(request) || request.TimeoutSeconds != 0 {
			return OperationRequest{}, errors.New("project process start request is invalid")
		}
		request.TimeoutSeconds = 1
		normalized, err := normalizeProjectExecRequest(request)
		if err != nil {
			return OperationRequest{}, errors.New("project process start request is invalid")
		}
		normalized.TimeoutSeconds = 0
		return normalized, nil
	case OperationProjectProcessStatus, OperationProjectProcessStop:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		request.BackgroundProcessID = strings.TrimSpace(request.BackgroundProcessID)
		if !validProjectOperationRequestCommon(request) || request.Repository != "" || request.IdempotencyKey != "" ||
			!emptyProjectExecRequestFields(request) || !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) {
			return OperationRequest{}, errors.New("project process request is invalid")
		}
		if kind == OperationProjectProcessStatus {
			if request.StdoutOffset < 0 || request.StderrOffset < 0 || request.OutputLimit < 1 || request.OutputLimit > MaxProjectProcessReadBytes || request.GraceSeconds != 0 {
				return OperationRequest{}, errors.New("project process status request is invalid")
			}
			return request, nil
		}
		if request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds < 1 || request.GraceSeconds > 30 {
			return OperationRequest{}, errors.New("project process stop request is invalid")
		}
		return request, nil
	default:
		return OperationRequest{}, errors.New("project process operation is invalid")
	}
}

func emptyProjectProcessRequestFields(request OperationRequest) bool {
	return request.BackgroundProcessID == "" && request.StdoutOffset == 0 && request.StderrOffset == 0 && request.OutputLimit == 0 && request.GraceSeconds == 0
}

func hasProjectProcessResult(result OperationResult) bool {
	return result.BackgroundProcessID != "" || result.BackgroundProcessState != "" || result.BackgroundStartedAt != "" ||
		result.BackgroundFinishedAt != "" || result.BackgroundExitKnown || result.BackgroundExitCode != 0 ||
		result.BackgroundTerminalSignal != "" || result.BackgroundReason != "" || result.BackgroundStdout != "" ||
		result.BackgroundStderr != "" || result.BackgroundStdoutNext != 0 || result.BackgroundStderrNext != 0 ||
		result.BackgroundStdoutEOF || result.BackgroundStderrEOF || result.BackgroundStdoutTruncated || result.BackgroundStderrTruncated
}

func validProjectProcessResult(result OperationResult) bool {
	if !backgroundProcessIDPattern.MatchString(result.BackgroundProcessID) || !backgroundProcessStatePattern.MatchString(result.BackgroundProcessState) ||
		len(result.BackgroundStdout) > MaxProjectProcessReadBytes || len(result.BackgroundStderr) > MaxProjectProcessReadBytes ||
		!utf8.ValidString(result.BackgroundStdout) || !utf8.ValidString(result.BackgroundStderr) ||
		strings.ContainsRune(result.BackgroundStdout, 0) || strings.ContainsRune(result.BackgroundStderr, 0) ||
		result.BackgroundStdoutNext < 0 || result.BackgroundStderrNext < 0 {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, result.BackgroundStartedAt)
	if err != nil || started.IsZero() {
		return false
	}
	terminal := result.BackgroundProcessState == "exited" || result.BackgroundProcessState == "failed" || result.BackgroundProcessState == "stopped"
	if terminal {
		finished, err := time.Parse(time.RFC3339Nano, result.BackgroundFinishedAt)
		if err != nil || finished.Before(started) {
			return false
		}
	} else if result.BackgroundFinishedAt != "" || result.BackgroundExitKnown || result.BackgroundExitCode != 0 || result.BackgroundTerminalSignal != "" || result.BackgroundReason != "" {
		return false
	}
	if result.BackgroundExitKnown && (result.BackgroundExitCode < 0 || result.BackgroundExitCode > 255) {
		return false
	}
	if result.BackgroundTerminalSignal != "" && !backgroundProcessSignalPattern.MatchString(result.BackgroundTerminalSignal) {
		return false
	}
	if result.BackgroundReason != "" && !backgroundProcessReasonPattern.MatchString(result.BackgroundReason) {
		return false
	}
	metadata := result
	metadata.BackgroundProcessID = ""
	metadata.BackgroundProcessState = ""
	metadata.BackgroundStartedAt = ""
	metadata.BackgroundFinishedAt = ""
	metadata.BackgroundExitKnown = false
	metadata.BackgroundExitCode = 0
	metadata.BackgroundTerminalSignal = ""
	metadata.BackgroundReason = ""
	metadata.BackgroundStdout = ""
	metadata.BackgroundStderr = ""
	metadata.BackgroundStdoutNext = 0
	metadata.BackgroundStderrNext = 0
	metadata.BackgroundStdoutEOF = false
	metadata.BackgroundStderrEOF = false
	metadata.BackgroundStdoutTruncated = false
	metadata.BackgroundStderrTruncated = false
	return validProjectOperationResult(metadata)
}
