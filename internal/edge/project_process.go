package edge

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxProjectProcessReadBytes       = 24 << 10
	MaxProjectProcessStdinBytes      = 32 << 10
	MaxProjectProcessStdinTotalBytes = 16 << 20
)

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
	case OperationProjectProcessStatus, OperationProjectProcessStdin, OperationProjectProcessStop, OperationProjectProcessSignal, OperationProjectProcessList, OperationProjectProcessCleanup:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		request.BackgroundProcessID = strings.TrimSpace(request.BackgroundProcessID)
		if !validProjectOperationRequestCommon(request) || request.Repository != "" {
			return OperationRequest{}, errors.New("project process request is invalid")
		}
		if kind == OperationProjectProcessStdin {
			request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
			if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) ||
				request.ProcessStdinOffset < 0 || request.ProcessStdinOffset > MaxProjectProcessStdinTotalBytes-int64(len(request.Stdin)) || len(request.Stdin) > MaxProjectProcessStdinBytes || !utf8.ValidString(request.Stdin) || strings.ContainsRune(request.Stdin, 0) ||
				projectExecSecretShaped(request.Stdin) ||
				(request.Stdin == "" && !request.ProcessStdinClose) || len(request.Argv) != 0 || request.CWD != "" || len(request.Environment) != 0 || request.TimeoutSeconds != 0 ||
				request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 || request.BackgroundSignal != "" || request.ProcessLimit != 0 {
				return OperationRequest{}, errors.New("project process stdin request is invalid")
			}
			return request, nil
		}
		if request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) || request.ProcessStdinOffset != 0 || request.ProcessStdinClose {
			return OperationRequest{}, errors.New("project process request is invalid")
		}
		if kind == OperationProjectProcessStatus {
			if !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) || request.StdoutOffset < 0 || request.StderrOffset < 0 || request.OutputLimit < 1 || request.OutputLimit > MaxProjectProcessReadBytes || request.GraceSeconds != 0 || request.BackgroundSignal != "" || request.ProcessLimit != 0 {
				return OperationRequest{}, errors.New("project process status request is invalid")
			}
			return request, nil
		}
		if kind == OperationProjectProcessStop {
			if !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds < 1 || request.GraceSeconds > 30 || request.BackgroundSignal != "" || request.ProcessLimit != 0 {
				return OperationRequest{}, errors.New("project process stop request is invalid")
			}
			return request, nil
		}
		if kind == OperationProjectProcessSignal {
			if !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) || !backgroundProcessSignalPattern.MatchString(request.BackgroundSignal) || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 || request.ProcessLimit != 0 {
				return OperationRequest{}, errors.New("project process signal request is invalid")
			}
			return request, nil
		}
		if kind == OperationProjectProcessList {
			if request.BackgroundProcessID != "" || request.BackgroundSignal != "" || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 || request.ProcessLimit < 1 || request.ProcessLimit > 100 {
				return OperationRequest{}, errors.New("project process list request is invalid")
			}
			return request, nil
		}
		if (request.BackgroundProcessID != "" && !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID)) || request.BackgroundSignal != "" || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 || request.ProcessLimit != 0 {
			return OperationRequest{}, errors.New("project process cleanup request is invalid")
		}
		return request, nil
	default:
		return OperationRequest{}, errors.New("project process operation is invalid")
	}
}

func emptyProjectProcessRequestFields(request OperationRequest) bool {
	return request.BackgroundProcessID == "" && request.ProcessStdinOffset == 0 && !request.ProcessStdinClose && request.StdoutOffset == 0 && request.StderrOffset == 0 && request.OutputLimit == 0 && request.GraceSeconds == 0 && request.BackgroundSignal == "" && request.ProcessLimit == 0
}

func hasProjectProcessResult(result OperationResult) bool {
	return result.BackgroundProcessID != "" || result.BackgroundProcessState != "" || result.BackgroundStartedAt != "" ||
		result.BackgroundFinishedAt != "" || result.BackgroundExitKnown || result.BackgroundExitCode != 0 ||
		result.BackgroundTerminalSignal != "" || result.BackgroundReason != "" || result.BackgroundStdout != "" ||
		result.BackgroundStderr != "" || result.BackgroundStdoutNext != 0 || result.BackgroundStderrNext != 0 ||
		result.BackgroundStdoutEOF || result.BackgroundStderrEOF || result.BackgroundStdoutTruncated || result.BackgroundStderrTruncated ||
		result.BackgroundStdinNext != 0 || result.BackgroundStdinAccepted != 0 || result.BackgroundStdinClosed || result.BackgroundStdinReused ||
		len(result.BackgroundProcesses) != 0 || result.BackgroundCleanupRemoved != 0 || result.BackgroundCleanupActive != 0
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
	metadata.BackgroundStdinNext = 0
	metadata.BackgroundStdinAccepted = 0
	metadata.BackgroundStdinClosed = false
	metadata.BackgroundStdinReused = false
	metadata.BackgroundProcesses = nil
	metadata.BackgroundCleanupRemoved = 0
	metadata.BackgroundCleanupActive = 0
	return validProjectOperationResult(metadata)
}

func validProjectProcessStdinResult(result OperationResult) bool {
	if result.BackgroundProcessState != "running" || result.BackgroundStdinNext < 0 || result.BackgroundStdinAccepted < 0 || result.BackgroundStdinAccepted > MaxProjectProcessStdinBytes ||
		result.BackgroundStdinNext > MaxProjectProcessStdinTotalBytes || result.BackgroundStdinNext < int64(result.BackgroundStdinAccepted) || result.BackgroundStdinAccepted == 0 && !result.BackgroundStdinClosed ||
		result.BackgroundStdout != "" || result.BackgroundStderr != "" || result.BackgroundStdoutNext != 0 || result.BackgroundStderrNext != 0 ||
		result.BackgroundStdoutEOF || result.BackgroundStderrEOF || result.BackgroundStdoutTruncated || result.BackgroundStderrTruncated {
		return false
	}
	metadata := result
	metadata.BackgroundStdinNext = 0
	metadata.BackgroundStdinAccepted = 0
	metadata.BackgroundStdinClosed = false
	metadata.BackgroundStdinReused = false
	return validProjectProcessResult(metadata)
}

func hasProjectProcessStdinReceipt(result OperationResult) bool {
	return result.BackgroundStdinNext != 0 || result.BackgroundStdinAccepted != 0 || result.BackgroundStdinClosed || result.BackgroundStdinReused
}

func validProjectProcessListResult(result OperationResult) bool {
	if len(result.BackgroundProcesses) > 100 || result.BackgroundProcessID != "" || result.BackgroundProcessState != "" ||
		result.BackgroundCleanupRemoved != 0 || result.BackgroundCleanupActive != 0 {
		return false
	}
	for _, item := range result.BackgroundProcesses {
		if !validBackgroundProcessSummary(item) {
			return false
		}
	}
	metadata := result
	metadata.BackgroundProcesses = nil
	return validProjectOperationResult(metadata)
}

func validProjectProcessCleanupResult(result OperationResult) bool {
	if result.BackgroundCleanupRemoved < 0 || result.BackgroundCleanupRemoved > 4096 || result.BackgroundCleanupActive < 0 || result.BackgroundCleanupActive > 4096 ||
		len(result.BackgroundProcesses) != 0 || result.BackgroundProcessID != "" || result.BackgroundProcessState != "" {
		return false
	}
	metadata := result
	metadata.BackgroundCleanupRemoved = 0
	metadata.BackgroundCleanupActive = 0
	return validProjectOperationResult(metadata)
}

func validBackgroundProcessSummary(item BackgroundProcessSummary) bool {
	if !backgroundProcessIDPattern.MatchString(item.ProcessID) || !backgroundProcessStatePattern.MatchString(item.State) {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, item.StartedAt)
	if err != nil || started.IsZero() {
		return false
	}
	terminal := item.State == "exited" || item.State == "failed" || item.State == "stopped"
	if terminal {
		finished, err := time.Parse(time.RFC3339Nano, item.FinishedAt)
		if err != nil || finished.Before(started) {
			return false
		}
	} else if item.FinishedAt != "" || item.ExitKnown || item.ExitCode != 0 || item.TerminalSignal != "" || item.Reason != "" {
		return false
	}
	if item.ExitKnown && (item.ExitCode < 0 || item.ExitCode > 255) {
		return false
	}
	if item.TerminalSignal != "" && !backgroundProcessSignalPattern.MatchString(item.TerminalSignal) {
		return false
	}
	return item.Reason == "" || backgroundProcessReasonPattern.MatchString(item.Reason)
}
