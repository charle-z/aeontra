package edge

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var projectGitPlanPattern = regexp.MustCompile(`^gp_[a-f0-9]{32}$`)

func normalizeProjectGitSyncRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if !emptyProjectExecRequestFields(request) || !emptyProjectProcessRequestFields(request) {
		return OperationRequest{}, errors.New("project Git sync request is invalid")
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.GitPlanID = strings.TrimSpace(request.GitPlanID)
	if !validProjectOperationRequestCommon(request) || request.Repository != "" {
		return OperationRequest{}, errors.New("project Git sync request is invalid")
	}
	switch kind {
	case OperationProjectGitStatus:
		if request.IdempotencyKey != "" || request.GitPlanID != "" {
			return OperationRequest{}, errors.New("project Git status request is invalid")
		}
	case OperationProjectGitFetch, OperationProjectGitFastForwardPreview, OperationProjectGitPublishPreview:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || request.GitPlanID != "" {
			return OperationRequest{}, errors.New("project Git sync request is invalid")
		}
	case OperationProjectGitFastForward, OperationProjectGitPublish:
		if !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) || !projectGitPlanPattern.MatchString(request.GitPlanID) {
			return OperationRequest{}, errors.New("project Git execution request is invalid")
		}
	default:
		return OperationRequest{}, errors.New("project Git sync kind is invalid")
	}
	return request, nil
}

func hasProjectGitSyncResult(result OperationResult) bool {
	return result.GitBranch != "" || result.GitHead != "" || result.GitRemoteHead != "" || result.GitAhead != 0 || result.GitBehind != 0 ||
		result.GitDiverged || result.GitDetached || result.GitDirty || result.GitClean || result.GitFetched || result.GitFastForwarded || result.GitPublished || result.GitPlanExpiresAt != ""
}

func validProjectGitSyncResult(result OperationResult) bool {
	attached := projectSnapshotBranchPattern.MatchString(result.GitBranch) && !result.GitDetached
	detached := result.GitBranch == "" && result.GitDetached && result.GitRemoteHead == "" && !result.GitFetched && result.GitAhead == 0 && result.GitBehind == 0 && !result.GitDiverged
	if (!attached && !detached) || !projectSnapshotCommitPattern.MatchString(result.GitHead) ||
		(result.GitRemoteHead != "" && !projectSnapshotCommitPattern.MatchString(result.GitRemoteHead)) || result.GitAhead < 0 || result.GitBehind < 0 ||
		result.GitDirty == result.GitClean || (result.GitDiverged && (result.GitAhead == 0 || result.GitBehind == 0)) ||
		(result.GitFetched && result.GitRemoteHead == "") || (result.GitFastForwarded && (!result.GitClean || !result.GitFetched || result.GitHead != result.GitRemoteHead)) ||
		(result.GitPublished && (!result.GitClean || result.GitDetached || result.GitHead != result.GitRemoteHead)) ||
		((result.GitPlanExpiresAt == "") != (result.GitPlanID == "")) || (result.GitPlanExpiresAt != "" && (!projectGitPlanPattern.MatchString(result.GitPlanID) || !validProjectGitPlanTime(result.GitPlanExpiresAt))) {
		return false
	}
	metadata := result
	metadata.GitBranch, metadata.GitHead, metadata.GitRemoteHead = "", "", ""
	metadata.GitAhead, metadata.GitBehind = 0, 0
	metadata.GitDiverged, metadata.GitDetached, metadata.GitDirty, metadata.GitClean = false, false, false, false
	metadata.GitFetched, metadata.GitFastForwarded, metadata.GitPublished, metadata.GitPlanID, metadata.GitPlanExpiresAt = false, false, false, "", ""
	return validProjectOperationResult(metadata)
}

func validProjectGitSyncResultForKind(kind OperationKind, result OperationResult) bool {
	if !validProjectGitSyncResult(result) {
		return false
	}
	hasPlan := result.GitPlanID != ""
	switch kind {
	case OperationProjectGitStatus:
		return !hasPlan && !result.GitFastForwarded && !result.GitPublished
	case OperationProjectGitFetch:
		return !hasPlan && !result.GitDetached && result.GitFetched && !result.GitFastForwarded && !result.GitPublished
	case OperationProjectGitFastForwardPreview:
		return hasPlan && !result.GitDetached && result.GitClean && result.GitFetched && !result.GitDiverged && result.GitAhead == 0 && !result.GitFastForwarded && !result.GitPublished
	case OperationProjectGitFastForward:
		return !hasPlan && !result.GitDetached && result.GitClean && result.GitFetched && !result.GitDiverged && result.GitAhead == 0 && result.GitBehind == 0 && result.GitHead == result.GitRemoteHead && !result.GitPublished
	case OperationProjectGitPublishPreview:
		return hasPlan && !result.GitDetached && result.GitClean && !result.GitDiverged && result.GitBehind == 0 && !result.GitFastForwarded && !result.GitPublished
	case OperationProjectGitPublish:
		return !hasPlan && !result.GitDetached && result.GitClean && result.GitHead == result.GitRemoteHead && result.GitAhead == 0 && result.GitBehind == 0 && !result.GitDiverged && !result.GitFastForwarded && result.GitPublished
	default:
		return false
	}
}

func validProjectGitPlanTime(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}
