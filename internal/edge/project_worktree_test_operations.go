package edge

// This file owns closed request and result validation for managed test processes.

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	projectWorktreeTestProfileIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	projectWorktreeTestStaleReasonPattern = regexp.MustCompile(`^(content_changed|head_changed|branch_changed|profile_changed|edge_restarted|evidence_unavailable)$`)
)

func normalizeProjectWorktreeTestRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.WorktreeID = strings.TrimSpace(request.WorktreeID)
	request.WorkJobID = strings.TrimSpace(request.WorkJobID)
	request.WorkLeaseID = strings.TrimSpace(request.WorkLeaseID)
	request.TestProfileID = strings.TrimSpace(request.TestProfileID)
	request.TestProfileDigest = strings.ToLower(strings.TrimSpace(request.TestProfileDigest))
	request.BackgroundProcessID = strings.TrimSpace(request.BackgroundProcessID)
	if !validProjectOperationRequestCommon(request) || request.Repository != "" {
		return OperationRequest{}, errors.New("project worktree test request is invalid")
	}
	base := OperationRequest{Alias: request.Alias, TargetAlias: request.TargetAlias, Profile: request.Profile}
	switch kind {
	case OperationProjectWorktreeTestProfile:
		if request.TestProfileID != "" && !projectWorktreeTestProfileIDPattern.MatchString(request.TestProfileID) ||
			!projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project worktree test profile request is invalid")
		}
		base.TestProfileID, base.IdempotencyKey = request.TestProfileID, request.IdempotencyKey
	case OperationProjectWorktreeTestStart:
		if !validProjectWorktreeTestAuthority(request) || !projectWorktreeTestProfileIDPattern.MatchString(request.TestProfileID) ||
			!operationCatalogPattern.MatchString(request.TestProfileDigest) || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project worktree test start request is invalid")
		}
		base.WorktreeID, base.WorkJobID, base.WorkLeaseID, base.WorkFence = request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence
		base.TestProfileID, base.TestProfileDigest, base.IdempotencyKey = request.TestProfileID, request.TestProfileDigest, request.IdempotencyKey
	case OperationProjectWorktreeTestStatus:
		if !validProjectWorktreeTestAuthority(request) || !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) ||
			!projectWorktreeTestProfileIDPattern.MatchString(request.TestProfileID) || !operationCatalogPattern.MatchString(request.TestProfileDigest) ||
			request.StdoutOffset < 0 || request.StderrOffset < 0 || request.OutputLimit < 0 || request.OutputLimit > MaxProjectProcessReadBytes {
			return OperationRequest{}, errors.New("project worktree test status request is invalid")
		}
		if request.OutputLimit == 0 {
			request.OutputLimit = MaxProjectProcessReadBytes
		}
		base.WorktreeID, base.WorkJobID, base.WorkLeaseID, base.WorkFence = request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence
		base.BackgroundProcessID, base.StdoutOffset, base.StderrOffset, base.OutputLimit = request.BackgroundProcessID, request.StdoutOffset, request.StderrOffset, request.OutputLimit
		base.TestProfileID, base.TestProfileDigest = request.TestProfileID, request.TestProfileDigest
	case OperationProjectWorktreeTestStop:
		if !validProjectWorktreeTestAuthority(request) || !backgroundProcessIDPattern.MatchString(request.BackgroundProcessID) ||
			!projectWorktreeTestProfileIDPattern.MatchString(request.TestProfileID) || !operationCatalogPattern.MatchString(request.TestProfileDigest) ||
			!projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project worktree test stop request is invalid")
		}
		base.WorktreeID, base.WorkJobID, base.WorkLeaseID, base.WorkFence = request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence
		base.BackgroundProcessID = request.BackgroundProcessID
		base.TestProfileID, base.TestProfileDigest, base.IdempotencyKey = request.TestProfileID, request.TestProfileDigest, request.IdempotencyKey
	default:
		return OperationRequest{}, errors.New("project worktree test operation is invalid")
	}
	if !reflect.DeepEqual(request, base) {
		return OperationRequest{}, errors.New("project worktree test request contains unrelated fields")
	}
	return request, nil
}

func validProjectWorktreeTestAuthority(request OperationRequest) bool {
	return projectWorktreeIDPattern.MatchString(request.WorktreeID) && projectWorkJobIDPattern.MatchString(request.WorkJobID) &&
		projectWorkLeaseIDPattern.MatchString(request.WorkLeaseID) && request.WorkFence > 0 && request.WorkFence <= uint64(1<<63-1)
}

func hasProjectWorktreeTestResult(result OperationResult) bool {
	return result.TestProfileID != "" || result.TestProfileDigest != "" || result.TestTimeoutSeconds != 0 || result.ContentDigest != "" ||
		result.TestStale || result.TestStaleReason != ""
}

func validProjectWorktreeTestResultForKind(kind OperationKind, result OperationResult) bool {
	if kind == OperationProjectWorktreeTestProfile {
		if !projectWorktreeTestProfileIDPattern.MatchString(result.TestProfileID) || !operationCatalogPattern.MatchString(result.TestProfileDigest) || result.TestTimeoutSeconds < 1 || result.TestTimeoutSeconds > 3600 ||
			result.ContentDigest != "" || result.TestStale || result.TestStaleReason != "" || hasProjectProcessResult(result) {
			return false
		}
		metadata := result
		metadata.TestProfileID, metadata.TestProfileDigest, metadata.TestTimeoutSeconds = "", "", 0
		return emptyOperationResult(metadata)
	}
	if kind != OperationProjectWorktreeTestStart && kind != OperationProjectWorktreeTestStatus && kind != OperationProjectWorktreeTestStop {
		return false
	}
	if !validProjectWorktreeResultIdentity(result) || !projectWorktreeTestProfileIDPattern.MatchString(result.TestProfileID) ||
		!operationCatalogPattern.MatchString(result.TestProfileDigest) || result.TestTimeoutSeconds < 1 || result.TestTimeoutSeconds > 3600 ||
		!operationCatalogPattern.MatchString(result.ContentDigest) || !backgroundProcessIDPattern.MatchString(result.BackgroundProcessID) ||
		!backgroundProcessStatePattern.MatchString(result.BackgroundProcessState) ||
		len(result.BackgroundStdout) > MaxProjectProcessReadBytes || len(result.BackgroundStderr) > MaxProjectProcessReadBytes ||
		!utf8.ValidString(result.BackgroundStdout) || !utf8.ValidString(result.BackgroundStderr) || strings.ContainsRune(result.BackgroundStdout, 0) || strings.ContainsRune(result.BackgroundStderr, 0) ||
		result.BackgroundStdoutNext < 0 || result.BackgroundStderrNext < 0 ||
		(result.TestStale && !projectWorktreeTestStaleReasonPattern.MatchString(result.TestStaleReason)) || (!result.TestStale && result.TestStaleReason != "") {
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
	if result.BackgroundExitKnown && (result.BackgroundExitCode < 0 || result.BackgroundExitCode > 255) ||
		(result.BackgroundTerminalSignal != "" && !backgroundProcessSignalPattern.MatchString(result.BackgroundTerminalSignal)) ||
		(result.BackgroundReason != "" && !backgroundProcessReasonPattern.MatchString(result.BackgroundReason)) {
		return false
	}
	if kind == OperationProjectWorktreeTestStart && result.TestStale {
		return false
	}
	metadata := result
	metadata.WorkspaceID = ""
	metadata.WorktreeID, metadata.WorktreeState, metadata.WorktreeRole, metadata.WorktreeBaseCommit, metadata.WorktreeBranch = "", "", "", "", ""
	metadata.WorktreeEvidenceKnown, metadata.WorktreeHeadCommit, metadata.WorktreeClean = false, "", false
	metadata.WorktreeCommitsAheadBase, metadata.WorktreeChangedPathCount = 0, 0
	metadata.WorkJobID, metadata.WorkLeaseID, metadata.WorkFence = "", "", 0
	metadata.WorktreeCreatedAt, metadata.WorktreeUpdatedAt, metadata.Worktrees = "", "", nil
	metadata.TestProfileID, metadata.TestProfileDigest, metadata.TestTimeoutSeconds, metadata.ContentDigest = "", "", 0, ""
	metadata.TestStale, metadata.TestStaleReason = false, ""
	metadata.BackgroundProcessID, metadata.BackgroundProcessState, metadata.BackgroundStartedAt, metadata.BackgroundFinishedAt = "", "", "", ""
	metadata.BackgroundExitKnown, metadata.BackgroundExitCode, metadata.BackgroundTerminalSignal, metadata.BackgroundReason = false, 0, "", ""
	metadata.BackgroundStdout, metadata.BackgroundStderr, metadata.BackgroundStdoutNext, metadata.BackgroundStderrNext = "", "", 0, 0
	metadata.BackgroundStdoutEOF, metadata.BackgroundStderrEOF, metadata.BackgroundStdoutTruncated, metadata.BackgroundStderrTruncated = false, false, false, false
	return emptyOperationResult(metadata)
}

func validProjectWorktreeResultIdentity(result OperationResult) bool {
	return projectWorktreeIDPattern.MatchString(result.WorktreeID) && workspaceIDPattern.MatchString(result.WorkspaceID) && result.WorktreeState == "ready" &&
		result.WorktreeRole == "writer" && projectWorktreeCommitPattern.MatchString(result.WorktreeBaseCommit) && projectWorktreeBranchPattern.MatchString(result.WorktreeBranch) &&
		projectWorktreeCommitPattern.MatchString(result.WorktreeHeadCommit) && projectWorkJobIDPattern.MatchString(result.WorkJobID) &&
		projectWorkLeaseIDPattern.MatchString(result.WorkLeaseID) && result.WorkFence > 0 && result.WorkFence <= uint64(1<<63-1)
}
