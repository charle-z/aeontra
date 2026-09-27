package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

var (
	projectTaskTestProcessIDPattern = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	projectTaskTestDigestPattern    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type projectTaskTestParams struct {
	TaskID  string `json:"task_id"`
	Ordinal int    `json:"ordinal"`
}

type projectTaskTestView struct {
	TaskID            string `json:"task_id"`
	Ordinal           int    `json:"ordinal"`
	State             string `json:"state"`
	ProcessState      string `json:"process_state,omitempty"`
	TestEvidenceState string `json:"test_evidence_state"`
	AcceptanceState   string `json:"acceptance_state"`
	ProfileID         string `json:"profile_id"`
	HeadCommit        string `json:"head_commit,omitempty"`
	Branch            string `json:"branch,omitempty"`
	NextTool          string `json:"next_tool,omitempty"`
}

func projectTaskTestOperationKey(taskID string, ordinal int, action string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("task-test-%s-v1\x00%s\x00%d", action, taskID, ordinal)))
	return "task-test-" + action + ":" + hex.EncodeToString(digest[:16])
}

func (s *Server) projectTaskTestStored(params projectTaskTestParams) (workqueue.TaskGroup, workqueue.TaskWorker, error) {
	if s.workQueue == nil {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, errWorkQueueUnavailable
	}
	task, found, err := s.workQueue.Task(params.TaskID)
	if err != nil || !found || params.Ordinal < 0 || params.Ordinal >= len(task.Workers) || task.TestAcceptanceContract == nil || workqueue.ValidateTaskTestAcceptanceContract(task.TestAcceptanceContract) != nil {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, errors.New("project task test contract or worker is unavailable")
	}
	return task, task.Workers[params.Ordinal], nil
}

func (s *Server) projectTaskTestContext(params projectTaskTestParams) (workqueue.TaskGroup, workqueue.TaskWorker, edge.Device, error) {
	task, worker, err := s.projectTaskTestStored(params)
	if err != nil {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, edge.Device{}, err
	}
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, edge.Device{}, errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, edge.Device{}, errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(task.Target)
	if err != nil || !s.edgeDevices.DeviceActive(device.ID) {
		return workqueue.TaskGroup{}, workqueue.TaskWorker{}, edge.Device{}, errors.New("active edge target not found")
	}
	return task, worker, device, nil
}

func (s *Server) handleProjectTaskTestStart(arguments json.RawMessage) (string, error) {
	var params projectTaskTestParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	task, worker, device, err := s.projectTaskTestContext(params)
	if err != nil {
		return "", err
	}
	if s.modelTurns == nil || worker.State != workqueue.StateSucceeded || worker.WorktreeCleaned || worker.RuntimeID == "" || worker.WorktreeID == "" || worker.WorkspaceID == "" {
		return "", errors.New("project task worker is not ready for test acceptance")
	}
	runtime, err := s.modelTurns.Runtime(context.Background(), worker.RuntimeID)
	if err != nil || runtime.State != modelturn.RuntimeStateCompleted {
		return "", errors.New("project task model runtime is not complete")
	}
	contract := task.TestAcceptanceContract
	request := edge.OperationRequest{
		Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell",
		WorktreeID: worker.WorktreeID, WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence,
		TestProfileID: contract.ProfileID, TestProfileDigest: contract.ProfileDigest,
		IdempotencyKey: projectTaskTestOperationKey(task.ID, worker.Ordinal, "start"),
	}
	op, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeTestStart, request)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 30*time.Second)
	}
	if err != nil {
		return "", err
	}
	if op.State != edge.OperationSucceeded || !validProjectTaskTestStart(task, worker, device.ID, op) {
		return "", errors.New("project task test start failed or returned inconsistent evidence")
	}
	// A replayed start operation can outlive the Edge process generation that
	// created it. Only a fresh status call can assert the current process state.
	return marshalToolValue(projectTaskTestView{TaskID: task.ID, Ordinal: worker.Ordinal, State: "status_required", TestEvidenceState: "unverified", AcceptanceState: "pending", ProfileID: contract.ProfileID, HeadCommit: op.Result.WorktreeHeadCommit, Branch: op.Result.WorktreeBranch, NextTool: "project_task_test_status"}, nil)
}

func (s *Server) projectTaskTestStartOperation(task workqueue.TaskGroup, worker workqueue.TaskWorker, device edge.Device) (edge.Operation, bool, error) {
	lookup, ok := s.edgeOperations.(edgeOperationIdempotencyLookup)
	if !ok {
		return edge.Operation{}, false, errors.New("edge test operation recovery is unavailable")
	}
	op, found, err := lookup.OperationByIdempotency(device.ID, edge.OperationProjectWorktreeTestStart, projectTaskTestOperationKey(task.ID, worker.Ordinal, "start"))
	if err != nil || !found {
		return edge.Operation{}, found, err
	}
	if !validProjectTaskTestStartRequest(task, worker, device.ID, op) {
		return edge.Operation{}, false, errors.New("project task test operation binding changed")
	}
	return op, true, nil
}

func validProjectTaskTestStartRequest(task workqueue.TaskGroup, worker workqueue.TaskWorker, deviceID string, op edge.Operation) bool {
	contract := task.TestAcceptanceContract
	return contract != nil && op.DeviceID == deviceID && op.Kind == edge.OperationProjectWorktreeTestStart &&
		op.Request.IdempotencyKey == projectTaskTestOperationKey(task.ID, worker.Ordinal, "start") &&
		op.Request.Alias == task.Project && op.Request.TargetAlias == task.Target && op.Request.Profile == "linux-workcell" &&
		op.Request.WorktreeID == worker.WorktreeID && op.Request.WorkJobID == worker.JobID &&
		op.Request.WorkLeaseID != "" && op.Request.WorkFence > 0 &&
		op.Request.TestProfileID == contract.ProfileID && op.Request.TestProfileDigest == contract.ProfileDigest
}

func validProjectTaskTestStart(task workqueue.TaskGroup, worker workqueue.TaskWorker, deviceID string, op edge.Operation) bool {
	result := op.Result
	return validProjectTaskTestCapturedStart(task, worker, deviceID, op) &&
		op.Request.WorkLeaseID == worker.LeaseID && op.Request.WorkFence == worker.Fence &&
		result.WorkLeaseID == worker.LeaseID && result.WorkFence == worker.Fence
}

// The captured start remains eligible for an explicit stop even after a lease
// rotates. It must still belong to this same task worker and process identity.
func validProjectTaskTestCapturedStart(task workqueue.TaskGroup, worker workqueue.TaskWorker, deviceID string, op edge.Operation) bool {
	result := op.Result
	return validProjectTaskTestStartRequest(task, worker, deviceID, op) &&
		result.WorktreeID == worker.WorktreeID && result.WorkspaceID == worker.WorkspaceID &&
		result.WorkJobID == worker.JobID && result.WorkLeaseID == op.Request.WorkLeaseID && result.WorkFence == op.Request.WorkFence &&
		result.WorktreeBaseCommit == task.BaseCommit && result.WorktreeBranch == "codex/worktree-"+strings.TrimPrefix(worker.WorktreeID, "wt_") &&
		validProjectTaskCommit(result.WorktreeHeadCommit) && projectTaskTestDigestPattern.MatchString(result.ContentDigest) &&
		result.TestProfileID == task.TestAcceptanceContract.ProfileID && result.TestProfileDigest == task.TestAcceptanceContract.ProfileDigest &&
		projectTaskTestProcessIDPattern.MatchString(result.BackgroundProcessID)
}

func (s *Server) handleProjectTaskTestStatus(arguments json.RawMessage) (string, error) {
	var params projectTaskTestParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	view, err := s.projectTaskTestStatus(context.Background(), params)
	return marshalToolValue(view, err)
}

func (s *Server) projectTaskTestStatus(ctx context.Context, params projectTaskTestParams) (projectTaskTestView, error) {
	task, worker, err := s.projectTaskTestStored(params)
	if err != nil {
		return projectTaskTestView{}, err
	}
	view := projectTaskTestView{TaskID: task.ID, Ordinal: worker.Ordinal, State: "not_started", TestEvidenceState: "not_started", AcceptanceState: "pending", ProfileID: task.TestAcceptanceContract.ProfileID}
	if worker.WorktreeCleaned {
		if worker.TestAcceptanceReceipt == nil {
			return projectTaskTestView{}, errors.New("project task test receipt is missing after cleanup")
		}
		view.State, view.TestEvidenceState = "passed", "verified"
		view.HeadCommit, view.Branch = worker.TestAcceptanceReceipt.HeadCommit, worker.TestAcceptanceReceipt.Branch
		return view, nil
	}
	task, worker, device, err := s.projectTaskTestContext(params)
	if err != nil {
		return projectTaskTestView{}, err
	}
	start, found, err := s.projectTaskTestStartOperation(task, worker, device)
	if err != nil {
		return projectTaskTestView{}, err
	}
	if !found {
		if worker.TestAcceptanceReceipt != nil {
			view.State, view.TestEvidenceState = "reconciliation_required", "unavailable"
			return view, nil
		}
		view.NextTool = "project_task_test_start"
		return view, nil
	}
	if start.State == edge.OperationQueued || start.State == edge.OperationLeased {
		view.State, view.TestEvidenceState, view.NextTool = "starting", "running", "project_task_test_status"
		return view, nil
	}
	if start.State != edge.OperationSucceeded {
		if worker.TestAcceptanceReceipt != nil {
			view.State, view.TestEvidenceState = "reconciliation_required", "unavailable"
		} else {
			view.State, view.TestEvidenceState = "failed", "failed"
		}
		return view, nil
	}
	if !validProjectTaskTestStart(task, worker, device.ID, start) {
		view.State, view.TestEvidenceState = "reconciliation_required", "stale"
		return view, nil
	}
	request := edge.OperationRequest{
		Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell",
		WorktreeID: start.Request.WorktreeID, WorkJobID: start.Request.WorkJobID, WorkLeaseID: start.Request.WorkLeaseID, WorkFence: start.Request.WorkFence,
		BackgroundProcessID: start.Result.BackgroundProcessID,
		TestProfileID:       start.Request.TestProfileID, TestProfileDigest: start.Request.TestProfileDigest,
	}
	status, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeTestStatus, request)
	if err == nil {
		status, err = s.edgeOperations.WaitOperation(ctx, status.ID, 10*time.Second)
	}
	if err != nil || status.State != edge.OperationSucceeded {
		view.State, view.TestEvidenceState = "reconciliation_required", "unavailable"
		return view, nil
	}
	result := status.Result
	view.ProcessState, view.HeadCommit, view.Branch = result.BackgroundProcessState, result.WorktreeHeadCommit, result.WorktreeBranch
	if result.BackgroundProcessID != start.Result.BackgroundProcessID || result.TestProfileID != start.Result.TestProfileID || result.TestProfileDigest != start.Result.TestProfileDigest ||
		result.WorktreeID != worker.WorktreeID || result.WorkspaceID != worker.WorkspaceID || result.WorkJobID != worker.JobID ||
		result.WorkLeaseID != worker.LeaseID || result.WorkFence != worker.Fence || result.WorktreeBaseCommit != task.BaseCommit ||
		result.WorktreeHeadCommit != start.Result.WorktreeHeadCommit || result.WorktreeBranch != start.Result.WorktreeBranch ||
		result.ContentDigest != start.Result.ContentDigest || result.TestStale {
		view.State, view.TestEvidenceState = "reconciliation_required", "stale"
		return view, nil
	}
	view.State, view.TestEvidenceState = "running", "running"
	view.NextTool = "project_task_test_status"
	if result.BackgroundProcessState != "exited" {
		if result.BackgroundProcessState == "failed" || result.BackgroundProcessState == "stopped" {
			view.State, view.TestEvidenceState, view.NextTool = "failed", "failed", ""
		}
		return view, nil
	}
	view.NextTool = ""
	if !result.BackgroundExitKnown || result.BackgroundExitCode != 0 || result.BackgroundTerminalSignal != "" {
		view.State, view.TestEvidenceState = "failed", "failed"
		return view, nil
	}
	receipt := workqueue.TaskTestAcceptanceReceipt{
		Version: 1, TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID,
		BaseCommit: task.BaseCommit, HeadCommit: result.WorktreeHeadCommit, Branch: result.WorktreeBranch,
		LeaseID: worker.LeaseID, Fence: worker.Fence, ContentDigest: result.ContentDigest,
		ProfileID: task.TestAcceptanceContract.ProfileID, ProfileDigest: task.TestAcceptanceContract.ProfileDigest,
		ContractDigest: workqueue.TaskTestAcceptanceContractDigest(task.TestAcceptanceContract), EdgeOperationID: status.ID,
		EdgeResult: workqueue.TaskTestEdgeResultPassed,
	}
	if worker.TestAcceptanceReceipt != nil {
		stored := worker.TestAcceptanceReceipt
		if stored.TaskID != receipt.TaskID || stored.Ordinal != receipt.Ordinal || stored.JobID != receipt.JobID ||
			stored.WorktreeID != receipt.WorktreeID || stored.WorkspaceID != receipt.WorkspaceID || stored.BaseCommit != receipt.BaseCommit ||
			stored.HeadCommit != receipt.HeadCommit || stored.Branch != receipt.Branch || stored.LeaseID != receipt.LeaseID || stored.Fence != receipt.Fence ||
			stored.ContentDigest != receipt.ContentDigest || stored.ProfileID != receipt.ProfileID || stored.ProfileDigest != receipt.ProfileDigest ||
			stored.ContractDigest != receipt.ContractDigest || stored.EdgeResult != receipt.EdgeResult {
			view.State, view.TestEvidenceState = "reconciliation_required", "stale"
			return view, nil
		}
	} else if _, err := s.workQueue.RecordTaskWorkerTestReceipt(receipt); err != nil {
		return projectTaskTestView{}, err
	}
	view.State, view.TestEvidenceState = "passed", "verified"
	return view, nil
}

func (s *Server) handleProjectTaskTestStop(arguments json.RawMessage) (string, error) {
	var params projectTaskTestParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	task, worker, device, err := s.projectTaskTestContext(params)
	if err != nil {
		return "", err
	}
	start, found, err := s.projectTaskTestStartOperation(task, worker, device)
	if err != nil {
		return "", err
	}
	if !found || start.State != edge.OperationSucceeded || !validProjectTaskTestCapturedStart(task, worker, device.ID, start) {
		return "", errors.New("project task test process is not available for stop")
	}
	request := edge.OperationRequest{
		Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell",
		WorktreeID: start.Request.WorktreeID, WorkJobID: start.Request.WorkJobID, WorkLeaseID: start.Request.WorkLeaseID, WorkFence: start.Request.WorkFence,
		BackgroundProcessID: start.Result.BackgroundProcessID,
		TestProfileID:       start.Request.TestProfileID, TestProfileDigest: start.Request.TestProfileDigest,
		IdempotencyKey: projectTaskTestOperationKey(task.ID, worker.Ordinal, "stop"),
	}
	stop, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeTestStop, request)
	if err == nil {
		stop, err = s.edgeOperations.WaitOperation(context.Background(), stop.ID, 30*time.Second)
	}
	if err != nil {
		return "", err
	}
	if stop.State != edge.OperationSucceeded || stop.Result.BackgroundProcessID != start.Result.BackgroundProcessID {
		return "", errors.New("project task test stop failed")
	}
	return marshalToolValue(projectTaskTestView{TaskID: task.ID, Ordinal: worker.Ordinal, State: "stopping", ProcessState: stop.Result.BackgroundProcessState, TestEvidenceState: "not_verified", AcceptanceState: "pending", ProfileID: task.TestAcceptanceContract.ProfileID, NextTool: "project_task_test_status"}, nil)
}
