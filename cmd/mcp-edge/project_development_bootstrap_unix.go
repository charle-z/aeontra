//go:build !windows

package main

import (
	"context"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectDevelopmentBootstrap(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	binding := operation.Request.DevelopmentBootstrap
	resolvedSelection := operation.Kind == edge.OperationProjectDevelopmentBootstrapStart
	if binding == nil || !binding.Valid(resolvedSelection) || operation.DeviceID != binding.Anchor.DeviceID {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	var processOperation edge.Operation
	if resolvedSelection {
		if processes == nil {
			return edge.OperationResult{}, "project_process_unavailable"
		}
		recipe, err := edgeclient.BuildDevelopmentBootstrapRecipe(*binding.Resolution)
		if err != nil {
			return edge.OperationResult{}, "project_development_resolution_invalid"
		}
		processOperation = operation
		processOperation.Kind = edge.OperationProjectProcessStart
		processOperation.Request.Argv, processOperation.Request.Environment = recipe.Argv, recipe.Environment
		result, recovered, code := recoverDevelopmentProcess(processes, processOperation, binding.Anchor)
		if code != "" {
			return edge.OperationResult{}, code
		}
		if recovered {
			return developmentBootstrapProcessResult(result, binding), ""
		}
	}
	// The registry attests repository authority; bootstrap does not inspect
	// source cleanliness or need the requested toolchain to exist already.
	_, workspaces, projects, roots, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	var resolved edgeclient.ProjectResolution
	var err error
	if resolvedSelection {
		// New effects inspect the owner-bound checkout, but retain this exact
		// captured resolution through spawn; never resolve an alias a second time.
		resolved, err = projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	} else {
		resolved, err = projects.ResolveRegistered(operation.Request.Alias, operation.Request.TargetAlias)
	}
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	if resolved.Workspace.ID != binding.Anchor.WorkspaceID || !resolved.Project.ClaimGenerationValid ||
		resolved.Project.ClaimGeneration != binding.Anchor.Generation || resolved.Project.Owner != binding.Anchor.Owner || resolved.Project.Repository != binding.Anchor.Repository {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	if !resolvedSelection {
		selection, err := edgeclient.ResolveDevelopmentBootstrap(ctx, binding.CapabilityID)
		if err != nil {
			return edge.OperationResult{}, "project_development_resolution_unavailable"
		}
		digest, err := development.BootstrapResolutionDigest(selection)
		if err != nil {
			return edge.OperationResult{}, "project_development_resolution_invalid"
		}
		copy := *binding
		copy.Resolution, copy.ResolutionDigest = &selection, digest
		return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias,
			ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias,
			ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
			DevelopmentBootstrap: &copy}, ""
	}
	// The exact resolution was persisted in the durable operation before this
	// effect. Replay uses the process manager's original idempotency key.
	result, code := startResolvedDevelopmentProcess(ctx, processes, processOperation, resolved, roots, binding.Anchor)
	if code != "" {
		return edge.OperationResult{}, code
	}
	return developmentBootstrapProcessResult(result, binding), ""
}

func developmentBootstrapProcessResult(result edge.OperationResult, binding *edge.ProjectDevelopmentBootstrapBinding) edge.OperationResult {
	copy := *binding
	selection := *binding.Resolution
	copy.Resolution = &selection
	result.DevelopmentBootstrap = &copy
	return result
}
