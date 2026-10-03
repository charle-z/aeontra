//go:build !windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectDevelopmentInspect(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	inspection, code := inspectProjectDevelopment(ctx, stateRoot, operation)
	if code != "" {
		return edge.OperationResult{}, code
	}
	return inspection.result, ""
}

type projectDevelopmentInspectionContext struct {
	result   edge.OperationResult
	resolved edgeclient.ProjectResolution
	roots    edgeclient.WorkspaceRoots
}

func projectDevelopmentGitRunner(stateRoot string) edgeclient.DevGitCommandRunner {
	return edgeclient.NewRegisteredProjectSourceGitRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin")
}

func inspectProjectDevelopment(ctx context.Context, stateRoot string, operation edge.Operation) (*projectDevelopmentInspectionContext, string) {
	request := operation.Request
	_, workspaces, projects, roots, code := openProjectControlState(stateRoot)
	if code != "" {
		return nil, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, request.Alias, request.TargetAlias)
	if err != nil {
		return nil, safeProjectControlFailure(err)
	}
	if !resolved.Project.ClaimGenerationValid {
		return nil, "project_development_identity_unavailable"
	}
	readiness, err := edgeclient.DetectToolchainReadiness(resolved.Workspace.Path)
	if err != nil {
		return nil, "project_toolchain_manifest_invalid"
	}
	requirements, err := edgeclient.DevelopmentRequirementsFromToolchainReadiness(readiness)
	if err != nil {
		return nil, "project_development_requirements_unresolved"
	}
	inventory, err := edgeclient.CollectDevelopmentWorkcellInventory(ctx, edgeclient.DirectWorkcellCommandRequest{
		OperationID: operation.ID, Workspace: resolved.Workspace, StateRoot: stateRoot, WorkspaceRoots: roots,
	})
	if err != nil {
		return nil, "project_development_inventory_unavailable"
	}
	sourceDigest, sourceHead, sourceClean, err := edgeclient.RegisteredProjectSourceEvidence(ctx, projects, resolved, projectDevelopmentGitRunner(stateRoot))
	if err != nil {
		return nil, "project_development_source_unavailable"
	}
	preparation := edgeclient.LinuxWorkcellPreparation{Workspace: resolved.Workspace}
	workcell, err := edgeclient.DevelopmentWorkcellAttestation("workcell:"+resolved.Workspace.ID, resolved.Project.ClaimGeneration, preparation, inventory)
	if err != nil {
		return nil, "project_development_attestation_invalid"
	}
	record, err := workcell.Record()
	if err != nil {
		return nil, "project_development_attestation_invalid"
	}
	inspection := &edge.ProjectDevelopmentInspection{Version: 1, ProjectGeneration: resolved.Project.ClaimGeneration, SourceDigest: sourceDigest,
		SourceHead: sourceHead, SourceClean: sourceClean, SourceEvidenceKnown: true,
		Requirements: make([]development.CapabilityID, 0, len(requirements)), Environments: []development.EnvironmentRecord{record}}
	for _, requirement := range requirements {
		inspection.Requirements = append(inspection.Requirements, requirement.ID)
	}
	// A toolbox is deliberately not inferred from a socket or a base-image
	// name. Its live manager supplies a separate identity-checked attestation.
	result := edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias,
		ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias,
		ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		DevelopmentInspection: inspection}
	return &projectDevelopmentInspectionContext{result: result, resolved: resolved, roots: roots}, ""
}

func developmentCommandProcessOperation(operation edge.Operation) edge.Operation {
	binding := operation.Request.DevelopmentCommand
	processOperation := operation
	processOperation.Kind = edge.OperationProjectProcessStart
	// The private binding stays only in local metadata so its digest can be
	// bound to the durable process. It is not passed as execution authority.
	processOperation.Request.Argv = append([]string{"/usr/bin/timeout", "--signal=TERM", "--kill-after=10s", strconv.Itoa(binding.TimeoutSeconds) + "s"}, operation.Request.Argv...)
	return processOperation
}

func startResolvedDevelopmentProcess(ctx context.Context, processes *edgeclient.ProjectProcessManager, operation edge.Operation, resolved edgeclient.ProjectResolution, roots edgeclient.WorkspaceRoots, anchor development.WorkspaceAnchor) (edge.OperationResult, string) {
	if processes == nil || operation.DeviceID != anchor.DeviceID || resolved.Workspace.ID != anchor.WorkspaceID ||
		!anchor.Valid() || resolved.Workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev ||
		!resolved.Project.ClaimGenerationValid || resolved.Project.ClaimGeneration != anchor.Generation ||
		resolved.Project.Owner != anchor.Owner || resolved.Project.Repository != anchor.Repository ||
		resolved.Project.Alias != operation.Request.Alias || resolved.TargetAlias != operation.Request.TargetAlias {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	bindingDigest, err := developmentProcessBindingDigest(operation.Request)
	if err != nil {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	snapshot, _, err := processes.Start(ctx, edgeclient.ProjectProcessStartRequest{
		OperationID: operation.ID, IdempotencyKey: operation.Request.IdempotencyKey,
		ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
		ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectClaimGeneration: resolved.Project.ClaimGeneration, ProjectState: resolved.SafeState(),
		Workspace: resolved.Workspace, WorkspaceRoots: roots, Argv: operation.Request.Argv,
		CWD: operation.Request.CWD, Stdin: operation.Request.Stdin, Environment: operation.Request.Environment,
		DevelopmentBindingDigest: bindingDigest,
	})
	if err != nil {
		return edge.OperationResult{}, safeDevelopmentProcessFailure(err)
	}
	return projectProcessOperationResult(resolved, snapshot), ""
}

func developmentProcessBindingDigest(request edge.OperationRequest) (string, error) {
	if (request.DevelopmentCommand == nil) == (request.DevelopmentBootstrap == nil) ||
		request.DevelopmentCommand != nil && !request.DevelopmentCommand.Valid() ||
		request.DevelopmentBootstrap != nil && !request.DevelopmentBootstrap.Valid(true) {
		return "", errors.New("development process binding is invalid")
	}
	body, err := json.Marshal(struct {
		Command   *edge.ProjectDevelopmentCommandBinding   `json:"command,omitempty"`
		Bootstrap *edge.ProjectDevelopmentBootstrapBinding `json:"bootstrap,omitempty"`
	}{request.DevelopmentCommand, request.DevelopmentBootstrap})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte("aeontra-development-process-binding-v1\x00"), body...))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func recoverDevelopmentProcess(processes *edgeclient.ProjectProcessManager, operation edge.Operation, anchor development.WorkspaceAnchor) (edge.OperationResult, bool, string) {
	bindingDigest, err := developmentProcessBindingDigest(operation.Request)
	if err != nil {
		return edge.OperationResult{}, false, "project_development_identity_mismatch"
	}
	operationID, key := operation.ID, operation.Request.IdempotencyKey
	recoveryOnly := operation.Request.DevelopmentRecoveryOperationID != "" || operation.Request.DevelopmentRecoveryIdempotencyKey != ""
	if recoveryOnly {
		if operation.Request.DevelopmentRecoveryOperationID == "" || operation.Request.DevelopmentRecoveryIdempotencyKey == "" {
			return edge.OperationResult{}, false, "project_development_identity_mismatch"
		}
		operationID, key = operation.Request.DevelopmentRecoveryOperationID, operation.Request.DevelopmentRecoveryIdempotencyKey
	}
	snapshot, found, err := processes.RecoverySnapshotByIdempotency(edgeclient.ProjectProcessRecoveryRequest{
		OperationID: operationID, IdempotencyKey: key, ProjectAlias: operation.Request.Alias, TargetAlias: operation.Request.TargetAlias,
		ProjectOwner: anchor.Owner, ProjectRepository: anchor.Repository, ProjectClaimGeneration: anchor.Generation, WorkspaceID: anchor.WorkspaceID,
		Argv: operation.Request.Argv, CWD: operation.Request.CWD, Stdin: operation.Request.Stdin, Environment: operation.Request.Environment,
		DevelopmentBindingDigest: bindingDigest,
	})
	if err != nil {
		return edge.OperationResult{}, false, safeDevelopmentProcessFailure(err)
	}
	if !found {
		if recoveryOnly {
			return edge.OperationResult{}, false, "project_development_reconciliation_required"
		}
		return edge.OperationResult{}, false, ""
	}
	return projectProcessOperationResult(durableProjectProcessResolutionFromSnapshot(snapshot), snapshot), true, ""
}

func safeDevelopmentProcessFailure(err error) string {
	switch {
	case errors.Is(err, edgeclient.ErrProjectProcessIdempotencyConflict):
		return "project_process_idempotency_conflict"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "project_process_failed"
	}
}

func executeProjectDevelopmentCommandStart(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	binding := operation.Request.DevelopmentCommand
	if processes == nil || binding == nil || !binding.Valid() || operation.DeviceID != binding.Anchor.DeviceID {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	processOperation := developmentCommandProcessOperation(operation)
	result, recovered, code := recoverDevelopmentProcess(processes, processOperation, binding.Anchor)
	if code != "" {
		return edge.OperationResult{}, code
	}
	if recovered {
		copy := *binding
		copy.Requirements = append([]development.CapabilityID(nil), binding.Requirements...)
		result.DevelopmentCommand = &copy
		return result, ""
	}
	inspectionContext, code := inspectProjectDevelopment(ctx, stateRoot, operation)
	if code != "" {
		return edge.OperationResult{}, code
	}
	inspection := inspectionContext.result
	observed := inspection.DevelopmentInspection
	if inspection.WorkspaceID != binding.Anchor.WorkspaceID || inspection.ProjectOwner != binding.Anchor.Owner ||
		inspection.ProjectRepository != binding.Anchor.Repository || observed.ProjectGeneration != binding.Anchor.Generation {
		return edge.OperationResult{}, "project_development_identity_mismatch"
	}
	if observed.SourceDigest != binding.SourceDigest {
		return edge.OperationResult{}, "project_development_source_drift"
	}
	found := false
	for _, record := range observed.Environments {
		attestation, err := record.Attestation()
		if err != nil || record.Digest != binding.EnvironmentDigest || attestation.Class != development.ClassWorkcell {
			continue
		}
		requirements := make([]development.Requirement, len(binding.Requirements))
		for i, id := range binding.Requirements {
			requirements[i] = development.Requirement{ID: id}
		}
		found = len(attestation.Capabilities.Missing(requirements)) == 0
	}
	if !found {
		return edge.OperationResult{}, "project_development_capability_drift"
	}
	// Keep the original command in the verified binding. The fixed timeout
	// wrapper runs inside Bubblewrap, not in the host control plane.
	result, code = startResolvedDevelopmentProcess(ctx, processes, processOperation, inspectionContext.resolved, inspectionContext.roots, binding.Anchor)
	if code != "" {
		return edge.OperationResult{}, code
	}
	copy := *binding
	copy.Requirements = append([]development.CapabilityID(nil), binding.Requirements...)
	result.DevelopmentCommand = &copy
	return result, ""
}
