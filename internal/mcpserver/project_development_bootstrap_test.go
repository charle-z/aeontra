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

func TestProjectDevelopmentUnsupportedMissingRequirementAwaitsReasoningWithoutEffects(t *testing.T) {
	server, edges, _ := developmentServer(t)
	observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
		if op.Kind == edge.OperationProjectDevelopmentInspect {
			op.Result.DevelopmentInspection.Requirements = []development.CapabilityID{"toolchain.go.v1-26-6", "toolchain.pnpm.v10-13-1", "toolchain.rust.v1-95-0"}
			caps, _ := development.NewCapabilitySet("toolchain.go", "toolchain.go.v1-26-6")
			attestation, _ := development.NewEnvironmentAttestation("workcell:"+op.Result.WorkspaceID, development.ClassWorkcell, 1, caps)
			record, _ := attestation.Record()
			op.Result.DevelopmentInspection.Environments = []development.EnvironmentRecord{record}
		}
		return op
	}}
	server.WithEdgeStore(observer)
	view := developmentStart(t, server, "unsupported-pnpm-001")
	developmentRounds(t, server, 2)
	request, found, err := server.workQueue.DevelopmentRequest(view.RequestID)
	if err != nil || !found || request.State != workqueue.DevelopmentRequestAwaitingReasoning || request.Reason != workqueue.DevelopmentRequestReasonNewRequirement {
		t.Fatalf("unsupported requirement stayed active: request=%+v err=%v", request, err)
	}
	objective, found, err := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if err != nil || !found || len(objective.Steps[0].Provisioning) != 0 || len(objective.Steps[0].Attempts) != 0 {
		t.Fatalf("unsupported requirement planned an effect: objective=%+v err=%v", objective, err)
	}
	jobs, err := server.workQueue.List(20)
	if err != nil || len(jobs) != 0 || edges.starts != 0 || edges.bootstrapStarts != 0 {
		t.Fatalf("unsupported requirement dispatched work: jobs=%+v err=%v", jobs, err)
	}
	operationCount := len(edges.operations)
	developmentRounds(t, server, 4)
	repeated, _, err := server.workQueue.DevelopmentRequest(view.RequestID)
	if err != nil || repeated.Revision != request.Revision || repeated.State != request.State || repeated.Reason != request.Reason || len(edges.operations) != operationCount {
		t.Fatal("awaiting reasoning did not remain quiet", err)
	}
}

func TestProjectDevelopmentBootstrapCancellationRequiresExternalStopReceipt(t *testing.T) {
	for _, mode := range []string{"running", "pending-stop", "lost-stop-ack", "unstarted", "queue-only-cancel"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			edges.bootstrapEnabled = true
			edges.missingCapability = true
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Request.BackgroundProcessID == "pr_"+strings.Repeat("b", 32) && op.Kind == edge.OperationProjectProcessStatus {
					op.Result.BackgroundProcessState = "running"
					op.Result.BackgroundExitKnown = false
					edges.mu.Lock()
					edges.bootstrapInstalled = false
					edges.mu.Unlock()
				}
				if mode == "pending-stop" && op.Kind == edge.OperationProjectProcessStop {
					op.State = edge.OperationQueued
				}
				return op
			}}
			if mode == "lost-stop-ack" {
				observer.lostACK = edge.OperationProjectProcessStop
			}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "bootstrap-cancel-001")
			developmentRounds(t, server, 1)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if mode == "unstarted" {
				initial, _ := server.edgeOperations.OperationStatus(request.InspectionOperationID)
				_, envs, err := developmentInspection(request, initial)
				if err != nil {
					t.Fatal(err)
				}
				supervisor, _, err := server.developmentBootstrapSupervisor(objective, envs)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "command", initial.Result.DevelopmentInspection.SourceDigest); err != nil {
					t.Fatal(err)
				}
			} else {
				developmentRounds(t, server, 1)
			}
			objective, _, _ = server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if len(objective.Steps[0].Provisioning) != 1 {
				t.Fatal("test lacks captured provisioning")
			}
			provision := objective.Steps[0].Provisioning[0]
			if mode == "queue-only-cancel" {
				job, _, err := server.workQueue.Get(provision.JobID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = server.workQueue.Cancel(job.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = server.workQueue.Complete(job.ID, job.LeaseID, job.Fence, workqueue.Result{Outcome: workqueue.StateCancelled, Summary: "cancelled"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + request.ID + `"}`)); err != nil {
				t.Fatal(err)
			}
			developmentRounds(t, server, 1)
			request, _, _ = server.workQueue.DevelopmentRequest(request.ID)
			job, _, _ := server.workQueue.Get(provision.JobID)
			if mode == "pending-stop" || mode == "queue-only-cancel" {
				if request.State != workqueue.DevelopmentRequestCancelling {
					t.Fatal("queue cancellation or pending stop inferred external stop")
				}
			} else {
				if request.State != workqueue.DevelopmentRequestCancelled || job.State != workqueue.StateCancelled {
					t.Fatalf("request=%+v job=%+v", request, job)
				}
				if mode != "unstarted" && job.ResultRef == "" {
					t.Fatal("host effect cancellation lacked receipt")
				}
			}
			if edges.starts != 0 || mode == "unstarted" && edges.bootstrapStarts != 0 {
				t.Fatal("cancellation dispatched a workload/installer")
			}
		})
	}
}

func TestProjectDevelopmentCancelSettlesOnlyFailedResolutionWithoutStart(t *testing.T) {
	for _, mode := range []string{"unstarted", "unknown-start-ack"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			edges.bootstrapEnabled = true
			edges.missingCapability = true
			var resolved *edge.ProjectDevelopmentBootstrapBinding
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind == edge.OperationProjectDevelopmentBootstrapResolve {
					resolved = op.Result.DevelopmentBootstrap
					op.State = edge.OperationFailed
					op.SafeCode = "project_development_resolution_unavailable"
					op.Result = edge.OperationResult{}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "bootstrap-failed-cancel-001")
			developmentRounds(t, server, 2)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if len(objective.Steps[0].Provisioning) != 1 || len(objective.Steps[0].Attempts) != 0 || resolved == nil {
				t.Fatal("test lacks pre-command bootstrap failure")
			}
			provision := objective.Steps[0].Provisioning[0]
			job, _, _ := server.workQueue.Get(provision.JobID)
			if provision.State != development.ProvisioningFailed || job.State != workqueue.StateFailed {
				t.Fatalf("test lacks terminal failure: provision=%+v job=%+v", provision, job)
			}
			if mode == "unknown-start-ack" {
				var resolve edge.Operation
				for _, op := range edges.operations {
					if op.Kind == edge.OperationProjectDevelopmentBootstrapResolve {
						resolve = op
					}
				}
				startRequest := resolve.Request
				startRequest.IdempotencyKey = strings.Replace(startRequest.IdempotencyKey, "bootstrap-resolve:", "bootstrap-start:", 1)
				startRequest.DevelopmentBootstrap = resolved
				observer.lostACK = edge.OperationProjectDevelopmentBootstrapStart
				if _, _, err := observer.CreateOperation(resolve.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startRequest); err == nil {
					t.Fatal("test did not lose the installer start acknowledgement")
				}
				if _, found, err := edges.OperationByIdempotency(resolve.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startRequest.IdempotencyKey); err != nil || !found {
					t.Fatal("lost installer acknowledgement lacked authoritative operation", err)
				}
			}
			operationCount, bootstrapStarts := len(edges.operations), edges.bootstrapStarts
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + request.ID + `"}`)); err != nil {
				t.Fatal(err)
			}
			if err := server.reconcileDevelopmentRequestsOnce(context.Background()); err != nil && mode == "unstarted" {
				t.Fatal(err)
			}
			request, _, _ = server.workQueue.DevelopmentRequest(request.ID)
			objective, _, _ = server.workQueue.DevelopmentObjective(request.ObjectiveID)
			want := workqueue.DevelopmentRequestCancelled
			if mode == "unknown-start-ack" {
				want = workqueue.DevelopmentRequestCancelling
			}
			if request.State != want || objective.State != development.ObjectiveCancelled {
				t.Fatalf("cancellation state=%s want=%s objective=%s", request.State, want, objective.State)
			}
			retained, _, _ := server.workQueue.Get(job.ID)
			if retained.State != workqueue.StateFailed || retained.Fence != job.Fence || retained.Summary != job.Summary ||
				objective.Steps[0].Provisioning[0].State != development.ProvisioningFailed || len(objective.Steps[0].Attempts) != 0 {
				t.Fatal("cancellation rewrote failure or fabricated command evidence")
			}
			if len(edges.operations) != operationCount || edges.bootstrapStarts != bootstrapStarts || edges.starts != 0 {
				t.Fatal("failed-resolution cancellation dispatched an Edge effect")
			}
		})
	}
}

func TestProjectDevelopmentBootstrapFailClosedOnSourceAndInstallerFailure(t *testing.T) {
	for _, mode := range []string{"source", "pending-inspection", "bad-inspection", "installer-failed", "running-installer", "unverified-receipt"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			edges.bootstrapEnabled = true
			edges.missingCapability = true
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind == edge.OperationProjectDevelopmentInspect && strings.Contains(op.Request.IdempotencyKey, ":provision-inspect:") {
					if mode == "source" {
						op.Result.DevelopmentInspection.SourceDigest = "sha256:" + strings.Repeat("f", 64)
					}
					if mode == "pending-inspection" {
						op.State = edge.OperationQueued
					}
					if mode == "bad-inspection" {
						op.Result.DevelopmentInspection = nil
					}
				}
				if mode == "installer-failed" && op.Kind == edge.OperationProjectProcessStatus {
					op.Result.BackgroundExitCode = 1
					edges.mu.Lock()
					edges.bootstrapInstalled = false
					edges.mu.Unlock()
				}
				if (mode == "running-installer" || mode == "unverified-receipt") && op.Kind == edge.OperationProjectProcessStatus {
					edges.mu.Lock()
					edges.bootstrapInstalled = false
					edges.mu.Unlock()
					if mode == "running-installer" {
						op.Result.BackgroundProcessState = "running"
						op.Result.BackgroundExitKnown = false
					}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "bootstrap-failure-001")
			developmentRounds(t, server, 1)
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			if mode == "bad-inspection" {
				if err == nil {
					t.Fatal("bad installer source anchor accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "running-installer" || mode == "unverified-receipt" {
				developmentRounds(t, server, 4)
				request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
			}
			if mode == "source" && request.Reason != workqueue.DevelopmentRequestReasonSourceChanged {
				t.Fatal("changed source installed toolchain")
			}
			if mode == "installer-failed" && request.Reason != workqueue.DevelopmentRequestReasonNewRequirement {
				t.Fatalf("failed installer inferred capabilities: %+v", request)
			}
			if (mode == "pending-inspection" || mode == "running-installer" || mode == "unverified-receipt") &&
				(request.State != workqueue.DevelopmentRequestActive || request.Reason != workqueue.DevelopmentRequestReasonCapabilityMissing) {
				t.Fatalf("pending or unverified provisioning misclassified: %+v", request)
			}
			if edges.starts != 0 {
				t.Fatal("unverified bootstrap executed command")
			}
		})
	}
}

func TestProjectDevelopmentBootstrapCatalogAndCompletedEffects(t *testing.T) {
	server, edges, _ := developmentServer(t)
	view := developmentStart(t, server, "bootstrap-no-effects-001")
	developmentRounds(t, server, 1)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if stopped, err := server.cancelDevelopmentBootstrap(context.Background(), request, objective); err != nil || !stopped {
		t.Fatal("no provisioning should need no external stop", err)
	}
	snapshot := developmentCatalogSnapshot{scope: objective.Scope}
	other := objective
	other.Scope.Project = "other"
	if _, err := snapshot.Catalog(context.Background(), other); err == nil {
		t.Fatal("catalog escaped pinned scope")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshot.Catalog(ctx, objective); err == nil {
		t.Fatal("cancelled catalog request accepted")
	}
	server.edgeOperations = edges.projectTaskEdgeStore
	if _, _, err := server.developmentBootstrapSupervisor(objective, nil); err == nil {
		t.Fatal("non-bootstrap broker accepted")
	}
	server.edgeOperations = edges
	edges.bootstrapEnabled = true
	edges.missingCapability = true
	// A separate command needs the precise version, making provisioning real.
	view = developmentStart(t, server, "bootstrap-completed-001")
	developmentRounds(t, server, 7)
	request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ = server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if len(objective.Steps[0].Provisioning) != 1 || objective.Steps[0].Provisioning[0].State != development.ProvisioningSucceeded {
		t.Fatal("test lacks verified completed installer")
	}
	if stopped, err := server.cancelDevelopmentBootstrap(context.Background(), request, objective); err != nil || !stopped {
		t.Fatal("completed installer cancellation mutated lifecycle", err)
	}
}
