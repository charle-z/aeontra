package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectGitSyncParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	PlanID         string `json:"plan_id,omitempty"`
}

type projectGitSyncPublicView struct {
	OperationID    string              `json:"operation_id"`
	OperationState edge.OperationState `json:"operation_state"`
	Alias          string              `json:"alias"`
	Repository     string              `json:"repository,omitempty"`
	Target         string              `json:"target"`
	Branch         string              `json:"branch,omitempty"`
	Head           string              `json:"head,omitempty"`
	RemoteHead     string              `json:"remote_head,omitempty"`
	Ahead          int                 `json:"ahead"`
	Behind         int                 `json:"behind"`
	Diverged       bool                `json:"diverged"`
	Detached       bool                `json:"detached"`
	Dirty          bool                `json:"dirty"`
	Fetched        bool                `json:"remote_tracking_current"`
	FastForwarded  bool                `json:"fast_forwarded"`
	Published      bool                `json:"published"`
	PlanID         string              `json:"plan_id,omitempty"`
	PlanExpiresAt  string              `json:"plan_expires_at,omitempty"`
	Reused         bool                `json:"reused"`
	Reason         string              `json:"reason,omitempty"`
}

func (s *Server) addProjectGitSyncTools(projectSchema map[string]any) {
	common := map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}
	key := stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128)
	s.addDirectTool(toolDef{Name: "project_git_status", Description: "Inspect the registered Edge checkout and its fixed owner-bound origin. Returns only branch, commit, remote commit, clean/detached/diverged and ahead/behind metadata; no paths, URLs or credentials.", InputSchema: closedObject(common, []string{"alias", "target"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitStatus)
	})
	s.addDirectTool(toolDef{Name: "project_git_fetch", Description: "Fetch exactly the registered owner-bound origin without tags or caller refspecs, then return bounded checkout relation metadata.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key}, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitFetch)
	})
	s.addDirectTool(toolDef{Name: "project_git_fast_forward_preview", Description: "Create a short-lived single-use plan for an exact clean attached non-diverged fast-forward to the already fetched owner-bound remote HEAD. It does not change the checkout.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key}, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitFastForwardPreview)
	})
	s.addDirectTool(toolDef{Name: "project_git_fast_forward", Description: "Consume one exact Edge-owned plan after revalidating project, branch, clean tree, local HEAD and remote HEAD; executes only git merge --ff-only of the bound commit.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key, "plan_id": stringSchema("opaque single-use Edge fast-forward plan", `^gp_[a-f0-9]{32}$`, 35)}, []string{"alias", "target", "idempotency_key", "plan_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitFastForward)
	})
	s.addDirectTool(toolDef{Name: "project_git_publish_preview", Description: "Create a short-lived single-use plan bound to the registered owner repository, clean attached branch, exact local HEAD and current remote branch state. It exposes no credential, URL, refspec or force option.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key}, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitPublishPreview)
	})
	s.addDirectTool(toolDef{Name: "project_git_publish", Description: "Consume and revalidate one exact publication plan, then push only the bound current branch to its same-name branch on the fixed owner-bound origin without force or caller refspecs.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key, "plan_id": stringSchema("opaque single-use Edge publication plan", `^gp_[a-f0-9]{32}$`, 35)}, []string{"alias", "target", "idempotency_key", "plan_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectGitSync(raw, edge.OperationProjectGitPublish)
	})
}

func (s *Server) handleProjectGitSync(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectGitSyncParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if kind == edge.OperationProjectGitStatus && (params.IdempotencyKey != "" || params.PlanID != "") {
		return "", errors.New("status accepts no plan or idempotency key")
	}
	if kind != edge.OperationProjectGitFastForward && kind != edge.OperationProjectGitPublish && params.PlanID != "" {
		return "", errors.New("plan is accepted only for fast-forward or publication")
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: params.IdempotencyKey, GitPlanID: params.PlanID}
	op, created, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
	}
	view := projectGitSyncPublicView{OperationID: op.ID, OperationState: op.State, Alias: params.Alias, Target: params.Target, Reused: err == nil && !created}
	if op.State == edge.OperationSucceeded {
		r := op.Result
		view.Alias = r.ProjectAlias
		view.Repository = r.ProjectOwner + "/" + r.ProjectRepository
		view.Target = r.ProjectTarget
		view.Branch = r.GitBranch
		view.Head = r.GitHead
		view.RemoteHead = r.GitRemoteHead
		view.Ahead = r.GitAhead
		view.Behind = r.GitBehind
		view.Diverged = r.GitDiverged
		view.Detached = r.GitDetached
		view.Dirty = r.GitDirty
		view.Fetched = r.GitFetched
		view.FastForwarded = r.GitFastForwarded
		view.Published = r.GitPublished
		view.PlanID = r.GitPlanID
		view.PlanExpiresAt = r.GitPlanExpiresAt
	} else if op.State == edge.OperationFailed || op.State == edge.OperationCancelled {
		view.Reason = op.SafeCode
	}
	return marshalToolValue(view, err)
}
