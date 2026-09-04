package main

import (
	"context"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

// executeProjectRegistryRecovery is deliberately registry-only. It never
// scans arbitrary filesystem paths and never removes a workspace or its
// source tree. The owner comes from the Edge's configured GitHub authority;
// the client can only restate a repository name and an exact claim generation.
type projectControlStateOpener func(string) (edgeclient.GitHubCredential, *edgeclient.WorkspaceRegistry, *edgeclient.ProjectRegistry, edgeclient.WorkspaceRoots, string)

func executeProjectRegistryRecovery(ctx context.Context, stateRoot string, operation edge.Operation, openState projectControlStateOpener, safeFailure func(error) string) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()

	switch operation.Kind {
	case edge.OperationProjectRegistryList:
		claims, err := projects.ListClaimsContext(ctx, operation.Request.TargetAlias)
		if err != nil {
			return edge.OperationResult{}, safeFailure(err)
		}
		return projectRegistryClaimsResult("listed", claims), ""
	case edge.OperationProjectReconcile:
		// Reconcile only persists state derived from the currently registered
		// workspace binding and its boundary attestation. It does not reclaim,
		// delete, or associate anything.
		claim, err := projects.ReconcileClaim(ctx, operation.Request.Alias, operation.Request.TargetAlias)
		if err != nil {
			return edge.OperationResult{}, safeFailure(err)
		}
		return projectRegistryClaimsResult("reconciled", []edgeclient.ProjectClaim{claim}), ""
	case edge.OperationProjectRelease:
		claims, err := projects.ListClaimsContext(ctx, operation.Request.TargetAlias)
		if err != nil {
			return edge.OperationResult{}, safeFailure(err)
		}
		request := operation.Request
		var selected *edgeclient.ProjectClaim
		for index := range claims {
			claim := claims[index]
			if claim.Alias == request.Alias && claim.Target == request.TargetAlias {
				selected = &claim
				break
			}
		}
		if selected == nil {
			return edge.OperationResult{}, "project_project_not_found"
		}
		if selected.Owner != credential.Owner || selected.Repository != request.Repository || selected.Generation != request.ProjectClaimGeneration {
			return edge.OperationResult{}, "project_plan_changed"
		}
		if err := projects.ReleaseClaim(request.Alias, credential.Owner, request.Repository, request.TargetAlias, request.ProjectClaimGeneration); err != nil {
			return edge.OperationResult{}, safeFailure(err)
		}
		return edge.OperationResult{
			ProjectRegistryAction:  "released",
			ProjectAlias:           selected.Alias,
			ProjectOwner:           selected.Owner,
			ProjectRepository:      selected.Repository,
			ProjectTarget:          selected.Target,
			ProjectState:           "released",
			ProjectClaimGeneration: selected.Generation,
		}, ""
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

func projectRegistryClaimsResult(action string, claims []edgeclient.ProjectClaim) edge.OperationResult {
	result := edge.OperationResult{ProjectRegistryAction: action, ProjectClaims: make([]edge.ProjectClaimSummary, 0, len(claims))}
	for _, claim := range claims {
		result.ProjectClaims = append(result.ProjectClaims, edge.ProjectClaimSummary{
			Alias: claim.Alias, Owner: claim.Owner, Repository: claim.Repository,
			Target: claim.Target, WorkspaceID: claim.WorkspaceID, Generation: claim.Generation,
			State: string(claim.State), Reason: string(claim.Reason), Repairable: claim.Repairable,
		})
	}
	return result
}
