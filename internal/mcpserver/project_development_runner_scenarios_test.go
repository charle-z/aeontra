package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/tools"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type developmentRunnerScenario struct {
	*developmentRunnerTestProvider
	startError    error
	result        *tools.DevelopmentRunnerResult
	probe         tools.DevelopmentRunnerResult
	probeError    error
	generation    uint64
	missingCaps   bool
	cancelPending bool
	cancelError   error
}

func (runner *developmentRunnerScenario) EnsureCalibration(context.Context) (tools.DevelopmentRunnerResult, error) {
	return runner.probe, runner.probeError
}
func (runner *developmentRunnerScenario) ConfiguredTemplateAttestation() (development.EnvironmentAttestation, error) {
	base, err := runner.developmentRunnerTestProvider.ConfiguredTemplateAttestation()
	if err == nil && runner.missingCaps {
		caps, _ := development.NewCapabilitySet()
		return development.NewEnvironmentAttestation(base.EnvironmentID, base.Class, base.Generation, caps)
	}
	if err != nil || runner.generation == 0 {
		return base, err
	}
	return development.NewEnvironmentAttestation(base.EnvironmentID, base.Class, runner.generation, base.Capabilities)
}
func (runner *developmentRunnerScenario) Start(ctx context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	if runner.startError != nil {
		return tools.DevelopmentRunnerResult{}, runner.startError
	}
	if runner.result == nil {
		return runner.developmentRunnerTestProvider.Start(ctx, request)
	}
	if runner.result.State == "failed" || runner.result.State == "cancelled" {
		runner.pending = true
		if _, err := runner.developmentRunnerTestProvider.Start(ctx, request); err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
		effect, _, err := runner.queue.DevelopmentRunnerEffect(request.EffectID)
		if err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
		effect.Revision++
		effect.State = runner.result.State
		effect.Failure = runner.result.Failure
		if effect.State == "failed" && effect.Failure == "" {
			effect.Failure = development.FailureReconciliationNeeded
		}
		if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
		return *runner.result, nil
	}
	if _, err := runner.developmentRunnerTestProvider.Start(ctx, request); err != nil {
		return tools.DevelopmentRunnerResult{}, err
	}
	return *runner.result, nil
}
func (runner *developmentRunnerScenario) Cancel(ctx context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	if runner.cancelError != nil {
		return tools.DevelopmentRunnerResult{}, runner.cancelError
	}
	if runner.cancelPending {
		effect, _, err := runner.queue.DevelopmentRunnerEffect(request.EffectID)
		if err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
		if effect.State != "cancel_intent" {
			effect.Revision++
			effect.State = "cancel_intent"
			if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
				return tools.DevelopmentRunnerResult{}, err
			}
		}
		return tools.DevelopmentRunnerResult{State: "cancel_intent", Pending: true}, nil
	}
	return runner.developmentRunnerTestProvider.Cancel(ctx, request)
}

func TestProjectDevelopmentRunnerFailureClassificationAndLeaseSettlement(t *testing.T) {
	for _, class := range []development.FailureClass{development.FailureCode, development.FailureCapabilityMissing, development.FailureDependencyMissing, development.FailureExternalTransient, ""} {
		t.Run(string(class), func(t *testing.T) {
			server, _, _ := developmentServer(t)
			runner := &developmentRunnerScenario{developmentRunnerTestProvider: &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}, result: &tools.DevelopmentRunnerResult{State: "failed", Failure: class}}
			server.developmentRunner = runner
			view := runnerDevelopmentStart(t, server, "runner-classify-001")
			developmentRounds(t, server, 2)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			effect, _, _ := server.workQueue.DevelopmentRunnerEffect(developmentRunnerEffect(request, objective))
			job, _, _ := server.workQueue.Get(effect.JobID)
			if objective.Steps[0].Attempts[0].State != development.AttemptFailed || job.State != workqueue.StateFailed || runner.posts != 1 {
				t.Fatal("failure left an execution lease or accepted attempt")
			}
			wantState, wantReason := workqueue.DevelopmentRequestFailed, workqueue.DevelopmentRequestReasonOperationFailed
			if class == development.FailureCode {
				wantState, wantReason = workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonCodeFailure
			}
			if class == development.FailureCapabilityMissing || class == development.FailureDependencyMissing {
				wantState, wantReason = workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement
			}
			if request.State != wantState || request.Reason != wantReason {
				t.Fatalf("request=%+v", request)
			}
		})
	}
}

func TestProjectDevelopmentRunnerFailedCalibrationNeverPlansWorkload(t *testing.T) {
	for _, mode := range []string{"failed", "broker-error", "missing-capability"} {
		t.Run(mode, func(t *testing.T) {
			server, _, _ := developmentServer(t)
			runner := &developmentRunnerScenario{developmentRunnerTestProvider: &developmentRunnerTestProvider{queue: server.workQueue}, probe: tools.DevelopmentRunnerResult{State: "failed"}}
			if mode == "broker-error" {
				runner.probeError = errors.New("calibration broker unavailable")
			}
			server.developmentRunner = runner
			view := runnerDevelopmentStart(t, server, "runner-probe-fail-001")
			developmentRounds(t, server, 1)
			if mode == "missing-capability" {
				runner.calibrated = true
				runner.missingCaps = true
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if mode == "missing-capability" {
				if err != nil {
					t.Fatal(err)
				}
				if len(objective.Steps[0].Attempts) != 0 || runner.posts != 0 || request.Reason != workqueue.DevelopmentRequestReasonCapabilityMissing {
					t.Fatal("calibrated template without required capability planned a workload")
				}
			} else {
				if len(objective.Steps[0].Attempts) != 0 || runner.posts != 0 {
					t.Fatal("failed calibration dispatched workload")
				}
				if mode == "broker-error" && err == nil {
					t.Fatal("calibration error hidden")
				}
				if mode == "failed" && request.State != workqueue.DevelopmentRequestFailed {
					t.Fatal("failed probe advanced")
				}
			}
		})
	}
}

func TestProjectDevelopmentRunnerRejectsUnverifiedSourceTemplateAndReceipt(t *testing.T) {
	for _, mode := range []string{"private-source", "dirty-source", "unknown-source", "missing-run", "bad-caps", "changed-generation", "provider-missing", "bad-anchor", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			runner := &developmentRunnerScenario{developmentRunnerTestProvider: &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true, pending: mode == "changed-generation"}}
			server.developmentRunner = runner
			view := runnerDevelopmentStart(t, server, "runner-unverified-001")
			developmentRounds(t, server, 1)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			switch mode {
			case "private-source":
				runner.startError = tools.ErrDevelopmentRunnerSourceNotPublic
			case "dirty-source", "unknown-source", "bad-anchor":
				edges.mu.Lock()
				op := edges.operations[request.InspectionOperationID]
				if mode == "dirty-source" {
					op.Result.DevelopmentInspection.SourceClean = false
				}
				if mode == "unknown-source" {
					op.Result.DevelopmentInspection.SourceEvidenceKnown = false
				}
				if mode == "bad-anchor" {
					op.Result.ProjectRepository = "other"
				}
				edges.operations[op.ID] = op
				edges.mu.Unlock()
			case "missing-run":
				runner.result = &tools.DevelopmentRunnerResult{State: "succeeded", ReceiptDigest: "sha256:" + strings.Repeat("f", 64)}
			case "bad-caps":
				runner.result = &tools.DevelopmentRunnerResult{State: "succeeded", RunID: 91, ReceiptDigest: "sha256:" + strings.Repeat("f", 64), Capabilities: []development.CapabilityID{"bad capability"}}
			case "provider-missing":
				server.developmentRunner = nil
			case "cancelled":
				runner.result = &tools.DevelopmentRunnerResult{State: "cancelled"}
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			if mode == "changed-generation" {
				if err != nil {
					t.Fatal(err)
				}
				runner.generation = 2
				err = server.reconcileDevelopmentRequestsOnce(context.Background())
			}
			request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "missing-run" || mode == "bad-caps" || mode == "provider-missing" || mode == "bad-anchor" {
				if err == nil {
					t.Fatal("unverified provider accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "private-source" || mode == "dirty-source" || mode == "unknown-source" {
				if request.Reason != workqueue.DevelopmentRequestReasonSourceChanged {
					t.Fatal("source export denial not actionable")
				}
			}
			if mode == "changed-generation" && request.Reason != workqueue.DevelopmentRequestReasonNewRequirement {
				t.Fatal("attempt retargeted template generation")
			}
			if mode == "cancelled" && request.State != workqueue.DevelopmentRequestCancelled {
				t.Fatal("cancelled run left request live")
			}
			if request.State == workqueue.DevelopmentRequestCompleted {
				t.Fatal("unverified receipt accepted")
			}
		})
	}
}

func TestProjectDevelopmentRunnerCancelPendingAndCapturedSourceValidation(t *testing.T) {
	for _, mode := range []string{"pending", "error", "source-changed", "provider-unavailable", "before-effect"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			runner := &developmentRunnerScenario{developmentRunnerTestProvider: &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true, pending: true}}
			server.developmentRunner = runner
			view := runnerDevelopmentStart(t, server, "runner-cancel-pending-001")
			developmentRounds(t, server, 1)
			if mode != "before-effect" {
				developmentRounds(t, server, 1)
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "pending" {
				runner.cancelPending = true
			}
			if mode == "error" {
				runner.cancelError = errors.New("cancel ACK uncertain")
			}
			if mode == "provider-unavailable" {
				server.developmentRunner = nil
			}
			if mode == "source-changed" {
				edges.mu.Lock()
				op := edges.operations[request.InspectionOperationID]
				op.Result.DevelopmentInspection.SourceDigest = "sha256:" + strings.Repeat("f", 64)
				edges.operations[op.ID] = op
				edges.mu.Unlock()
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + request.ID + `"}`)); err != nil {
				t.Fatal(err)
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "error" || mode == "source-changed" || mode == "provider-unavailable" {
				if err == nil || request.State != workqueue.DevelopmentRequestCancelling {
					t.Fatal("uncertain captured cancellation completed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pending" {
				if request.State != workqueue.DevelopmentRequestCancelling {
					t.Fatal("cancel ACK inferred stop")
				}
				runner.cancelPending = false
				developmentRounds(t, server, 1)
				request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			}
			if request.State != workqueue.DevelopmentRequestCancelled {
				t.Fatal("verified cancellation not settled")
			}
		})
	}
}

func TestProjectDevelopmentRunnerRequestRejectsUnsupportedPrivateBody(t *testing.T) {
	server, _, _ := developmentServer(t)
	if returned := server.WithDevelopmentRunner(nil); returned != server {
		t.Fatal("configuration builder did not retain server")
	}
	for _, body := range []projectDevelopmentBody{{Argv: []string{"go"}, TimeoutSeconds: 4200}, {Argv: []string{"make", "validate-all"}, TimeoutSeconds: 4200}} {
		if _, err := server.developmentRunnerRequest(workqueue.DevelopmentRequest{}, development.Objective{}, body, edge.Operation{}, workqueue.Lease{}); err == nil {
			t.Fatal("missing source/private profile accepted")
		}
	}
	if profile, err := developmentRunnerCommandProfile([]string{"make", "validate-all"}, "", "", nil, 4200); err != nil || profile != "make-validate-all" {
		t.Fatal("exact profile rejected")
	}
}
