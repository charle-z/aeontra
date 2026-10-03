package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func TestProjectDevelopmentCancellationRequiresExactPreStartAbsenceReceipt(t *testing.T) {
	for _, mode := range []string{
		"capability-drift", "source-drift", "inventory-unavailable", "measurement-failed", "legacy-no-receipt", "generic-failure", "interrupted",
		"unavailable-journal", "foreign-device", "foreign-original", "foreign-key", "foreign-anchor",
		"source-mismatch", "environment-mismatch", "body-mismatch", "conflicting-process", "original-source-mismatch", "recovery-argv-mismatch", "original-process",
	} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind != edge.OperationProjectDevelopmentCommandStart {
					return op
				}
				op.State = edge.OperationFailed
				op.Result = edge.OperationResult{}
				if op.Request.DevelopmentRecoveryOperationID == "" {
					op.SafeCode = "project_development_capability_drift"
					switch mode {
					case "source-drift":
						op.SafeCode = "project_development_source_drift"
					case "inventory-unavailable":
						op.SafeCode = "project_development_inventory_unavailable"
					case "measurement-failed":
						op.SafeCode = "project_development_inventory_measurement_failed"
					case "generic-failure":
						op.SafeCode = "project_process_failed"
					case "interrupted":
						op.SafeCode = "operation_execution_interrupted"
					case "original-source-mismatch":
						binding := *op.Request.DevelopmentCommand
						binding.SourceDigest = "sha256:" + strings.Repeat("f", 64)
						op.Request.DevelopmentCommand = &binding
					case "original-process":
						op.Result.BackgroundProcessID = "pr_" + strings.Repeat("f", 32)
					}
					return op
				}
				op.SafeCode = edge.DevelopmentCommandEffectAbsentSafeCode
				receipt := &edge.ProjectDevelopmentCommandAbsence{Version: 1,
					OriginalOperationID:    op.Request.DevelopmentRecoveryOperationID,
					OriginalIdempotencyKey: op.Request.DevelopmentRecoveryIdempotencyKey,
					Command:                *op.Request.DevelopmentCommand,
				}
				op.Result.DevelopmentCommandAbsence = receipt
				switch mode {
				case "legacy-no-receipt":
					op.SafeCode = "project_development_reconciliation_required"
					op.Result.DevelopmentCommandAbsence = nil
				case "unavailable-journal":
					op.SafeCode = "project_process_failed"
					op.Result.DevelopmentCommandAbsence = nil
				case "foreign-device":
					op.DeviceID = "ed_" + strings.Repeat("f", 32)
				case "foreign-original":
					receipt.OriginalOperationID = "eo_" + strings.Repeat("f", 32)
				case "foreign-key":
					receipt.OriginalIdempotencyKey = "another-original-command"
				case "foreign-anchor":
					receipt.Command.Anchor.Generation++
				case "source-mismatch":
					receipt.Command.SourceDigest = "sha256:" + strings.Repeat("f", 64)
				case "environment-mismatch":
					receipt.Command.EnvironmentDigest = "sha256:" + strings.Repeat("f", 64)
				case "body-mismatch":
					receipt.Command.PrivateBodyDigest = "sha256:" + strings.Repeat("f", 64)
				case "conflicting-process":
					op.Result.BackgroundProcessID = "pr_" + strings.Repeat("f", 32)
				case "recovery-argv-mismatch":
					op.Request.Argv = []string{"another", "command"}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "cancel-absence-receipt-001")
			developmentRounds(t, server, 1)
			// The second round dispatches the rejected original and its absence
			// observation. Some forged observations deliberately return errors.
			_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			want := workqueue.DevelopmentRequestCancelling
			if mode == "capability-drift" || mode == "source-drift" || mode == "inventory-unavailable" || mode == "measurement-failed" {
				want = workqueue.DevelopmentRequestCancelled
				if objective.State != development.ObjectiveCancelled || objective.Steps[0].Attempts[0].State != development.AttemptCancelled {
					t.Fatalf("absence did not cancel exact attempt: %+v", objective)
				}
			}
			if request.State != want || request.ProcessID != "" || edges.starts != 1 || len(objective.Steps[0].Attempts) != 1 {
				t.Fatalf("state=%s want=%s process=%q starts=%d objective=%+v", request.State, want, request.ProcessID, edges.starts, objective)
			}
			for _, op := range edges.operations {
				if op.Kind == edge.OperationProjectProcessStop {
					t.Fatal("absence cancellation issued a blind process stop")
				}
			}
		})
	}
}

func TestProjectDevelopmentLegacyCancellationReobservesAbsenceOnce(t *testing.T) {
	for _, mode := range []string{"capability-drift", "source-drift", "inventory-unavailable", "measurement-failed", "lost-v2-ack", "legacy-skew", "unavailable-v2", "cancelled-recovery", "interrupted", "generic-failure", "legacy-body-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind != edge.OperationProjectDevelopmentCommandStart {
					return op
				}
				op.State, op.Result = edge.OperationFailed, edge.OperationResult{}
				if op.Request.DevelopmentRecoveryOperationID == "" {
					op.SafeCode = "project_development_capability_drift"
					if mode == "source-drift" {
						op.SafeCode = "project_development_source_drift"
					} else if mode == "inventory-unavailable" {
						op.SafeCode = "project_development_inventory_unavailable"
					} else if mode == "measurement-failed" {
						op.SafeCode = "project_development_inventory_measurement_failed"
					} else if mode == "interrupted" {
						op.SafeCode = "operation_execution_interrupted"
					} else if mode == "generic-failure" {
						op.SafeCode = "project_process_failed"
					}
				} else if strings.HasSuffix(op.Request.IdempotencyKey, ":command-recover-v2") {
					op.State, op.SafeCode = edge.OperationQueued, ""
				} else {
					op.SafeCode = "project_development_reconciliation_required"
					if mode == "legacy-body-mismatch" {
						op.Request.Argv = []string{"foreign-command"}
					}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "cancel-legacy-absence-001")
			developmentRounds(t, server, 1)
			_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
				t.Fatal(err)
			}
			if mode == "lost-v2-ack" {
				observer.lostACK = edge.OperationProjectDevelopmentCommandStart
			}
			for range 4 {
				_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			var v2 edge.Operation
			v2Count := 0
			for _, op := range edges.operations {
				if strings.HasSuffix(op.Request.IdempotencyKey, ":command-recover-v2") {
					v2, v2Count = op, v2Count+1
				}
			}
			shouldObserve := mode != "interrupted" && mode != "generic-failure" && mode != "legacy-body-mismatch"
			if !shouldObserve {
				if v2Count != 0 || request.State != workqueue.DevelopmentRequestCancelling {
					t.Fatal("unverified original/legacy failure gained a fresh observation or settlement")
				}
				return
			}
			if v2Count != 1 || v2.State != edge.OperationQueued || request.State != workqueue.DevelopmentRequestCancelling || request.ActiveOperationID != v2.ID {
				t.Fatalf("pending recovery was duplicated, cancelled or not captured: count=%d state=%s request=%+v", v2Count, v2.State, request)
			}
			original := edges.operations[v2.Request.DevelopmentRecoveryOperationID]
			if v2.Request.DevelopmentRecoveryIdempotencyKey != original.Request.IdempotencyKey || v2.Request.DevelopmentCommand == nil || edges.starts != 1 {
				t.Fatal("fresh observation changed original effect or replayed command")
			}
			v2.State, v2.SafeCode = edge.OperationFailed, edge.DevelopmentCommandEffectAbsentSafeCode
			v2.Result.DevelopmentCommandAbsence = &edge.ProjectDevelopmentCommandAbsence{Version: 1,
				OriginalOperationID: original.ID, OriginalIdempotencyKey: original.Request.IdempotencyKey, Command: *original.Request.DevelopmentCommand}
			if mode == "legacy-skew" {
				v2.SafeCode, v2.Result = "project_development_reconciliation_required", edge.OperationResult{}
			} else if mode == "unavailable-v2" {
				v2.SafeCode, v2.Result = "project_process_failed", edge.OperationResult{}
			} else if mode == "cancelled-recovery" {
				v2.State, v2.SafeCode, v2.Result = edge.OperationCancelled, "operation_cancelled", edge.OperationResult{}
			}
			edges.operations[v2.ID] = v2
			for range 4 {
				_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			}
			request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			want := workqueue.DevelopmentRequestCancelled
			if mode == "legacy-skew" || mode == "unavailable-v2" || mode == "cancelled-recovery" {
				want = workqueue.DevelopmentRequestCancelling
			}
			if request.State != want || request.ProcessID != "" || edges.starts != 1 {
				t.Fatalf("state=%s want=%s process=%q starts=%d", request.State, want, request.ProcessID, edges.starts)
			}
			for _, op := range edges.operations {
				if strings.HasSuffix(op.Request.IdempotencyKey, ":command-recover-v2") {
					v2Count--
				}
				if op.Kind == edge.OperationProjectProcessStop {
					t.Fatal("absence observation issued a blind stop")
				}
			}
			if v2Count != 0 {
				t.Fatal("second versioned recovery observation was created")
			}
		})
	}
}
