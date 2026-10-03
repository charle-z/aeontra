package devsupervisor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

const MaxProvisionRounds = 4

type ProvisionCoordinatorQueue interface {
	ProvisionWorkerQueue
	LeaseNext(string, string, time.Duration) (workqueue.Lease, error)
	LeasesForHolder(string, string, int) ([]workqueue.Lease, error)
	DevelopmentProvisionOwner(string) (development.Objective, string, bool, error)
}

// ReconcileProvisionPool is one bounded round of the existing coordinator. It
// owns no ticker, process, queue or database. Pools and executors are registered
// by the server; repository metadata cannot choose them.
func (supervisor *Supervisor) ReconcileProvisionPool(ctx context.Context, pool, holder string, executors map[string]ProvisionExecutor) error {
	if supervisor == nil || ctx == nil || ctx.Err() != nil || len(executors) == 0 || len(executors) > MaxAttestationProviders {
		return ErrProvisionUnavailable
	}
	queue, ok := supervisor.provisionQueue.(ProvisionCoordinatorQueue)
	if !ok {
		return ErrProvisionUnavailable
	}
	leases, err := queue.LeasesForHolder(pool, holder, MaxProvisionRounds)
	if err != nil {
		return err
	}
	for len(leases) < MaxProvisionRounds {
		lease, err := queue.LeaseNext(pool, holder, time.Minute)
		if errors.Is(err, workqueue.ErrNoJobAvailable) {
			break
		}
		if err != nil {
			return err
		}
		leases = append(leases, lease)
	}
	var joined error
	for _, lease := range leases {
		if err := ctx.Err(); err != nil {
			return errors.Join(joined, err)
		}
		if !strings.HasPrefix(lease.Job.IdempotencyKey, "development:") {
			joined = errors.Join(joined, ErrProvisionConflict)
			continue
		}
		id := strings.TrimPrefix(lease.Job.IdempotencyKey, "development:")
		objective, stepID, found, err := queue.DevelopmentProvisionOwner(id)
		if err != nil || !found {
			joined = errors.Join(joined, ErrProvisionConflict, err)
			continue
		}
		step, found := objectiveStep(objective, stepID)
		if !found || len(step.Provisioning) == 0 || step.Provisioning[len(step.Provisioning)-1].ProvisionID != id {
			joined = errors.Join(joined, ErrProvisionConflict)
			continue
		}
		plan := step.Provisioning[len(step.Provisioning)-1].Plan
		executor := executors[plan.Provider]
		if plan.Pool != pool || plan.Profile != lease.Job.Profile || executor == nil {
			joined = errors.Join(joined, ErrProvisionConflict)
			continue
		}
		_, err = supervisor.ReconcileProvisionLease(ctx, objective.ObjectiveID, stepID, lease, executor)
		// A successful effect without a fresh capability proof is pending,
		// not a failed coordinator and never an accepted objective.
		if !errors.Is(err, ErrProvisionUnverified) {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}
