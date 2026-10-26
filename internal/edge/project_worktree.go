package edge

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var (
	projectWorktreeIDPattern     = regexp.MustCompile(`^wt_[a-f0-9]{32}$`)
	projectWorkJobIDPattern      = regexp.MustCompile(`^wj_[a-f0-9]{32}$`)
	projectWorkLeaseIDPattern    = regexp.MustCompile(`^wl_[a-f0-9]{32}$`)
	projectWorktreeCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	projectWorktreeBranchPattern = regexp.MustCompile(`^codex/worktree-[a-f0-9]{32}$`)
)

func normalizeProjectWorktreeRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.WorktreeID = strings.TrimSpace(request.WorktreeID)
	request.WorktreeBaseCommit = strings.ToLower(strings.TrimSpace(request.WorktreeBaseCommit))
	request.WorktreeRole = strings.TrimSpace(request.WorktreeRole)
	request.WorkJobID = strings.TrimSpace(request.WorkJobID)
	request.WorkLeaseID = strings.TrimSpace(request.WorkLeaseID)
	if !projectOperationAliasPattern.MatchString(request.Alias) || !projectOperationTargetPattern.MatchString(request.TargetAlias) || request.Profile != "linux-workcell" {
		return OperationRequest{}, errors.New("project worktree request is invalid")
	}
	base := OperationRequest{Alias: request.Alias, TargetAlias: request.TargetAlias, Profile: request.Profile}
	switch kind {
	case OperationProjectWorktreeCreate:
		if !projectWorktreeCommitPattern.MatchString(request.WorktreeBaseCommit) || (request.WorktreeRole != "reader" && request.WorktreeRole != "writer" && request.WorktreeRole != "direct") ||
			!projectWorkJobIDPattern.MatchString(request.WorkJobID) || !projectWorkLeaseIDPattern.MatchString(request.WorkLeaseID) || request.WorkFence == 0 ||
			len(request.IdempotencyKey) < 8 || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project worktree create request is invalid")
		}
		base.IdempotencyKey, base.WorktreeBaseCommit, base.WorktreeRole = request.IdempotencyKey, request.WorktreeBaseCommit, request.WorktreeRole
		base.WorkJobID, base.WorkLeaseID, base.WorkFence = request.WorkJobID, request.WorkLeaseID, request.WorkFence
	case OperationProjectWorktreeClaim:
		if !projectWorktreeIDPattern.MatchString(request.WorktreeID) || !projectWorkJobIDPattern.MatchString(request.WorkJobID) || !projectWorkLeaseIDPattern.MatchString(request.WorkLeaseID) || request.WorkFence == 0 {
			return OperationRequest{}, errors.New("project worktree claim request is invalid")
		}
		base.WorktreeID, base.WorkJobID, base.WorkLeaseID, base.WorkFence = request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence
	case OperationProjectWorktreeStatus:
		if !projectWorktreeIDPattern.MatchString(request.WorktreeID) || !validWorktreeAncestorCommits(request.WorktreeAncestorCommits) {
			return OperationRequest{}, errors.New("project worktree status request is invalid")
		}
		base.WorktreeID = request.WorktreeID
		if request.WorktreeRole == "direct" {
			base.WorktreeRole = "direct"
		}
		base.WorktreeAncestorCommits = request.WorktreeAncestorCommits
	case OperationProjectWorktreeList:
		if request.WorktreeLimit < 1 || request.WorktreeLimit > 100 {
			return OperationRequest{}, errors.New("project worktree list request is invalid")
		}
		base.WorktreeLimit = request.WorktreeLimit
		if request.WorktreeRole == "direct" {
			base.WorktreeRole = "direct"
		}
	case OperationProjectWorktreeCleanup:
		if !projectWorktreeIDPattern.MatchString(request.WorktreeID) || !projectWorkJobIDPattern.MatchString(request.WorkJobID) || !projectWorkLeaseIDPattern.MatchString(request.WorkLeaseID) || request.WorkFence == 0 ||
			len(request.IdempotencyKey) < 8 || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project worktree cleanup request is invalid")
		}
		base.WorktreeID, base.WorkJobID, base.WorkLeaseID, base.WorkFence, base.IdempotencyKey = request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence, request.IdempotencyKey
		if request.WorktreeRole == "direct" && projectWorktreeCommitPattern.MatchString(request.WorktreeBaseCommit) {
			base.WorktreeRole, base.WorktreeBaseCommit = request.WorktreeRole, request.WorktreeBaseCommit
		}
	default:
		return OperationRequest{}, errors.New("project worktree operation is invalid")
	}
	if !reflect.DeepEqual(request, base) {
		return OperationRequest{}, errors.New("project worktree request contains unrelated fields")
	}
	return request, nil
}

func emptyProjectWorktreeRequestFields(request OperationRequest) bool {
	return len(request.WorktreeAncestorCommits) == 0 && request.WorktreeID == "" && request.WorktreeBaseCommit == "" && request.WorktreeRole == "" && request.WorkJobID == "" && request.WorkLeaseID == "" && request.WorkFence == 0 && request.WorktreeLimit == 0 && request.TestProfileID == "" && request.TestProfileDigest == ""
}

func hasProjectWorktreeResult(result OperationResult) bool {
	return result.WorkspaceStorage != nil || len(result.WorktreeAncestorCommits) != 0 || result.WorktreeAncestorsVerified || result.WorktreeID != "" || result.WorktreeState != "" || result.WorktreeRole != "" || result.WorktreeBaseCommit != "" || result.WorktreeBranch != "" ||
		result.WorktreeEvidenceKnown || result.WorktreeHeadCommit != "" || result.WorktreeClean || result.WorktreeCommitsAheadBase != 0 || result.WorktreeChangedPathCount != 0 ||
		result.WorkJobID != "" || result.WorkLeaseID != "" || result.WorkFence != 0 || result.WorktreeCreatedAt != "" || result.WorktreeUpdatedAt != "" || len(result.Worktrees) != 0
}

func validProjectWorktreeResultForKind(kind OperationKind, result OperationResult) bool {
	if result.WorkspaceStorage != nil {
		v := result.WorkspaceStorage
		if kind != OperationProjectWorktreeStatus || v.RuntimeBytes < 0 || v.CacheBytes < 0 || v.ArtifactBytes < 0 || v.ScratchBytes < 0 || v.ScratchBytes > v.RuntimeBytes || v.ScratchBudgetBytes < 0 || v.ScannedEntries < 0 || v.ScannedEntries > 10000 ||
			(v.RetentionState != "unreferenced" && v.RetentionState != "retained" && v.RetentionState != "unknown") {
			return false
		}
		if v.ScratchBudgetState != "unconfigured" && v.ScratchBudgetState != "normal" && v.ScratchBudgetState != "over_budget" && v.ScratchBudgetState != "unknown" && v.ScratchBudgetState != "invalid" {
			return false
		}
	}
	if !validWorktreeAncestorCommits(result.WorktreeAncestorCommits) || (len(result.WorktreeAncestorCommits) != 0) != result.WorktreeAncestorsVerified ||
		(kind != OperationProjectWorktreeStatus && (result.WorktreeAncestorsVerified || len(result.WorktreeAncestorCommits) != 0)) {
		return false
	}
	if kind == OperationProjectWorktreeList {
		if len(result.Worktrees) > 100 || result.WorktreeID != "" || result.WorktreeState != "" || result.WorktreeRole != "" || result.WorktreeBaseCommit != "" || result.WorktreeBranch != "" || result.WorkJobID != "" || result.WorkLeaseID != "" || result.WorkFence != 0 {
			return false
		}
		for _, item := range result.Worktrees {
			if !validProjectWorktreeSummary(item) {
				return false
			}
		}
		metadata := result
		metadata.Worktrees = nil
		return validProjectOperationResult(metadata)
	}
	if kind != OperationProjectWorktreeCreate && kind != OperationProjectWorktreeClaim && kind != OperationProjectWorktreeStatus && kind != OperationProjectWorktreeCleanup {
		return false
	}
	if !validProjectWorktreeSummary(ProjectWorktreeSummary{
		WorktreeID: result.WorktreeID, WorkspaceID: result.WorkspaceID, State: result.WorktreeState, Role: result.WorktreeRole,
		BaseCommit: result.WorktreeBaseCommit, Branch: result.WorktreeBranch, JobID: result.WorkJobID, LeaseID: result.WorkLeaseID, Fence: result.WorkFence,
		CreatedAt: result.WorktreeCreatedAt, UpdatedAt: result.WorktreeUpdatedAt,
	}) {
		return false
	}
	if kind == OperationProjectWorktreeCleanup && result.WorktreeState != "removed" {
		return false
	}
	if kind != OperationProjectWorktreeCleanup && kind != OperationProjectWorktreeStatus && result.WorktreeState != "ready" {
		return false
	}
	hasEvidence := result.WorktreeEvidenceKnown || result.WorktreeHeadCommit != "" || result.WorktreeClean || result.WorktreeCommitsAheadBase != 0 || result.WorktreeChangedPathCount != 0
	if kind == OperationProjectWorktreeStatus && result.WorktreeState == "ready" {
		if !result.WorktreeEvidenceKnown || !projectWorktreeCommitPattern.MatchString(result.WorktreeHeadCommit) ||
			result.WorktreeCommitsAheadBase < 0 || result.WorktreeCommitsAheadBase > 10000 || result.WorktreeChangedPathCount < 0 || result.WorktreeChangedPathCount > 10000 ||
			((result.WorktreeHeadCommit == result.WorktreeBaseCommit) != (result.WorktreeCommitsAheadBase == 0)) {
			return false
		}
	} else if hasEvidence {
		return false
	}
	metadata := result
	metadata.WorkspaceStorage = nil
	metadata.WorktreeID, metadata.WorktreeState, metadata.WorktreeRole, metadata.WorktreeBaseCommit, metadata.WorktreeBranch = "", "", "", "", ""
	metadata.WorktreeEvidenceKnown, metadata.WorktreeHeadCommit, metadata.WorktreeClean = false, "", false
	metadata.WorktreeAncestorCommits, metadata.WorktreeAncestorsVerified = nil, false
	metadata.WorktreeCommitsAheadBase, metadata.WorktreeChangedPathCount = 0, 0
	metadata.WorkJobID, metadata.WorkLeaseID, metadata.WorkFence = "", "", 0
	metadata.WorktreeCreatedAt, metadata.WorktreeUpdatedAt = "", ""
	return validProjectOperationResult(metadata)
}

func validWorktreeAncestorCommits(commits []string) bool {
	if len(commits) > 4 {
		return false
	}
	seen := make(map[string]bool)
	for _, commit := range commits {
		if !projectWorktreeCommitPattern.MatchString(commit) || seen[commit] {
			return false
		}
		seen[commit] = true
	}
	return true
}

func validProjectWorktreeSummary(item ProjectWorktreeSummary) bool {
	if !projectWorktreeIDPattern.MatchString(item.WorktreeID) || !workspaceIDPattern.MatchString(item.WorkspaceID) ||
		(item.State != "ready" && item.State != "removed") || (item.Role != "reader" && item.Role != "writer") ||
		!projectWorktreeCommitPattern.MatchString(item.BaseCommit) || !projectWorktreeBranchPattern.MatchString(item.Branch) ||
		!projectWorkJobIDPattern.MatchString(item.JobID) || !projectWorkLeaseIDPattern.MatchString(item.LeaseID) || item.Fence == 0 {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
	if err != nil || created.IsZero() {
		return false
	}
	updated, err := time.Parse(time.RFC3339Nano, item.UpdatedAt)
	return err == nil && !updated.Before(created)
}
