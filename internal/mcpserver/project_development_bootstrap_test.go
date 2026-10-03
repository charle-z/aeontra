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

func TestProjectDevelopmentBootstrapFailClosedOnSourceAndInstallerFailure(t *testing.T) {
	for _, mode := range []string{"source", "pending-inspection", "bad-inspection", "installer-failed"} {
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
			if mode == "source" && request.Reason != workqueue.DevelopmentRequestReasonSourceChanged {
				t.Fatal("changed source installed toolchain")
			}
			if mode == "installer-failed" && request.Reason != workqueue.DevelopmentRequestReasonNewRequirement {
				t.Fatalf("failed installer inferred capabilities: %+v", request)
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
