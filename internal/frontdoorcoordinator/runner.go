package frontdoorcoordinator

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrDurableState          = errors.New("front-door coordinator durable state failed")
	ErrStatusPublish         = errors.New("front-door coordinator status publication failed")
	ErrTransitionInterrupted = errors.New("front-door coordinator transition interrupted")
)

const (
	defaultStatusPublishAttempts = 5
	defaultStatusPublishBackoff  = time.Second
)

type Platform interface {
	Topology(context.Context) (Topology, error)
	SetBackendDomains(context.Context, string) (string, error)
	ConfigureFront(context.Context, string, string) (string, error)
	ProbeBackend(context.Context, string) error
	ProbeFront(context.Context, string) error
	PublishStatus(context.Context, Status) error
}

type Runner struct {
	Platform            Platform
	Journal             *Journal
	RequestID           string
	PublishAttempts     int
	PublishBackoff      time.Duration
	CompensationBackoff time.Duration
}

func (r Runner) Run(ctx context.Context, target Target) (Status, error) {
	if r.Platform == nil || r.Journal == nil {
		return Status{}, errors.New("front-door coordinator runner is not configured")
	}
	if target != TargetCutover && target != TargetRollback {
		return Status{}, errors.New("front-door coordinator runner target must be cutover or rollback")
	}
	if err := ValidateRequestID(r.RequestID); err != nil {
		return Status{}, err
	}
	current, err := r.Journal.Read()
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrDurableState, err)
	}
	if current.RequestID == r.RequestID {
		if current.Target != target {
			return current, errors.New("front-door coordinator request target changed")
		}
		switch current.State {
		case StateQueued, StateRunning:
		case StateCompensating:
			return r.resumeCompensation(ctx, current)
		case StateSucceeded:
			if err := r.publishStatus(ctx, current); err != nil {
				return Status{}, err
			}
			return current, nil
		case StateFailed:
			if err := r.publishStatus(ctx, current); err != nil {
				return Status{}, err
			}
			return current, errors.New("front-door coordinator request already failed; dispatch a new reviewed request")
		default:
			return current, errors.New("front-door coordinator journal state is invalid for the active request")
		}
	} else {
		if current.State == StateQueued || current.State == StateRunning || current.State == StateCompensating {
			return current, errors.New("a different front-door coordinator request is already active")
		}
		current, err = r.persist(ctx, Status{RequestID: r.RequestID, Target: target, State: StateQueued})
		if err != nil {
			return Status{}, err
		}
	}

	for attempts := 0; attempts < 12; attempts++ {
		if err := ctx.Err(); err != nil {
			return interruptedTransition(current, err)
		}
		topology, err := r.Platform.Topology(ctx)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			if err := r.waitCompensationRetry(ctx); err != nil {
				return interruptedTransition(current, err)
			}
			continue
		}
		phase, done, err := NextPhase(target, topology)
		if err != nil {
			if current.Phase == PhaseNone {
				return r.fail(ctx, current, "topology_invalid", err)
			}
			return r.compensate(ctx, current, "topology_invalid", err)
		}
		if done {
			return r.persist(ctx, Status{RequestID: r.RequestID, Target: target, State: StateSucceeded, Phase: PhaseComplete, Topology: topology})
		}
		current, err = r.persist(ctx, Status{RequestID: r.RequestID, Target: target, State: StateRunning, Phase: phase, Topology: topology})
		if err != nil {
			return Status{}, err
		}
		deploymentID, next, err := r.execute(ctx, target, phase)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			return r.compensate(ctx, current, string(phase)+"_failed", err)
		}
		topology, err = r.Platform.Topology(ctx)
		if err != nil {
			if transitionInterrupted(ctx, err) {
				return interruptedTransition(current, err)
			}
			return r.compensate(ctx, current, "topology_read_failed", err)
		}
		current, err = r.persist(ctx, Status{
			RequestID: r.RequestID, Target: target, State: StateRunning, Phase: next, Topology: topology, DeploymentID: deploymentID,
		})
		if err != nil {
			return Status{}, err
		}
	}
	budgetErr := errors.New("front-door transition exceeded its finite phase budget")
	if current.Phase == PhaseNone {
		return r.fail(ctx, current, "topology_read_budget_exhausted", budgetErr)
	}
	return r.compensate(ctx, current, "transition_budget_exhausted", budgetErr)
}

func transitionInterrupted(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func interruptedTransition(current Status, cause error) (Status, error) {
	return current, errors.Join(ErrTransitionInterrupted, cause)
}

func (r Runner) execute(ctx context.Context, target Target, phase Phase) (string, Phase, error) {
	switch target {
	case TargetCutover:
		switch phase {
		case PhaseAddBackendOrigin:
			deploymentID, err := r.Platform.SetBackendDomains(ctx, FrontPublicOrigin+","+BackendOrigin)
			if err == nil {
				err = r.Platform.ProbeBackend(ctx, BackendOrigin)
			}
			return deploymentID, PhaseSwitchFrontBackend, err
		case PhaseSwitchFrontBackend:
			deploymentID, err := r.Platform.ConfigureFront(ctx, FrontTemporaryOrigin, BackendOrigin)
			if err == nil {
				err = r.Platform.ProbeFront(ctx, FrontTemporaryOrigin)
			}
			return deploymentID, PhaseReleasePublicBackend, err
		case PhaseReleasePublicBackend:
			deploymentID, err := r.Platform.SetBackendDomains(ctx, BackendOrigin)
			if err == nil {
				err = r.Platform.ProbeBackend(ctx, BackendOrigin)
			}
			return deploymentID, PhaseAssignPublicFront, err
		case PhaseAssignPublicFront:
			deploymentID, err := r.Platform.ConfigureFront(ctx, FrontPublicOrigin, BackendOrigin)
			if err == nil {
				err = r.Platform.ProbeFront(ctx, FrontPublicOrigin)
			}
			return deploymentID, PhaseComplete, err
		}
	case TargetRollback:
		switch phase {
		case PhaseMoveFrontTemporary:
			deploymentID, err := r.Platform.ConfigureFront(ctx, FrontTemporaryOrigin, BackendOrigin)
			if err == nil {
				err = r.Platform.ProbeFront(ctx, FrontTemporaryOrigin)
			}
			return deploymentID, PhaseRestorePublicBackend, err
		case PhaseRestorePublicBackend:
			deploymentID, err := r.Platform.SetBackendDomains(ctx, BackendOrigin+","+FrontPublicOrigin)
			if err == nil {
				err = r.Platform.ProbeBackend(ctx, FrontPublicOrigin)
			}
			return deploymentID, PhaseSwitchFrontPublicBackend, err
		case PhaseSwitchFrontPublicBackend:
			deploymentID, err := r.Platform.ConfigureFront(ctx, FrontTemporaryOrigin, FrontPublicOrigin)
			if err == nil {
				err = r.Platform.ProbeFront(ctx, FrontTemporaryOrigin)
			}
			return deploymentID, PhaseRemoveAlternateBackend, err
		case PhaseRemoveAlternateBackend:
			deploymentID, err := r.Platform.SetBackendDomains(ctx, FrontPublicOrigin)
			if err == nil {
				err = r.Platform.ProbeBackend(ctx, FrontPublicOrigin)
			}
			return deploymentID, PhaseComplete, err
		}
	}
	return "", PhaseNone, fmt.Errorf("unsupported front-door transition phase %s for target %s", phase, target)
}

func (r Runner) persist(ctx context.Context, status Status) (Status, error) {
	persisted, err := r.Journal.Advance(status)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrDurableState, err)
	}
	if err := r.publishStatus(ctx, persisted); err != nil {
		return Status{}, err
	}
	return persisted, nil
}

func (r Runner) publishStatus(ctx context.Context, status Status) error {
	attempts := r.PublishAttempts
	if attempts <= 0 {
		attempts = defaultStatusPublishAttempts
	}
	backoff := r.PublishBackoff
	if backoff <= 0 {
		backoff = defaultStatusPublishBackoff
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := r.Platform.PublishStatus(ctx, status); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == attempts {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), ErrStatusPublish, lastErr)
		case <-timer.C:
		}
	}
	return errors.Join(fmt.Errorf("%w after %d attempts", ErrStatusPublish, attempts), lastErr)
}

func (r Runner) fail(ctx context.Context, current Status, reason string, cause error) (Status, error) {
	failed, err := r.persist(ctx, Status{
		RequestID: current.RequestID, Target: current.Target, State: StateFailed, Phase: current.Phase, Topology: current.Topology, Reason: reason,
	})
	if err != nil {
		return Status{}, errors.Join(cause, err)
	}
	return failed, cause
}
