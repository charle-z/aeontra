package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectRegistryListParams struct {
	Target string `json:"target"`
}

type projectRegistryReconcileParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
}

type projectRegistryReleaseParams struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository"`
	Target     string `json:"target"`
	Generation uint64 `json:"generation"`
}

type projectRegistryClaimPublicView struct {
	Alias      string `json:"alias"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Target     string `json:"target"`
	Generation uint64 `json:"generation"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	Repairable bool   `json:"repairable"`
}

type projectRegistryPublicView struct {
	Action     string                           `json:"action"`
	State      edge.OperationState              `json:"state"`
	Alias      string                           `json:"alias,omitempty"`
	Owner      string                           `json:"owner,omitempty"`
	Repository string                           `json:"repository,omitempty"`
	Target     string                           `json:"target,omitempty"`
	Generation uint64                           `json:"generation,omitempty"`
	Claims     []projectRegistryClaimPublicView `json:"claims,omitempty"`
	Reason     string                           `json:"reason,omitempty"`
}

func (s *Server) addProjectRegistryRecoveryTools(projectSchema map[string]any) {
	target := projectSchema["target"]
	s.addDirectTool(toolDef{
		Name:        "project_registry_list",
		Description: "List durable project claims for one target on a paired Edge without running Git status or discovery, or changing workspaces. It performs a bounded repository-identity check. Claims include only safe repository identity, target, generation and lifecycle state.",
		InputSchema: closedObject(map[string]any{"target": target}, []string{"target"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectRegistryList)
	s.addDirectTool(toolDef{
		Name:        "project_reconcile",
		Description: "Reconcile durable project claim state after a recoverable workspace or registry interruption. It revalidates the registered boundary and never deletes source files or associates a new repository.",
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": target}, []string{"alias", "target"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectReconcile)
	s.addDirectTool(toolDef{
		Name:        "project_release",
		Description: "Release one stale project registry claim by exact alias, repository, target and generation. This removes registry metadata only; it never deletes or resets the workspace source tree.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "repository": projectSchema["repository"], "target": target,
			"generation": map[string]any{"type": "integer", "minimum": 1, "maximum": int64(^uint64(0) >> 1), "description": "exact claim generation returned by project_registry_list"},
		}, []string{"alias", "repository", "target", "generation"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false},
	}, s.handleProjectRelease)
}

func (s *Server) handleProjectRegistryList(arguments json.RawMessage) (string, error) {
	var params projectRegistryListParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.handleProjectRegistryOperation(edge.OperationProjectRegistryList, params.Target, "", "", 0)
}

func (s *Server) handleProjectReconcile(arguments json.RawMessage) (string, error) {
	var params projectRegistryReconcileParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.handleProjectRegistryOperation(edge.OperationProjectReconcile, params.Target, params.Alias, "", 0)
}

func (s *Server) handleProjectRelease(arguments json.RawMessage) (string, error) {
	var params projectRegistryReleaseParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.handleProjectRegistryOperation(edge.OperationProjectRelease, params.Target, params.Alias, params.Repository, params.Generation)
}

func (s *Server) handleProjectRegistryOperation(kind edge.OperationKind, target, alias, repository string, generation uint64) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{Alias: alias, Repository: repository, TargetAlias: target, Profile: "linux-workcell", ProjectClaimGeneration: generation}
	op, _, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
	}
	view := projectRegistryPublicView{State: op.State, Action: string(kind)}
	if op.State == edge.OperationSucceeded {
		view.Action = op.Result.ProjectRegistryAction
		view.State = op.State
		view.Alias, view.Owner, view.Repository, view.Target = op.Result.ProjectAlias, op.Result.ProjectOwner, op.Result.ProjectRepository, op.Result.ProjectTarget
		view.Generation = op.Result.ProjectClaimGeneration
		view.Claims = make([]projectRegistryClaimPublicView, 0, len(op.Result.ProjectClaims))
		for _, claim := range op.Result.ProjectClaims {
			view.Claims = append(view.Claims, projectRegistryClaimPublicView{Alias: claim.Alias, Owner: claim.Owner, Repository: claim.Repository, Target: claim.Target, Generation: claim.Generation, State: claim.State, Reason: claim.Reason, Repairable: claim.Repairable})
		}
	} else if op.State == edge.OperationFailed {
		view.State = op.State
		view.Reason = op.SafeCode
	}
	return marshalToolValue(view, err)
}
