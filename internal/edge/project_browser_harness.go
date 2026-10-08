package edge

import (
	"encoding/base64"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxBrowserHarnessLogBytes      = 24 << 10
	MaxBrowserHarnessArtifactChunk = 24 << 10
	MaxBrowserHarnessRuns          = 50
	MaxBrowserHarnessArtifacts     = 100
)

var (
	browserHarnessRunIDPattern   = regexp.MustCompile(`^bh_[a-f0-9]{32}$`)
	browserHarnessProfilePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)
	browserHarnessSHA256Pattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func normalizeProjectBrowserHarnessRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.BrowserHarnessRunID = strings.TrimSpace(request.BrowserHarnessRunID)
	request.BrowserHarnessProfile = strings.ToLower(strings.TrimSpace(request.BrowserHarnessProfile))
	request.BrowserHarnessArtifactPath = strings.TrimSpace(request.BrowserHarnessArtifactPath)
	if !projectOperationAliasPattern.MatchString(request.Alias) || !projectOperationTargetPattern.MatchString(request.TargetAlias) || request.Profile != "linux-workcell" ||
		request.Repository != "" || request.Platform != "" || request.Machine != "" || request.Target != "" || request.Difficulty != "" || request.OperatingSystem != "" || request.WorkspaceID != "" || request.RunUntil != "" || request.Release != "" ||
		request.GitPlanID != "" || request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) || !emptyProjectBrowserRequestFields(request) {
		return OperationRequest{}, errors.New("project browser harness request is invalid")
	}
	switch kind {
	case OperationProjectBrowserHarnessStart:
		if !browserHarnessProfilePattern.MatchString(request.BrowserHarnessProfile) || request.BrowserHarnessRunID != "" || request.BrowserHarnessTimeoutSeconds < 1 || request.BrowserHarnessTimeoutSeconds > 604800 || request.BrowserHarnessStorageMiB < 64 || request.BrowserHarnessStorageMiB > 65536 || request.BrowserHarnessListLimit != 0 || request.BrowserHarnessRemoveProfile || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness start request is invalid")
		}
		base := request
		clearProjectBrowserHarnessRequest(&base)
		base.TimeoutSeconds = 120
		normalized, err := normalizeProjectExecRequest(base)
		if err != nil {
			return OperationRequest{}, err
		}
		request.Alias, request.TargetAlias, request.Profile, request.IdempotencyKey = normalized.Alias, normalized.TargetAlias, normalized.Profile, normalized.IdempotencyKey
		request.Argv, request.CWD, request.Environment = normalized.Argv, normalized.CWD, normalized.Environment
		request.TimeoutSeconds = 0
		return request, nil
	case OperationProjectBrowserHarnessStatus:
		if !browserHarnessRunIDPattern.MatchString(request.BrowserHarnessRunID) || request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit != 0 || request.BrowserHarnessRemoveProfile || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset < 0 || request.StderrOffset < 0 || request.OutputLimit < 1 || request.OutputLimit > MaxBrowserHarnessLogBytes || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness status request is invalid")
		}
		return request, nil
	case OperationProjectBrowserHarnessList:
		if request.BrowserHarnessRunID != "" || request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit < 1 || request.BrowserHarnessListLimit > MaxBrowserHarnessRuns || request.BrowserHarnessRemoveProfile || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness list request is invalid")
		}
		return request, nil
	case OperationProjectBrowserHarnessStop:
		if !browserHarnessRunIDPattern.MatchString(request.BrowserHarnessRunID) || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit != 0 || request.BrowserHarnessRemoveProfile || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds < 1 || request.GraceSeconds > 30 {
			return OperationRequest{}, errors.New("project browser harness stop request is invalid")
		}
		return request, nil
	case OperationProjectBrowserHarnessCleanup:
		if request.BrowserHarnessRunID != "" && !browserHarnessRunIDPattern.MatchString(request.BrowserHarnessRunID) || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit != 0 || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness cleanup request is invalid")
		}
		return request, nil
	case OperationProjectBrowserHarnessArtifactList:
		if !browserHarnessRunIDPattern.MatchString(request.BrowserHarnessRunID) || request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit < 1 || request.BrowserHarnessListLimit > MaxBrowserHarnessArtifacts || request.BrowserHarnessRemoveProfile || request.BrowserHarnessArtifactPath != "" || request.BrowserHarnessArtifactOffset != 0 || request.BrowserHarnessArtifactLimit != 0 || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness artifact list request is invalid")
		}
		return request, nil
	case OperationProjectBrowserHarnessArtifactRead:
		if !browserHarnessRunIDPattern.MatchString(request.BrowserHarnessRunID) || request.IdempotencyKey != "" || !emptyProjectExecRequestFields(request) || request.BrowserHarnessProfile != "" || request.BrowserHarnessTimeoutSeconds != 0 || request.BrowserHarnessStorageMiB != 0 || request.BrowserHarnessListLimit != 0 || request.BrowserHarnessRemoveProfile || !validBrowserHarnessArtifactPath(request.BrowserHarnessArtifactPath) || request.BrowserHarnessArtifactOffset < 0 || request.BrowserHarnessArtifactLimit < 1 || request.BrowserHarnessArtifactLimit > MaxBrowserHarnessArtifactChunk || request.StdoutOffset != 0 || request.StderrOffset != 0 || request.OutputLimit != 0 || request.GraceSeconds != 0 {
			return OperationRequest{}, errors.New("project browser harness artifact read request is invalid")
		}
		return request, nil
	default:
		return OperationRequest{}, errors.New("project browser harness kind is invalid")
	}
}

func clearProjectBrowserHarnessRequest(r *OperationRequest) {
	r.BrowserHarnessRunID = ""
	r.BrowserHarnessProfile = ""
	r.BrowserHarnessTimeoutSeconds = 0
	r.BrowserHarnessStorageMiB = 0
	r.BrowserHarnessListLimit = 0
	r.BrowserHarnessRemoveProfile = false
	r.BrowserHarnessArtifactPath = ""
	r.BrowserHarnessArtifactOffset = 0
	r.BrowserHarnessArtifactLimit = 0
}

func emptyProjectBrowserHarnessRequestFields(r OperationRequest) bool {
	return r.BrowserHarnessRunID == "" && r.BrowserHarnessProfile == "" && r.BrowserHarnessTimeoutSeconds == 0 && r.BrowserHarnessStorageMiB == 0 && r.BrowserHarnessListLimit == 0 && !r.BrowserHarnessRemoveProfile && r.BrowserHarnessArtifactPath == "" && r.BrowserHarnessArtifactOffset == 0 && r.BrowserHarnessArtifactLimit == 0
}

func validBrowserHarnessArtifactPath(value string) bool {
	if value == "" || len(value) > 1024 || path.IsAbs(value) || strings.ContainsAny(value, `\`+"\x00") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && (strings.HasPrefix(clean, "artifacts/") || strings.HasPrefix(clean, "downloads/"))
}

func hasProjectBrowserHarnessResult(r OperationResult) bool {
	return r.BrowserHarnessRunID != "" || r.BrowserHarnessState != "" || r.BrowserHarnessProfile != "" || r.BrowserHarnessCreatedAt != "" || r.BrowserHarnessUpdatedAt != "" || r.BrowserHarnessStartedAt != "" || r.BrowserHarnessFinishedAt != "" || r.BrowserHarnessExitKnown || r.BrowserHarnessExitCode != 0 || r.BrowserHarnessTimeoutSeconds != 0 || r.BrowserHarnessStorageMiB != 0 || r.BrowserHarnessStdout != "" || r.BrowserHarnessStderr != "" || r.BrowserHarnessStdoutNext != 0 || r.BrowserHarnessStderrNext != 0 || r.BrowserHarnessStdoutEOF || r.BrowserHarnessStderrEOF || r.BrowserHarnessStdoutTruncated || r.BrowserHarnessStderrTruncated || r.BrowserHarnessArtifactCount != 0 || r.BrowserHarnessArtifactBytes != 0 || len(r.BrowserHarnessRuns) != 0 || r.BrowserHarnessListComplete || len(r.BrowserHarnessArtifacts) != 0 || r.BrowserHarnessArtifactsComplete || r.BrowserHarnessArtifactPath != "" || r.BrowserHarnessArtifactMediaType != "" || r.BrowserHarnessArtifactSHA256 != "" || r.BrowserHarnessArtifactOffset != 0 || r.BrowserHarnessArtifactNext != 0 || r.BrowserHarnessArtifactEOF || r.BrowserHarnessArtifactDataBase64 != "" || r.BrowserHarnessCleanupComplete || r.BrowserHarnessCleanupRuns != 0 || r.BrowserHarnessCleanupArtifacts != 0 || r.BrowserHarnessCleanupProfiles != 0
}

func validProjectBrowserHarnessResultForKind(kind OperationKind, r OperationResult) bool {
	metadata := r
	clearProjectBrowserHarnessResult(&metadata)
	if !validProjectOperationResult(metadata) {
		return false
	}
	switch kind {
	case OperationProjectBrowserHarnessStart, OperationProjectBrowserHarnessStatus, OperationProjectBrowserHarnessStop:
		return validBrowserHarnessSnapshot(r) && len(r.BrowserHarnessRuns) == 0 && len(r.BrowserHarnessArtifacts) == 0 && !r.BrowserHarnessListComplete && !r.BrowserHarnessArtifactsComplete && !r.BrowserHarnessCleanupComplete && r.BrowserHarnessArtifactPath == ""
	case OperationProjectBrowserHarnessList:
		if !r.BrowserHarnessListComplete || len(r.BrowserHarnessRuns) > MaxBrowserHarnessRuns {
			return false
		}
		for _, run := range r.BrowserHarnessRuns {
			if !validBrowserHarnessSummary(run) {
				return false
			}
		}
		return r.BrowserHarnessRunID == "" && len(r.BrowserHarnessArtifacts) == 0
	case OperationProjectBrowserHarnessArtifactList:
		if !browserHarnessRunIDPattern.MatchString(r.BrowserHarnessRunID) || !r.BrowserHarnessArtifactsComplete || len(r.BrowserHarnessArtifacts) > MaxBrowserHarnessArtifacts {
			return false
		}
		for _, artifact := range r.BrowserHarnessArtifacts {
			if !validBrowserHarnessArtifactSummary(artifact) {
				return false
			}
		}
		return !r.BrowserHarnessListComplete && r.BrowserHarnessArtifactPath == ""
	case OperationProjectBrowserHarnessArtifactRead:
		if !browserHarnessRunIDPattern.MatchString(r.BrowserHarnessRunID) || !validBrowserHarnessArtifactPath(r.BrowserHarnessArtifactPath) || r.BrowserHarnessArtifactMediaType == "" || r.BrowserHarnessArtifactBytes < 0 || !browserHarnessSHA256Pattern.MatchString(r.BrowserHarnessArtifactSHA256) || r.BrowserHarnessArtifactOffset < 0 || r.BrowserHarnessArtifactNext < r.BrowserHarnessArtifactOffset || r.BrowserHarnessArtifactNext > r.BrowserHarnessArtifactBytes {
			return false
		}
		decoded, err := base64.StdEncoding.DecodeString(r.BrowserHarnessArtifactDataBase64)
		return err == nil && len(decoded) <= MaxBrowserHarnessArtifactChunk && int64(len(decoded)) == r.BrowserHarnessArtifactNext-r.BrowserHarnessArtifactOffset && r.BrowserHarnessArtifactEOF == (r.BrowserHarnessArtifactNext == r.BrowserHarnessArtifactBytes)
	case OperationProjectBrowserHarnessCleanup:
		return r.BrowserHarnessCleanupComplete && r.BrowserHarnessCleanupRuns >= 0 && r.BrowserHarnessCleanupArtifacts >= 0 && r.BrowserHarnessCleanupProfiles >= 0 && r.BrowserHarnessRunID == ""
	default:
		return false
	}
}

func validBrowserHarnessSnapshot(r OperationResult) bool {
	if !browserHarnessRunIDPattern.MatchString(r.BrowserHarnessRunID) || !browserHarnessStateValidEdge(r.BrowserHarnessState) || !browserHarnessProfilePattern.MatchString(r.BrowserHarnessProfile) || r.BrowserHarnessTimeoutSeconds < 1 || r.BrowserHarnessTimeoutSeconds > 604800 || r.BrowserHarnessStorageMiB < 64 || r.BrowserHarnessStorageMiB > 65536 || r.BrowserHarnessExitCode < 0 || r.BrowserHarnessExitCode > 255 || len(r.BrowserHarnessStdout) > MaxBrowserHarnessLogBytes || len(r.BrowserHarnessStderr) > MaxBrowserHarnessLogBytes || !utf8.ValidString(r.BrowserHarnessStdout) || !utf8.ValidString(r.BrowserHarnessStderr) || strings.ContainsRune(r.BrowserHarnessStdout+r.BrowserHarnessStderr, 0) || r.BrowserHarnessStdoutNext < 0 || r.BrowserHarnessStderrNext < 0 || r.BrowserHarnessArtifactCount < 0 || r.BrowserHarnessArtifactBytes < 0 {
		return false
	}
	created, e1 := time.Parse(time.RFC3339Nano, r.BrowserHarnessCreatedAt)
	updated, e2 := time.Parse(time.RFC3339Nano, r.BrowserHarnessUpdatedAt)
	if e1 != nil || e2 != nil || updated.Before(created) {
		return false
	}
	if r.BrowserHarnessStartedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, r.BrowserHarnessStartedAt); err != nil {
			return false
		}
	}
	if r.BrowserHarnessFinishedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, r.BrowserHarnessFinishedAt); err != nil {
			return false
		}
	}
	return r.BrowserHarnessExitKnown || r.BrowserHarnessExitCode == 0
}

func validBrowserHarnessSummary(r BrowserHarnessSummary) bool {
	if !browserHarnessRunIDPattern.MatchString(r.RunID) || !browserHarnessStateValidEdge(r.State) || !browserHarnessProfilePattern.MatchString(r.Profile) || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 604800 || r.StorageMiB < 64 || r.StorageMiB > 65536 || r.ExitCode < 0 || r.ExitCode > 255 {
		return false
	}
	created, e1 := time.Parse(time.RFC3339Nano, r.CreatedAt)
	updated, e2 := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	return e1 == nil && e2 == nil && !updated.Before(created) && (r.ExitKnown || r.ExitCode == 0)
}

func validBrowserHarnessArtifactSummary(a BrowserHarnessArtifactSummary) bool {
	if !validBrowserHarnessArtifactPath(a.Path) || a.MediaType == "" || a.Bytes < 0 || !browserHarnessSHA256Pattern.MatchString(a.SHA256) {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, a.UpdatedAt)
	return err == nil
}

func browserHarnessStateValidEdge(value string) bool {
	switch value {
	case "starting", "running", "exited", "failed", "stopped", "timed_out", "storage_exceeded", "indeterminate":
		return true
	}
	return false
}

func clearProjectBrowserHarnessResult(r *OperationResult) {
	r.BrowserHarnessRunID = ""
	r.BrowserHarnessState = ""
	r.BrowserHarnessProfile = ""
	r.BrowserHarnessCreatedAt = ""
	r.BrowserHarnessUpdatedAt = ""
	r.BrowserHarnessStartedAt = ""
	r.BrowserHarnessFinishedAt = ""
	r.BrowserHarnessExitKnown = false
	r.BrowserHarnessExitCode = 0
	r.BrowserHarnessTimeoutSeconds = 0
	r.BrowserHarnessStorageMiB = 0
	r.BrowserHarnessStdout = ""
	r.BrowserHarnessStderr = ""
	r.BrowserHarnessStdoutNext = 0
	r.BrowserHarnessStderrNext = 0
	r.BrowserHarnessStdoutEOF = false
	r.BrowserHarnessStderrEOF = false
	r.BrowserHarnessStdoutTruncated = false
	r.BrowserHarnessStderrTruncated = false
	r.BrowserHarnessArtifactCount = 0
	r.BrowserHarnessArtifactBytes = 0
	r.BrowserHarnessRuns = nil
	r.BrowserHarnessListComplete = false
	r.BrowserHarnessArtifacts = nil
	r.BrowserHarnessArtifactsComplete = false
	r.BrowserHarnessArtifactPath = ""
	r.BrowserHarnessArtifactMediaType = ""
	r.BrowserHarnessArtifactSHA256 = ""
	r.BrowserHarnessArtifactOffset = 0
	r.BrowserHarnessArtifactNext = 0
	r.BrowserHarnessArtifactEOF = false
	r.BrowserHarnessArtifactDataBase64 = ""
	r.BrowserHarnessCleanupComplete = false
	r.BrowserHarnessCleanupRuns = 0
	r.BrowserHarnessCleanupArtifacts = 0
	r.BrowserHarnessCleanupProfiles = 0
}

var _ = url.URL{}
