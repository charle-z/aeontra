package frontdoorcoordinator

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	compensationPhaseBudget    = 12
	defaultCompensationBackoff = 2 * time.Second
)

func oppositeTarget(target Target) (Target, error) {
	switch target {
	case TargetCutover:
		return TargetRollback, nil
	case TargetRollback:
		return TargetCutover, nil
	default:
		return "", errors.New("front-door compensation target is invalid")
	}
}

func (r Runner) compensate(ctx context.Context, current Status, reason string, cause error) (Status, error) {
	recoveryTarget, err := oppositeTarget(current.Target)
	if err != nil {
		return r.fail(ctx, current, reason+"_compensation_unavailable", errors.Join(cause, err))
	}
	compensating, err := r.persist(ctx, Status{
		RequestID: current.RequestID, Target: current.Target, RecoveryTarget: recoveryTarget,
		State: StateCompensating, Phase: current.Phase, Topology: current.Topology, Reason: reason,
	})
	if err != nil {
		return Status{}, errors.Join(cause, err)
	}
	return r.runCompensation(ctx, compensating, cause)
}

func (r Runner) resumeCompensation(ctx context.Context, current Status) (Status, error) {
	expected, err := oppositeTarget(current.Target)
	if err != nil || current.RecoveryTarget != expected || current.Reason == "" {
		if err == nil {
			err = errors.New("front-door compensation journal is invalid")
		}
		return current, err
	}
	return r.runCompensation(ctx, current, fmt.Errorf("front-door transition previously failed: %s", current.Reason))
}

func (r Runner) runCompensation(ctx context.Context, current Status, originalCause error) (Status, error) {
	var lastErr error
	for attempts := 0; attempts < compensationPhaseBudget; attempts++ {
		if err := ctx.Err(); err != nil {
			return interruptedTransition(current, err)
		}
		topology, err := r.Platform.Topology(ctx)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			lastErr = fmt.Errorf("topology read: %w", err)
			if err := r.waitCompensationRetry(ctx); err != nil {
				return interruptedTransition(current, err)
			}
			continue
		}
		phase, done, err := NextPhase(current.RecoveryTarget, topology)
		if err != nil {
			return r.compensationFailed(ctx, current, originalCause, "topology_invalid", err)
		}
		if done {
			failed, persistErr := r.persist(ctx, Status{
				RequestID: current.RequestID, Target: current.Target, RecoveryTarget: current.RecoveryTarget,
				State: StateFailed, Phase: PhaseComplete, Topology: topology, Reason: current.Reason + "_compensated",
			})
			if persistErr != nil {
				return Status{}, errors.Join(originalCause, persistErr)
			}
			return failed, originalCause
		}
		current, err = r.persist(ctx, Status{
			RequestID: current.RequestID, Target: current.Target, RecoveryTarget: current.RecoveryTarget,
			State: StateCompensating, Phase: phase, Topology: topology, Reason: current.Reason,
		})
		if err != nil {
			return Status{}, errors.Join(originalCause, err)
		}
		deploymentID, next, err := r.execute(ctx, current.RecoveryTarget, phase)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			lastErr = fmt.Errorf("%s: %w", phase, err)
			if observed, observeErr := r.Platform.Topology(ctx); observeErr == nil {
				current, err = r.persist(ctx, Status{
					RequestID: current.RequestID, Target: current.Target, RecoveryTarget: current.RecoveryTarget,
					State: StateCompensating, Phase: phase, Topology: observed, DeploymentID: deploymentID, Reason: current.Reason,
				})
				if err != nil {
					return Status{}, errors.Join(originalCause, err)
				}
			} else if transitionInterrupted(ctx, observeErr) {
				return interruptedTransition(current, observeErr)
			} else {
				lastErr = errors.Join(lastErr, fmt.Errorf("topology observation: %w", observeErr))
			}
			if err := r.waitCompensationRetry(ctx); err != nil {
				return interruptedTransition(current, err)
			}
			continue
		}
		topology, err = r.Platform.Topology(ctx)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			lastErr = fmt.Errorf("post-effect topology read: %w", err)
			if err := r.waitCompensationRetry(ctx); err != nil {
				return interruptedTransition(current, err)
			}
			continue
		}
		current, err = r.persist(ctx, Status{
			RequestID: current.RequestID, Target: current.Target, RecoveryTarget: current.RecoveryTarget,
			State: StateCompensating, Phase: next, Topology: topology, DeploymentID: deploymentID, Reason: current.Reason,
		})
		if err != nil {
			return Status{}, errors.Join(originalCause, err)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("front-door compensation exceeded its finite phase budget")
	}
	return r.compensationFailed(ctx, current, originalCause, "budget_exhausted", lastErr)
}

func (r Runner) waitCompensationRetry(ctx context.Context) error {
	backoff := r.CompensationBackoff
	if backoff <= 0 {
		backoff = defaultCompensationBackoff
	}
	timer := time.NewTimer(backoff)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r Runner) compensationFailed(ctx context.Context, current Status, originalCause error, suffix string, compensationCause error) (Status, error) {
	failed, err := r.persist(ctx, Status{
		RequestID: current.RequestID, Target: current.Target, RecoveryTarget: current.RecoveryTarget,
		State: StateFailed, Phase: current.Phase, Topology: current.Topology,
		Reason: current.Reason + "_compensation_" + suffix,
	})
	combined := errors.Join(originalCause, fmt.Errorf("front-door compensation failed: %w", compensationCause))
	if err != nil {
		return Status{}, errors.Join(combined, err)
	}
	return failed, combined
}
