//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProjectProcessRecoveryRequest binds recovery to the original operation and
// captured workspace without reading paths, source, or mutable registry state.
type ProjectProcessRecoveryRequest struct {
	OperationID, IdempotencyKey, ProjectAlias, TargetAlias string
	ProjectOwner, ProjectRepository                        string
	ProjectClaimGeneration                                 uint64
	WorkspaceID                                            string
	Argv                                                   []string
	CWD, Stdin                                             string
	Environment                                            map[string]string
	DevelopmentBindingDigest                               string
}

// RecoverySnapshotByIdempotency observes one already authorized effect. It
// never opens a workspace, resolves an alias, prepares a workcell, or spawns.
// Source, registry and runtime changes cannot replace the captured binding.
func (manager *ProjectProcessManager) RecoverySnapshotByIdempotency(request ProjectProcessRecoveryRequest) (ProjectProcessSnapshot, bool, error) {
	if manager == nil || manager.db == nil || !directWorkcellOperationIDPattern.MatchString(request.OperationID) ||
		!projectProcessIdempotencyPattern.MatchString(request.IdempotencyKey) || !projectAliasPattern.MatchString(request.ProjectAlias) ||
		!projectTargetPattern.MatchString(request.TargetAlias) || !workspaceIDPattern.MatchString(request.WorkspaceID) ||
		!githubOwnerPattern.MatchString(request.ProjectOwner) || !devGitSimplePattern.MatchString(request.ProjectRepository) ||
		request.ProjectClaimGeneration == 0 || request.ProjectClaimGeneration > maxProjectClaimGeneration ||
		!projectWorktreeTestDigestRE.MatchString(request.DevelopmentBindingDigest) {
		return ProjectProcessSnapshot{}, false, errors.New("project process recovery request is invalid")
	}
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	record, err := manager.recordByIdempotency(request.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectProcessSnapshot{}, false, nil
	}
	if err != nil {
		return ProjectProcessSnapshot{}, false, errors.New("project process recovery journal unavailable")
	}
	if record.OperationID != request.OperationID || record.WorkspaceID != request.WorkspaceID ||
		record.ProjectAlias != request.ProjectAlias || record.TargetAlias != request.TargetAlias ||
		record.ProjectOwner != request.ProjectOwner || record.ProjectRepository != request.ProjectRepository ||
		record.ProjectClaimGeneration != request.ProjectClaimGeneration ||
		record.ProjectProfile != string(WorkspaceProfileLinuxWorkcell) || record.ProjectMode != string(WorkspaceModeDev) {
		return ProjectProcessSnapshot{}, false, ErrProjectProcessIdempotencyConflict
	}
	expected := ProjectProcessStartRequest{
		Workspace: Workspace{ID: request.WorkspaceID}, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias,
		ProjectOwner: request.ProjectOwner, ProjectRepository: request.ProjectRepository, ProjectClaimGeneration: request.ProjectClaimGeneration,
		// This is recorded metadata, not mutable source or registry authority.
		ProjectState: record.ProjectState, Argv: request.Argv, CWD: request.CWD, Stdin: request.Stdin, Environment: request.Environment,
		DevelopmentBindingDigest: request.DevelopmentBindingDigest,
	}
	digest, err := projectProcessRequestDigest(expected)
	if err != nil || digest != record.RequestDigest || projectProcessRequestContainsSecret(expected) {
		return ProjectProcessSnapshot{}, false, ErrProjectProcessIdempotencyConflict
	}
	if !projectProcessTerminal(record.State) {
		alive, aliveErr := manager.platform.Alive(record.Identity)
		if errors.Is(aliveErr, ErrProjectProcessIdentityChanged) || errors.Is(aliveErr, ErrProjectProcessGroupMissing) {
			if err := manager.finishFailed(record.ProcessID, "process_identity_changed"); err != nil {
				return ProjectProcessSnapshot{}, false, errors.New("project process recovery journal unavailable")
			}
		} else if aliveErr != nil {
			return ProjectProcessSnapshot{}, false, errors.New("project process recovery liveness unavailable")
		} else if !alive {
			// A local watcher may still be committing the exact terminal receipt.
			// Never turn missing liveness into a known zero exit or issue signals.
			if terminal, found := manager.waitTerminal(context.Background(), record.ProcessID, 50*time.Millisecond); found {
				record = terminal
			} else if exit, err := readProjectProcessWorkerExit(manager.workerRoot, record.ProcessID); err == nil {
				if err := manager.finishRecoveredExit(record, exit); err != nil {
					return ProjectProcessSnapshot{}, false, errors.New("project process recovery journal unavailable")
				}
			} else if err := manager.finishFailed(record.ProcessID, "process_lost"); err != nil {
				return ProjectProcessSnapshot{}, false, errors.New("project process recovery journal unavailable")
			}
		}
		record, err = manager.recordByID(record.ProcessID)
		if err != nil {
			return ProjectProcessSnapshot{}, false, errors.New("project process recovery journal unavailable")
		}
	}
	return manager.snapshot(record), true, nil
}
