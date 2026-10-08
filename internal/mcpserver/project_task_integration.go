package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func projectTaskIntegrationSchema() map[string]any {
	return closedObject(map[string]any{
		"version":              map[string]any{"type": "integer", "minimum": 1, "maximum": 1},
		"expected_base_commit": stringSchema("exact canonical base for the independent integrator", `^[a-f0-9]{40}$`, 40),
		"source_task_id":       stringSchema("terminal source task; recursive integration is not supported", `^tg_[a-f0-9]{32}$`, 35),
		"workers": map[string]any{"type": "array", "minItems": 2, "maxItems": workqueue.MaxTaskWorkers, "items": closedObject(map[string]any{
			"ordinal":     map[string]any{"type": "integer", "minimum": 0, "maximum": workqueue.MaxTaskWorkers - 1},
			"head_commit": stringSchema("selected immutable committed worker HEAD", `^[a-f0-9]{40}$`, 40),
		}, []string{"ordinal", "head_commit"})},
	}, []string{"version", "expected_base_commit", "source_task_id", "workers"})
}

func projectTaskIntegrationGoals(params projectTaskStartParams) []string {
	if params.IntegrationContract == nil {
		return params.Goals
	}
	goals := append([]string(nil), params.Goals...)
	var pins strings.Builder
	fmt.Fprintf(&pins, "\n\nResponsible reviewer/integrator for source task %s, exact base %s. Review the selected committed changes before integrating only in your own managed worktree. Preserve source worktrees and branches. Do not force/reset source refs or guess conflict resolution; report unresolved conflicts as blocked. Publication requires its separate preview and gates. Selected immutable inputs:\n", params.IntegrationContract.SourceTaskID, params.IntegrationContract.ExpectedBaseCommit)
	for _, worker := range params.IntegrationContract.Workers {
		fmt.Fprintf(&pins, "worker %d HEAD %s\n", worker.Ordinal, worker.HeadCommit)
	}
	goals[0] += pins.String()
	return goals
}

func validProjectTaskIntegrationParams(params projectTaskStartParams) bool {
	if params.IntegrationContract == nil {
		return true
	}
	return workqueue.ValidateTaskIntegrationContract(params.IntegrationContract) == nil && len(params.Goals) == 1 && params.TestProfileID != "" && params.ObjectiveContract != nil && params.ObjectiveContract.RequireClean
}

// Source status supplies fresh evidence, not caller claims. Capture receipts only
// after reconciliation so an absent optional receipt cannot later be substituted.
func (s *Server) captureProjectTaskIntegrationPins(ctx context.Context, contract *workqueue.TaskIntegrationContract, alias, target string) ([]workqueue.TaskIntegrationSourcePin, error) {
	if contract == nil {
		return nil, nil
	}
	source, found, err := s.workQueue.Task(contract.SourceTaskID)
	if err != nil || !found || source.IntegrationContract != nil || source.State != workqueue.TaskCompleted || source.Project != alias || source.Target != target || source.BaseCommit != contract.ExpectedBaseCommit {
		return nil, errors.New("project task integration source is unavailable or incompatible")
	}
	view := s.projectTaskStatusView(ctx, source)
	pins := make([]workqueue.TaskIntegrationSourcePin, 0, len(contract.Workers))
	for _, selected := range contract.Workers {
		if selected.Ordinal >= len(view.Workers) {
			return nil, errors.New("project task integration source worker is unavailable")
		}
		item := view.Workers[selected.Ordinal]
		if item.RuntimeState != string(modelturn.RuntimeStateCompleted) || item.State == "reconciliation_required" || !item.GitEvidenceKnown || item.Clean == nil || !*item.Clean || item.HeadCommit != selected.HeadCommit ||
			(source.TestAcceptanceContract != nil && item.TestEvidenceState != "verified") || (source.ObjectiveContract != nil && item.AcceptanceState != "accepted") {
			return nil, errors.New("project task integration source evidence changed or is unavailable")
		}
	}
	source, found, err = s.workQueue.Task(source.ID)
	if err != nil || !found {
		return nil, errors.New("project task integration source checkpoint unavailable")
	}
	for _, selected := range contract.Workers {
		worker := source.Workers[selected.Ordinal]
		if worker.State != workqueue.StateSucceeded || worker.WorktreeCleaned {
			return nil, errors.New("project task integration source is no longer live")
		}
		pins = append(pins, workqueue.TaskIntegrationPin(source, selected.Ordinal, selected.HeadCommit))
	}
	return pins, nil
}

func (s *Server) revalidateProjectTaskIntegrationSources(ctx context.Context, task workqueue.TaskGroup) error {
	if task.IntegrationContract == nil {
		return nil
	}
	pins, err := s.captureProjectTaskIntegrationPins(ctx, task.IntegrationContract, task.Project, task.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pins, task.IntegrationPins) {
		return errors.New("project task integration source bindings changed")
	}
	return nil
}

func (s *Server) verifyProjectTaskIntegrationBase(ctx context.Context, task workqueue.TaskGroup, device edge.Device) error {
	// A fresh server-owned key avoids treating the creation snapshot's cached
	// idempotent result as live evidence before a delayed runtime launch.
	key, err := projectTaskTestProfileOperationKey()
	if err != nil {
		return err
	}
	op, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectSnapshot, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", IdempotencyKey: key})
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(ctx, op.ID, 10*time.Second)
	}
	if err != nil || op.State != edge.OperationSucceeded || op.Result.ProjectAlias != task.Project || op.Result.ProjectTarget != task.Target || op.Result.SnapshotHead != task.BaseCommit {
		return errors.New("project task integration base changed or is unavailable")
	}
	return nil
}

func projectTaskIntegrationHeads(task workqueue.TaskGroup) []string {
	heads := make([]string, 0, len(task.IntegrationPins))
	seen := make(map[string]bool)
	for _, pin := range task.IntegrationPins {
		if !seen[pin.HeadCommit] {
			heads = append(heads, pin.HeadCommit)
			seen[pin.HeadCommit] = true
		}
	}
	return heads
}

func (s *Server) applyProjectTaskIntegrationStatus(ctx context.Context, task workqueue.TaskGroup, view *projectTaskView) {
	if task.IntegrationContract == nil {
		return
	}
	view.IntegrationState = "pending"
	item := &view.Workers[0]
	if task.State == workqueue.TaskFailed || task.State == workqueue.TaskCancelled {
		view.IntegrationState = "not_completed"
		return
	}
	if task.Workers[0].WorktreeCleaned {
		if task.IntegrationReceipt != nil && task.Workers[0].ObjectiveReceipt != nil && task.IntegrationReceipt.HeadCommit == task.Workers[0].ObjectiveReceipt.HeadCommit {
			view.IntegrationState = "verified"
			return
		}
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_receipt_missing"
		view.IntegrationState = "unavailable"
		return
	}
	if err := s.revalidateProjectTaskIntegrationSources(ctx, task); err != nil {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_source_changed"
		view.IntegrationState = "stale"
		return
	}
	if item.AcceptanceState != "accepted" {
		return
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_evidence_unavailable"
		view.IntegrationState = "unavailable"
		return
	}
	device, err := resolver.ResolveActiveDeviceName(task.Target)
	heads := projectTaskIntegrationHeads(task)
	var op edge.Operation
	if err == nil {
		op, _, err = s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: task.Workers[0].WorktreeID, WorktreeAncestorCommits: heads})
	}
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(ctx, op.ID, 10*time.Second)
	}
	r := op.Result
	w := task.Workers[0]
	if err != nil || op.State != edge.OperationSucceeded || !r.WorktreeAncestorsVerified || !reflect.DeepEqual(r.WorktreeAncestorCommits, heads) || !r.WorktreeEvidenceKnown || !r.WorktreeClean ||
		r.WorktreeState != "ready" || r.WorktreeRole != "writer" || r.ProjectAlias != task.Project || r.ProjectTarget != task.Target || r.WorktreeHeadCommit != item.HeadCommit || r.WorktreeBaseCommit != task.BaseCommit || r.WorktreeID != w.WorktreeID || r.WorkspaceID != w.WorkspaceID || r.WorkJobID != w.JobID || r.WorkLeaseID != w.LeaseID || r.WorkFence != w.Fence || r.WorktreeBranch != item.Branch {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_ancestry_unavailable"
		view.IntegrationState = "unavailable"
		return
	}
	// Recheck sources after ancestry and tests: shared Git metadata is trusted
	// repository authority, not an isolation guarantee for sibling refs.
	if err := s.revalidateProjectTaskIntegrationSources(ctx, task); err != nil {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_source_changed"
		view.IntegrationState = "stale"
		return
	}
	test, err := s.projectTaskTestStatus(ctx, projectTaskTestParams{TaskID: task.ID, Ordinal: 0})
	if err != nil || test.TestEvidenceState != "verified" || test.HeadCommit != item.HeadCommit {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_test_evidence_stale"
		view.IntegrationState = "unavailable"
		return
	}
	current, found, err := s.workQueue.Task(task.ID)
	if err != nil || !found || current.Workers[0].ObjectiveReceipt == nil {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_objective_unavailable"
		view.IntegrationState = "unavailable"
		return
	}
	receipt, err := s.workQueue.RecordTaskIntegrationReceipt(workqueue.TaskIntegrationReceipt{Version: 1, TaskID: task.ID, ContractDigest: workqueue.TaskIntegrationDigest(task.IntegrationContract), SourcesDigest: workqueue.TaskIntegrationDigest(task.IntegrationPins), ObjectiveReceiptDigest: workqueue.TaskIntegrationDigest(*current.Workers[0].ObjectiveReceipt), HeadCommit: item.HeadCommit})
	if err != nil || receipt.HeadCommit != item.HeadCommit {
		item.State, item.AcceptanceState, item.ReconciliationReason = "reconciliation_required", "reconciliation_required", "integration_receipt_conflict"
		view.IntegrationState = "unavailable"
		return
	}
	view.IntegrationState = "verified"
}
