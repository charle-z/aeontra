package devsupervisor

import (
	"context"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

// ProvisionEffect is returned by a server-owned broker after reconciling the
// stable effect ID. Pending effects must be recovered, never started anew.
type ProvisionEffect struct {
	Pending   bool
	Failure   development.FailureClass
	ResultRef string
}

type ProvisionRequest struct {
	Scope     development.ObjectiveScope
	Provision development.ProvisioningAttempt
	Lease     workqueue.Lease
}

// ProvisionExecutor must implement durable, idempotent effect reconciliation
// using ProvisionID, and reject stale leases before a new effect. It receives
// no arbitrary privileged command, path or credential from the objective.
type ProvisionExecutor interface {
	Reconcile(context.Context, ProvisionRequest) (ProvisionEffect, error)
	Cancel(context.Context, ProvisionRequest) (ProvisionEffect, error)
}

type ProvisionWorkerQueue interface {
	ProvisionQueue
	Heartbeat(string, string, uint64, time.Duration) (workqueue.HeartbeatStatus, error)
	Complete(string, string, uint64, workqueue.Result) (workqueue.Job, error)
}

// ReconcileProvisionLease is called by the existing coordinator with a lease
// obtained from workqueue. It creates no scheduler, goroutine or second store.
func (supervisor *Supervisor) ReconcileProvisionLease(ctx context.Context, objectiveID, stepID string, lease workqueue.Lease, executor ProvisionExecutor) (ProvisionResult, error) {
	if supervisor == nil {
		return ProvisionResult{}, ErrProvisionUnavailable
	}
	queue, ok := supervisor.provisionQueue.(ProvisionWorkerQueue)
	if !ok || executor == nil || ctx == nil || ctx.Err() != nil {
		return ProvisionResult{}, ErrProvisionUnavailable
	}
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return ProvisionResult{}, err
	}
	step, found := objectiveStep(objective, stepID)
	if !found || len(step.Provisioning) == 0 {
		return ProvisionResult{}, ErrProvisionConflict
	}
	provision := step.Provisioning[len(step.Provisioning)-1]
	job, found, err := queue.Get(lease.Job.ID)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !found || !matchesProvisionJob(job, provisionSpec(objective, provision)) ||
		lease.ID == "" || lease.ID != job.LeaseID || lease.Fence == 0 || lease.Fence != job.Fence ||
		lease.Job.LeaseHolder != job.LeaseHolder || lease.Job.ID != job.ID {
		return ProvisionResult{}, ErrProvisionConflict
	}
	// A completion ACK may have been lost. Read authoritative completion before
	// calling the provider, even if its old lease has since expired.
	if job.State == workqueue.StateSucceeded || job.State == workqueue.StateFailed {
		return supervisor.ProvisionStep(ctx, objectiveID, stepID, provision.SourceDigest)
	}
	if job.State == workqueue.StateCancelled && objective.State == development.ObjectiveCancelled {
		return ProvisionResult{Objective: objective, Provision: provision}, nil
	}
	if job.State != workqueue.StateLeased {
		return ProvisionResult{}, ErrProvisionConflict
	}
	heartbeat, err := queue.Heartbeat(job.ID, lease.ID, lease.Fence, time.Minute)
	if err != nil {
		return ProvisionResult{}, err
	}
	if provision.State == development.ProvisioningPlanned {
		next, err := objective.BindProvisionJob(stepID, provision.ProvisionID, job.ID)
		if err != nil {
			return ProvisionResult{}, err
		}
		objective, err = supervisor.persistProvision(next, stepID, provision.ProvisionID)
		if err != nil {
			return ProvisionResult{}, err
		}
	}
	step, _ = objectiveStep(objective, stepID)
	provision = step.Provisioning[len(step.Provisioning)-1]
	cancelling := heartbeat.CancelRequested && objective.State == development.ObjectiveCancelled && provision.State == development.ProvisioningCancelled
	if provision.JobID != job.ID || provision.State != development.ProvisioningQueued && !cancelling || provision.JobFence > lease.Fence {
		return ProvisionResult{}, ErrProvisionConflict
	}
	if provision.JobFence < lease.Fence && !cancelling {
		next, err := objective.BindProvisionFence(stepID, provision.ProvisionID, lease.Fence)
		if err != nil {
			return ProvisionResult{}, err
		}
		objective, err = supervisor.persistProvision(next, stepID, provision.ProvisionID)
		if err != nil {
			return ProvisionResult{}, err
		}
		step, _ = objectiveStep(objective, stepID)
		provision = step.Provisioning[len(step.Provisioning)-1]
	}
	request := ProvisionRequest{Scope: objective.Scope, Provision: provision, Lease: lease}
	// One reconciliation round is bounded; long effects continue in the broker's
	// journal and are polled on the next coordinator round.
	phase, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var effect ProvisionEffect
	if heartbeat.CancelRequested || objective.State == development.ObjectiveCancelled {
		effect, err = executor.Cancel(phase, request)
	} else {
		effect, err = executor.Reconcile(phase, request)
	}
	if err != nil {
		return ProvisionResult{}, err
	}
	if effect.Pending {
		return ProvisionResult{Objective: objective, Provision: provision}, nil
	}
	if heartbeat.CancelRequested && effect.Failure != "" {
		if _, known := development.ContinuationForFailure(effect.Failure); !known {
			return ProvisionResult{}, ErrProvisionConflict
		}
		// The queue's cancellation request is not proof that the external
		// effect stopped. Keep its captured lease and journal recoverable.
		return ProvisionResult{Objective: objective, Provision: provision}, ErrProvisionReconciliationRequired
	}
	result := workqueue.Result{Outcome: workqueue.StateSucceeded, Summary: "provisioned", ResultRef: effect.ResultRef}
	if heartbeat.CancelRequested {
		result.Outcome, result.Summary = workqueue.StateCancelled, "cancelled"
	} else if effect.Failure != "" {
		if _, known := development.ContinuationForFailure(effect.Failure); !known {
			return ProvisionResult{}, ErrProvisionConflict
		}
		result.Outcome, result.Summary = workqueue.StateFailed, string(effect.Failure)
	}
	completed, err := queue.Complete(job.ID, lease.ID, lease.Fence, result)
	if err != nil {
		// Complete may commit before returning an error. Recover only the exact
		// fenced outcome, never infer success from the provider's response.
		current, found, readErr := queue.Get(job.ID)
		if readErr != nil || !found || current.LeaseID != lease.ID || current.Fence != lease.Fence ||
			current.State != result.Outcome || current.Summary != result.Summary || current.ResultRef != result.ResultRef {
			return ProvisionResult{}, err
		}
		completed = current
	}
	if completed.State == workqueue.StateCancelled {
		return ProvisionResult{Objective: objective, Provision: provision}, nil
	}
	return supervisor.ProvisionStep(ctx, objectiveID, stepID, provision.SourceDigest)
}
