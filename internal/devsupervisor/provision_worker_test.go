package devsupervisor

import (
	"context"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type recordingProvisionExecutor struct {
	requests      []ProvisionRequest
	source        *mutableCatalogSource
	environment   development.EnvironmentAttestation
	pending       bool
	cancelFailure development.FailureClass
}

func (executor *recordingProvisionExecutor) Reconcile(_ context.Context, request ProvisionRequest) (ProvisionEffect, error) {
	executor.requests = append(executor.requests, request)
	if executor.pending {
		return ProvisionEffect{Pending: true}, nil
	}
	executor.source.Set(supervisorCatalogForWorker(executor.environment))
	return ProvisionEffect{}, nil
}

func (executor *recordingProvisionExecutor) Cancel(_ context.Context, request ProvisionRequest) (ProvisionEffect, error) {
	executor.requests = append(executor.requests, request)
	return ProvisionEffect{Failure: executor.cancelFailure}, nil
}

func supervisorCatalogForWorker(environment development.EnvironmentAttestation) development.EnvironmentCatalog {
	catalog, _ := development.NewEnvironmentCatalog(environment)
	return catalog
}

func TestProvisionWorkerPersistsLeaseBeforeEffectAndRecoversCompletedJob(t *testing.T) {
	store, source, supervisor, objective, plan := provisioningFixture(t, "objective-provision-worker")
	queued, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingProvisionExecutor{source: source, environment: supervisorEnvironment(t, "rust", plan.OutputClass, 1, "toolchain.rust.1.95.0"), pending: true}
	result, err := supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", lease, executor)
	if err != nil || result.Ready || result.Provision.JobFence != lease.Fence {
		t.Fatalf("pending result=%+v err=%v", result, err)
	}
	persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil || persisted.Steps[0].Provisioning[0].JobFence != lease.Fence {
		t.Fatal("fence was not durable before provider effect")
	}
	if len(executor.requests) != 1 || executor.requests[0].Provision.JobID != queued.Provision.JobID || executor.requests[0].Provision.JobFence != lease.Fence {
		t.Fatal("provider did not receive bound job/fence")
	}
	executor.pending = false
	result, err = supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", lease, executor)
	if err != nil || !result.Ready {
		t.Fatalf("completion result=%+v err=%v", result, err)
	}
	count := len(executor.requests)
	result, err = supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", lease, executor)
	if err != nil || !result.Ready || len(executor.requests) != count {
		t.Fatal("completed job repeated effect")
	}
	forged := lease
	forged.Fence++
	if _, err := supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", forged, executor); err == nil || len(executor.requests) != count {
		t.Fatal("forged lease reached provider")
	}
}

func TestProvisionWorkerCanCancelLeasedEffectAfterObjectiveCancellation(t *testing.T) {
	store, source, supervisor, objective, plan := provisioningFixture(t, "objective-provision-worker-cancel")
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Cancel(context.Background(), objective.ObjectiveID); err != nil {
		t.Fatal(err)
	}
	executor := &recordingProvisionExecutor{source: source}
	if _, err := supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", lease, executor); err != nil {
		t.Fatal(err)
	}
	job, found, err := store.Get(lease.Job.ID)
	if err != nil || !found || job.State != workqueue.StateCancelled || len(executor.requests) != 1 {
		t.Fatalf("cancelled effect queue=%+v calls=%d err=%v", job, len(executor.requests), err)
	}
}

func TestProvisionWorkerDoesNotClaimUnknownCancellationAsClean(t *testing.T) {
	store, source, supervisor, objective, plan := provisioningFixture(t, "objective-provision-cancel-unknown")
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Cancel(context.Background(), objective.ObjectiveID); err != nil {
		t.Fatal(err)
	}
	executor := &recordingProvisionExecutor{source: source, cancelFailure: development.FailureReconciliationNeeded}
	if _, err := supervisor.ReconcileProvisionLease(context.Background(), objective.ObjectiveID, "validate", lease, executor); err == nil {
		t.Fatal("unknown cancellation reported as reconciled")
	}
	job, found, err := store.Get(lease.Job.ID)
	if err != nil || !found || job.State != workqueue.StateLeased || !job.CancelRequested {
		t.Fatalf("unknown effect falsely terminal: %+v err=%v", job, err)
	}
}
