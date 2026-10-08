package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

// This wrapper changes broker observations, not the lifecycle under test.
type developmentScenarioEdge struct {
	*developmentTestEdge
	observe func(edge.Operation) edge.Operation
	lostACK edge.OperationKind
}

func (store *developmentScenarioEdge) CreateOperation(device string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	op, created, err := store.developmentTestEdge.CreateOperation(device, kind, request)
	if err != nil || !created {
		return op, created, err
	}
	if store.observe != nil {
		op = store.observe(op)
		store.mu.Lock()
		store.operations[op.ID] = op
		store.mu.Unlock()
	}
	if store.lostACK == kind {
		store.lostACK = ""
		return edge.Operation{}, false, errors.New("lost observation acknowledgement")
	}
	return op, created, nil
}

func TestProjectDevelopmentAdmissionAndReadErrorsNeverCreateEffects(t *testing.T) {
	for _, body := range []string{
		`{"unexpected":true}`, `{"idempotency_key":"short"}`,
		`{"alias":"project","target":"parrot","idempotency_key":"validation-key-001","argv":[],"timeout_seconds":600}`,
		`{"alias":"project","target":"parrot","idempotency_key":"validation-key-001","argv":["go"],"timeout_seconds":600,"requirements":["bad capability"]}`,
		`{"alias":"project","target":"parrot","idempotency_key":"validation-key-001","argv":["go"],"timeout_seconds":600,"runner_profile":"unregistered"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			if _, err := server.handleProjectDevelopmentStart(json.RawMessage(body)); err == nil {
				t.Fatal("invalid admission accepted")
			}
			requests, err := server.workQueue.DevelopmentRequests(4)
			if err != nil || len(requests) != 0 || len(edges.operations) != 0 {
				t.Fatal("rejected admission created durable or external effects")
			}
		})
	}
	server, _, _ := developmentServer(t)
	for _, body := range []string{`{"unexpected":true}`, `{"request_id":"bad"}`, `{"request_id":"dr_` + strings.Repeat("a", 32) + `"}`} {
		if _, err := server.handleProjectDevelopmentStatus(json.RawMessage(body)); err == nil {
			t.Fatal("unknown request status accepted")
		}
		if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(body)); err == nil {
			t.Fatal("unknown request cancellation accepted")
		}
	}
	for _, field := range []string{"queue", "model", "edge", "inactive-target"} {
		t.Run(field, func(t *testing.T) {
			s, edges, _ := developmentServer(t)
			queue, turns, operations, devices := s.workQueue, s.modelTurns, s.edgeOperations, s.edgeDevices
			defer func() { s.workQueue, s.modelTurns, s.edgeOperations, s.edgeDevices = queue, turns, operations, devices }()
			switch field {
			case "queue":
				s.workQueue = nil
			case "model":
				s.modelTurns = nil
			case "edge":
				s.edgeOperations = nil
			case "inactive-target":
				s.edgeDevices = &inactiveProjectTaskEdgeStore{edges.projectTaskEdgeStore}
			}
			body := json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"missing-store-001","argv":["go"],"timeout_seconds":600}`)
			if _, err := s.handleProjectDevelopmentStart(body); err == nil {
				t.Fatal("missing authority store accepted")
			}
			if err := s.reconcileDevelopmentRequestsOnce(context.Background()); err == nil && field != "inactive-target" {
				t.Fatal("incomplete coordinator accepted")
			}
			if len(edges.operations) != 0 {
				t.Fatal("unavailable store performed effects")
			}
		})
	}
}

func TestProjectDevelopmentInspectionPendingFailureAndInvalidBinding(t *testing.T) {
	for _, mode := range []string{"pending", "failed", "anchor", "record", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind != edge.OperationProjectDevelopmentInspect {
					return op
				}
				switch mode {
				case "pending":
					op.State = edge.OperationQueued
				case "failed":
					op.State = edge.OperationFailed
				case "anchor":
					op.Result.WorkspaceID = "bad"
				case "record":
					op.Result.DevelopmentInspection.Environments[0].Digest = "sha256:" + strings.Repeat("f", 64)
				case "foreign":
					op.Result.ProjectOwner = "other"
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "inspection-state-001")
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "anchor" || mode == "record" {
				if err == nil {
					t.Fatal("invalid inspection accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "pending" && (request.State != workqueue.DevelopmentRequestPreparing || request.Reason != workqueue.DevelopmentRequestReasonInspectionPending) {
				t.Fatal("queued inspection advertised ready")
			}
			if mode == "failed" && request.State != workqueue.DevelopmentRequestFailed {
				t.Fatal("failed inspection advanced")
			}
			if edges.starts != 0 {
				t.Fatal("inspection executed command")
			}
		})
	}
}

func TestProjectDevelopmentProcessObservationRecoveryAndPendingStates(t *testing.T) {
	for _, mode := range []string{"lost-ack", "pending", "running", "foreign-result", "unknown-exit"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind != edge.OperationProjectProcessStatus {
					return op
				}
				switch mode {
				case "pending":
					op.State = edge.OperationQueued
				case "running":
					op.Result.BackgroundProcessState = "running"
					op.Result.BackgroundExitKnown = false
				case "foreign-result":
					op.Result.BackgroundProcessID = "pr_" + strings.Repeat("f", 32)
				case "unknown-exit":
					op.Result.BackgroundExitKnown = false
				}
				return op
			}}
			if mode == "lost-ack" {
				observer.lostACK = edge.OperationProjectProcessStatus
			}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "observation-state-001")
			developmentRounds(t, server, 2)
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "foreign-result" {
				if err == nil {
					t.Fatal("foreign process result accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "lost-ack" {
				developmentRounds(t, server, 1)
				request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
				if request.State != workqueue.DevelopmentRequestCompleted {
					t.Fatal("lost status ACK was not recovered")
				}
			} else if mode == "unknown-exit" {
				if request.State != workqueue.DevelopmentRequestAwaitingReasoning {
					t.Fatal("unknown exit became acceptance")
				}
			} else {
				if request.State != workqueue.DevelopmentRequestActive {
					t.Fatal("live process became completed")
				}
				developmentRounds(t, server, 1)
			}
			if edges.starts != 1 {
				t.Fatal("observation duplicated command")
			}
		})
	}
}

func TestProjectDevelopmentCancellationPendingRecoveryAndScopeChecks(t *testing.T) {
	for _, mode := range []string{"before-start", "pending-start", "interrupted", "missing-recovery", "lost-stop-ack", "running-stop", "foreign-stop"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if mode == "pending-start" && op.Kind == edge.OperationProjectDevelopmentCommandStart {
					op.State = edge.OperationQueued
				}
				if op.Kind == edge.OperationProjectProcessStop {
					if mode == "running-stop" {
						op.Result.BackgroundProcessState = "running"
					}
					if mode == "foreign-stop" {
						op.Result.ProjectRepository = "foreign"
					}
				}
				return op
			}}
			if mode == "interrupted" || mode == "missing-recovery" {
				edges.interruptedStart = true
				edges.recoveryMissing = mode == "missing-recovery"
			}
			if mode == "lost-stop-ack" {
				observer.lostACK = edge.OperationProjectProcessStop
			}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "cancel-state-001")
			if mode != "before-start" {
				developmentRounds(t, server, 2)
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
				t.Fatal(err)
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "foreign-stop" {
				if err == nil {
					t.Fatal("foreign stop accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pending-start" || mode == "missing-recovery" || mode == "running-stop" {
				if request.State != workqueue.DevelopmentRequestCancelling {
					t.Fatal("unverified cancellation completed")
				}
			} else if request.State != workqueue.DevelopmentRequestCancelled {
				t.Fatalf("request=%+v", request)
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
				t.Fatal("cancellation replay", err)
			}
		})
	}
}

func TestProjectDevelopmentAcceptanceRevalidatesEnvironmentAndJournal(t *testing.T) {
	for _, mode := range []string{"environment", "missing-start", "tampered-body", "pending-inspection", "status-ack", "accepted-objective"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind == edge.OperationProjectDevelopmentInspect && strings.HasSuffix(op.Request.IdempotencyKey, ":accept-inspect") {
					if mode == "environment" {
						op.Result.DevelopmentInspection.Environments = nil
					}
					if mode == "pending-inspection" {
						op.State = edge.OperationQueued
					}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "acceptance-state-001")
			developmentRounds(t, server, 3)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "missing-start" || mode == "tampered-body" {
				edges.mu.Lock()
				for id, op := range edges.operations {
					if op.Kind == edge.OperationProjectDevelopmentCommandStart {
						if mode == "missing-start" {
							delete(edges.operations, id)
						} else {
							op.State = edge.OperationFailed
							op.Request.CWD = "changed"
							edges.operations[id] = op
						}
					}
				}
				edges.mu.Unlock()
			}
			if mode == "status-ack" {
				for _, op := range edges.operations {
					if op.Kind == edge.OperationProjectProcessStatus {
						request.ActiveOperationID = op.ID
					}
				}
				if _, err := server.saveDevelopmentRequest(request); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "accepted-objective" {
				objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
				step := objective.Steps[0]
				attempt := step.Attempts[0]
				var start edge.Operation
				for _, op := range edges.operations {
					if op.Kind == edge.OperationProjectDevelopmentCommandStart {
						start = op
					}
				}
				receipt, err := development.NewCommandAcceptanceReceipt(objective, "command", development.CommandOperationEvidence{OperationID: start.ID, CommandDigest: step.AcceptanceContract.CommandDigest, SourceDigest: attempt.SourceDigest, EnvironmentDigest: attempt.EnvironmentDigest, PrivateBodyRef: request.BodyRef, PrivateBodyDigest: request.BodyDigest, ExitCode: 0})
				if err != nil {
					t.Fatal(err)
				}
				accepted, err := objective.AcceptWithEvidence([]development.CommandAcceptanceReceipt{receipt})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = server.workQueue.SaveDevelopmentObjective(accepted); err != nil {
					t.Fatal(err)
				}
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "missing-start" || mode == "tampered-body" {
				if err == nil {
					t.Fatal("unavailable original evidence accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "environment" && request.Reason != workqueue.DevelopmentRequestReasonNewRequirement {
				t.Fatal("environment drift accepted")
			}
			if mode == "pending-inspection" && request.State != workqueue.DevelopmentRequestActive {
				t.Fatal("pending inspection accepted")
			}
			if mode == "status-ack" || mode == "accepted-objective" {
				if request.State != workqueue.DevelopmentRequestCompleted {
					t.Fatal("status ACK did not recover")
				}
			}
		})
	}
}
