package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

// Count attempted effects before the fixture can deduplicate them. Faults can
// lose a committed acknowledgement or reject creation before any journal write.
type developmentCancelFaultEdge struct {
	*developmentTestEdge
	stopCalls, statusCalls, statusCreated int
	lostStatusACKAt, rejectStatusAt       int
	observe                               func(edge.Operation, int) edge.Operation
	stopLookup                            func(edge.Operation, bool, error) (edge.Operation, bool, error)
}

func (store *developmentCancelFaultEdge) CreateOperation(device string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	switch kind {
	case edge.OperationProjectProcessStop:
		store.stopCalls++
	case edge.OperationProjectProcessStatus:
		store.statusCalls++
		if store.statusCalls == store.rejectStatusAt {
			return edge.Operation{}, false, errors.New("status rejected before journal insert")
		}
	}
	op, created, err := store.developmentTestEdge.CreateOperation(device, kind, request)
	if err != nil || !created {
		return op, created, err
	}
	if kind == edge.OperationProjectProcessStatus {
		store.statusCreated++
	}
	if store.observe != nil {
		op = store.observe(op, store.statusCreated)
		store.mu.Lock()
		store.operations[op.ID] = op
		store.mu.Unlock()
	}
	if kind == edge.OperationProjectProcessStatus && store.statusCalls == store.lostStatusACKAt {
		return edge.Operation{}, false, errors.New("status journal committed but acknowledgement lost")
	}
	return op, created, nil
}

func (store *developmentCancelFaultEdge) LatestDevelopmentProcessOperation(device string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	op, found, err := store.developmentTestEdge.LatestDevelopmentProcessOperation(device, kind, request)
	if kind == edge.OperationProjectProcessStop && store.stopLookup != nil {
		return store.stopLookup(op, found, err)
	}
	return op, found, err
}

func cancelFaultObservation(op edge.Operation, stopState, statusState edge.OperationState) edge.Operation {
	if op.Kind == edge.OperationProjectProcessStop {
		op.State, op.SafeCode, op.Result = stopState, "project_process_failed", edge.OperationResult{}
	} else if op.Kind == edge.OperationProjectProcessStatus {
		op.State = statusState
		op.Result.BackgroundProcessState, op.Result.BackgroundExitKnown, op.Result.BackgroundExitCode = "stopped", true, 137
		if statusState == edge.OperationFailed || statusState == edge.OperationCancelled {
			op.SafeCode, op.Result = "project_process_failed", edge.OperationResult{}
		}
	}
	return op
}

func beginCancelFaultRequest(t *testing.T, server *Server, store *developmentCancelFaultEdge) workqueue.DevelopmentRequest {
	t.Helper()
	server.WithEdgeStore(store)
	view := developmentStart(t, server, "cancel-faults-001")
	developmentRounds(t, server, 2)
	request, found, err := server.workQueue.DevelopmentRequest(view.RequestID)
	if err != nil || !found || request.ProcessID == "" {
		t.Fatalf("missing captured process: %+v found=%t err=%v", request, found, err)
	}
	if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
		t.Fatal(err)
	}
	return request
}

func cancelFaultRounds(server *Server, count int) {
	for range count {
		_ = server.reconcileDevelopmentRequestsOnce(context.Background())
	}
}

func assertCancelFaultIdentity(t *testing.T, server *Server, captured workqueue.DevelopmentRequest) workqueue.DevelopmentRequest {
	t.Helper()
	current, found, err := server.workQueue.DevelopmentRequest(captured.ID)
	if err != nil || !found {
		t.Fatalf("request lost: found=%t err=%v", found, err)
	}
	if current.ProcessID != captured.ProcessID || current.DeviceID != captured.DeviceID || current.Alias != captured.Alias || current.Target != captured.Target || current.ObjectiveID != captured.ObjectiveID || current.BodyRef != captured.BodyRef || current.BodyDigest != captured.BodyDigest {
		t.Fatalf("captured request identity changed: before=%+v after=%+v", captured, current)
	}
	return current
}

func TestProjectDevelopmentCancellationSecondObservationFaultsKeepPhase(t *testing.T) {
	for _, mode := range []string{"lost-second-ack", "reject-second-before-insert"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			store := &developmentCancelFaultEdge{developmentTestEdge: edges}
			if mode == "lost-second-ack" {
				store.lostStatusACKAt = 2
			} else {
				store.rejectStatusAt = 2
			}
			store.observe = func(op edge.Operation, statusCreated int) edge.Operation {
				op = cancelFaultObservation(op, edge.OperationFailed, edge.OperationQueued)
				if op.Kind == edge.OperationProjectProcessStatus && statusCreated == 1 {
					op.State, op.Result.BackgroundProcessState, op.Result.BackgroundExitKnown, op.Result.BackgroundExitCode = edge.OperationSucceeded, "running", false, 0
				}
				return op
			}
			captured := beginCancelFaultRequest(t, server, store)
			cancelFaultRounds(server, 2)
			pending := assertCancelFaultIdentity(t, server, captured)
			if pending.State != workqueue.DevelopmentRequestCancelling || pending.ActiveOperationID == "" || store.stopCalls != 1 || store.statusCalls != 2 {
				t.Fatalf("second observation lost phase: request=%+v stops=%d reads=%d", pending, store.stopCalls, store.statusCalls)
			}
			firstPinned := pending.ActiveOperationID
			cancelFaultRounds(server, 3)
			pending = assertCancelFaultIdentity(t, server, captured)
			wantCalls := 2
			if mode == "reject-second-before-insert" {
				wantCalls = 3
			}
			if pending.State != workqueue.DevelopmentRequestCancelling || store.stopCalls != 1 || store.statusCalls != wantCalls || store.statusCreated != 2 {
				t.Fatalf("read recovery replayed or lost an effect: request=%+v stops=%d reads=%d created=%d", pending, store.stopCalls, store.statusCalls, store.statusCreated)
			}
			if mode == "lost-second-ack" && pending.ActiveOperationID != firstPinned {
				t.Fatal("lost acknowledgement did not retain the committed observation")
			}
			if mode == "reject-second-before-insert" && pending.ActiveOperationID == firstPinned {
				t.Fatal("rejected creation never advanced from the previous running observation")
			}
			edges.mu.Lock()
			op := edges.operations[pending.ActiveOperationID]
			op.State = edge.OperationSucceeded
			edges.operations[op.ID] = op
			edges.mu.Unlock()
			cancelFaultRounds(server, 3)
			finished := assertCancelFaultIdentity(t, server, captured)
			if finished.State != workqueue.DevelopmentRequestCancelled || store.stopCalls != 1 || store.statusCalls != wantCalls || edges.starts != 1 {
				t.Fatalf("terminal observation did not converge exactly: request=%+v stops=%d reads=%d starts=%d", finished, store.stopCalls, store.statusCalls, edges.starts)
			}
		})
	}
}

func TestProjectDevelopmentCancellationFailedAndCancelledObservationsStayPinned(t *testing.T) {
	for _, stopState := range []edge.OperationState{edge.OperationFailed, edge.OperationCancelled} {
		for _, statusState := range []edge.OperationState{edge.OperationSucceeded, edge.OperationFailed, edge.OperationCancelled} {
			t.Run(string(stopState)+"/"+string(statusState), func(t *testing.T) {
				server, edges, _ := developmentServer(t)
				store := &developmentCancelFaultEdge{developmentTestEdge: edges, observe: func(op edge.Operation, _ int) edge.Operation {
					return cancelFaultObservation(op, stopState, statusState)
				}}
				captured := beginCancelFaultRequest(t, server, store)
				cancelFaultRounds(server, 3)
				pinned := assertCancelFaultIdentity(t, server, captured)
				cancelFaultRounds(server, 5)
				current := assertCancelFaultIdentity(t, server, captured)
				wantState := workqueue.DevelopmentRequestCancelling
				if statusState == edge.OperationSucceeded {
					wantState = workqueue.DevelopmentRequestCancelled
				}
				if current.State != wantState || current.ActiveOperationID != pinned.ActiveOperationID || store.stopCalls != 1 || store.statusCalls != 1 || edges.starts != 1 {
					t.Fatalf("failed/cancelled evidence replayed or completed: request=%+v stops=%d reads=%d starts=%d", current, store.stopCalls, store.statusCalls, edges.starts)
				}
			})
		}
	}
}

func TestProjectDevelopmentCancellationInconsistentStopLookupDoesNotReplay(t *testing.T) {
	for _, mode := range []string{"device", "kind", "alias", "process", "profile", "state", "missing", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			store := &developmentCancelFaultEdge{developmentTestEdge: edges, observe: func(op edge.Operation, _ int) edge.Operation {
				return cancelFaultObservation(op, edge.OperationFailed, edge.OperationQueued)
			}}
			captured := beginCancelFaultRequest(t, server, store)
			cancelFaultRounds(server, 2)
			pinned := assertCancelFaultIdentity(t, server, captured)
			store.stopLookup = func(op edge.Operation, found bool, err error) (edge.Operation, bool, error) {
				switch mode {
				case "device":
					op.DeviceID = "ed_" + strings.Repeat("f", 32)
				case "kind":
					op.Kind = edge.OperationProjectProcessStatus
				case "alias":
					op.Request.Alias = "foreign"
				case "process":
					op.Request.BackgroundProcessID = "pr_" + strings.Repeat("f", 32)
				case "profile":
					op.Request.Profile = "windows-workcell"
				case "state":
					op.State = edge.OperationQueued
				case "missing":
					op, found = edge.Operation{}, false
				case "unavailable":
					err = errors.New("stop journal temporarily unavailable")
				}
				return op, found, err
			}
			cancelFaultRounds(server, 5)
			current := assertCancelFaultIdentity(t, server, captured)
			if current.State != workqueue.DevelopmentRequestCancelling || current.ActiveOperationID != pinned.ActiveOperationID || store.stopCalls != 1 || store.statusCalls != 1 || edges.starts != 1 {
				t.Fatalf("inconsistent stop lookup changed phase or replayed: request=%+v stops=%d reads=%d starts=%d", current, store.stopCalls, store.statusCalls, edges.starts)
			}
		})
	}
}

func TestProjectDevelopmentCancellationRejectsEveryProcessBindingMismatch(t *testing.T) {
	tamper := map[string]func(*edge.Operation){
		"result-alias":      func(op *edge.Operation) { op.Result.ProjectAlias = "foreign" },
		"result-target":     func(op *edge.Operation) { op.Result.ProjectTarget = "windows" },
		"result-profile":    func(op *edge.Operation) { op.Result.ProjectProfile = "windows-workcell" },
		"result-mode":       func(op *edge.Operation) { op.Result.ProjectMode = "read-only" },
		"result-workspace":  func(op *edge.Operation) { op.Result.WorkspaceID = "ws_" + strings.Repeat("f", 32) },
		"result-owner":      func(op *edge.Operation) { op.Result.ProjectOwner = "foreign" },
		"result-repository": func(op *edge.Operation) { op.Result.ProjectRepository = "foreign" },
		"result-process":    func(op *edge.Operation) { op.Result.BackgroundProcessID = "pr_" + strings.Repeat("f", 32) },
		"request-profile":   func(op *edge.Operation) { op.Request.Profile = "windows-workcell" },
		"device":            func(op *edge.Operation) { op.DeviceID = "ed_" + strings.Repeat("f", 32) },
		"kind":              func(op *edge.Operation) { op.Kind = edge.OperationProjectProcessStop },
		"kind-unrelated":    func(op *edge.Operation) { op.Kind = edge.OperationProjectExec },
		"kind-failed-stop": func(op *edge.Operation) {
			op.Kind, op.State, op.Result = edge.OperationProjectProcessStop, edge.OperationFailed, edge.OperationResult{}
		},
	}
	for name, mutate := range tamper {
		t.Run(name, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			store := &developmentCancelFaultEdge{developmentTestEdge: edges, observe: func(op edge.Operation, _ int) edge.Operation {
				op = cancelFaultObservation(op, edge.OperationFailed, edge.OperationSucceeded)
				if op.Kind == edge.OperationProjectProcessStatus {
					mutate(&op)
				}
				return op
			}}
			captured := beginCancelFaultRequest(t, server, store)
			before, _, _ := server.workQueue.DevelopmentObjective(captured.ObjectiveID)
			cancelFaultRounds(server, 5)
			current := assertCancelFaultIdentity(t, server, captured)
			after, _, _ := server.workQueue.DevelopmentObjective(captured.ObjectiveID)
			if current.State != workqueue.DevelopmentRequestCancelling || store.stopCalls != 1 || store.statusCalls != 1 || edges.starts != 1 || !reflect.DeepEqual(before, after) {
				t.Fatalf("mismatched process evidence changed lifecycle: request=%+v stops=%d reads=%d starts=%d", current, store.stopCalls, store.statusCalls, edges.starts)
			}
		})
	}
}
