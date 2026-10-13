package mcpserver

import "github.com/charle-z/mcp-devbox/internal/workqueue"

func applyTaskObjectiveReceipt(view *projectTaskWorkerView, receipt workqueue.TaskObjectiveReceipt) {
	view.State, view.AcceptanceState, view.ObjectiveEvidenceState = "accepted", "accepted", "verified"
	view.HeadCommit = receipt.HeadCommit
	clean, ahead, changed := receipt.Clean, receipt.CommitsAheadBase, receipt.ChangedPathCount
	view.Clean, view.CommitsAheadBase, view.ChangedPathCount = &clean, &ahead, &changed
	at := receipt.RecordedAt
	view.ObjectiveRecordedAt = &at
}

// Evaluation uses live observations; a persisted receipt is never enough to
// accept a worktree that still exists. After managed cleanup it is historical
// evidence of the exact declared contract, not a new observation of the source.
func (s *Server) evaluateProjectTaskObjective(task workqueue.TaskGroup, worker workqueue.TaskWorker, view *projectTaskWorkerView, observedHead string) {
	view.ObjectiveEvidenceState = "not_verified"
	if view.State == "reconciliation_required" || view.RuntimeState != "completed" || view.TestEvidenceState != "verified" {
		return
	}
	if !view.GitEvidenceKnown || view.Clean == nil || view.CommitsAheadBase == nil || view.ChangedPathCount == nil || observedHead == "" || observedHead != view.HeadCommit {
		view.State, view.AcceptanceState, view.ReconciliationReason = "reconciliation_required", "reconciliation_required", "objective_source_mismatch"
		return
	}
	if !workqueue.TaskObjectiveCriteriaSatisfied(task.ObjectiveContract, *view.Clean, *view.CommitsAheadBase, *view.ChangedPathCount) {
		view.ObjectiveEvidenceState = "criteria_not_met"
		return
	}
	current, found, err := s.workQueue.Task(task.ID)
	if err != nil || !found || worker.Ordinal >= len(current.Workers) || current.Workers[worker.Ordinal].TestAcceptanceReceipt == nil {
		view.State, view.AcceptanceState, view.ReconciliationReason = "reconciliation_required", "reconciliation_required", "objective_test_receipt_unavailable"
		return
	}
	worker = current.Workers[worker.Ordinal]
	if worker.TestAcceptanceReceipt.HeadCommit != observedHead {
		view.State, view.AcceptanceState, view.ReconciliationReason = "reconciliation_required", "reconciliation_required", "objective_source_mismatch"
		return
	}
	receipt, err := s.workQueue.RecordTaskWorkerObjective(workqueue.TaskObjectiveReceipt{
		Version: 1, TaskID: task.ID, Ordinal: worker.Ordinal,
		ContractDigest: workqueue.TaskObjectiveContractDigest(current, worker), TestReceiptDigest: workqueue.TaskTestReceiptDigest(*worker.TestAcceptanceReceipt),
		HeadCommit: observedHead, Clean: *view.Clean, CommitsAheadBase: *view.CommitsAheadBase, ChangedPathCount: *view.ChangedPathCount,
	})
	if err != nil {
		view.State, view.AcceptanceState, view.ReconciliationReason = "reconciliation_required", "reconciliation_required", "objective_receipt_conflict"
		return
	}
	applyTaskObjectiveReceipt(view, receipt)
}
