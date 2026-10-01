package development

import "errors"

// ValidateTransition proves that a new durable objective revision extends the
// previous revision without rewriting policy, requirements, source identity,
// environment identity, or historical attempts.
func ValidateTransition(previous, next Objective) error {
	if !previous.Valid() || !next.Valid() ||
		previous.Version != next.Version ||
		previous.ObjectiveID != next.ObjectiveID ||
		previous.Scope != next.Scope ||
		next.Revision != previous.Revision+1 ||
		previous.Policy.MaxTier() != next.Policy.MaxTier() ||
		!sameExecutionClasses(previous.Policy.AllowedClasses(), next.Policy.AllowedClasses()) ||
		len(previous.Steps) != len(next.Steps) ||
		!validObjectiveStateTransition(previous.State, next.State) {
		return errors.New("development objective transition is invalid")
	}
	for index := range previous.Steps {
		before := previous.Steps[index]
		after := next.Steps[index]
		if before.StepID != after.StepID ||
			!requirementsSubset(before.Requirements, after.Requirements) ||
			!validStepStateTransition(before.State, after.State) ||
			len(after.Attempts) < len(before.Attempts) ||
			len(after.Attempts) > len(before.Attempts)+1 {
			return errors.New("development objective transition is invalid")
		}
		if len(after.Attempts) == len(before.Attempts)+1 {
			for attemptIndex := range before.Attempts {
				if before.Attempts[attemptIndex] != after.Attempts[attemptIndex] {
					return errors.New("development objective transition is invalid")
				}
			}
			if len(before.Attempts) != 0 {
				previousAttempt := before.Attempts[len(before.Attempts)-1]
				added := after.Attempts[len(after.Attempts)-1]
				if previousAttempt.State != AttemptFailed ||
					added.ParentAttemptID != previousAttempt.AttemptID ||
					added.State != AttemptPlanned {
					return errors.New("development objective transition is invalid")
				}
			} else if after.Attempts[0].ParentAttemptID != "" || after.Attempts[0].State != AttemptPlanned {
				return errors.New("development objective transition is invalid")
			}
			continue
		}
		if len(before.Attempts) == 0 {
			continue
		}
		for attemptIndex := 0; attemptIndex < len(before.Attempts)-1; attemptIndex++ {
			if before.Attempts[attemptIndex] != after.Attempts[attemptIndex] {
				return errors.New("development objective transition is invalid")
			}
		}
		if !validAttemptRevision(before.Attempts[len(before.Attempts)-1], after.Attempts[len(after.Attempts)-1]) {
			return errors.New("development objective transition is invalid")
		}
	}
	return nil
}

func requirementsSubset(before, after []Requirement) bool {
	index := 0
	for _, requirement := range after {
		if index < len(before) && before[index].ID == requirement.ID {
			index++
		}
	}
	return index == len(before)
}

func validAttemptRevision(before, after ExecutionAttempt) bool {
	immutableBefore := before
	immutableAfter := after
	immutableBefore.State, immutableAfter.State = "", ""
	immutableBefore.Failure, immutableAfter.Failure = "", ""
	if immutableBefore != immutableAfter {
		return false
	}
	if before.State == after.State && before.Failure == after.Failure {
		return true
	}
	switch before.State {
	case AttemptPlanned:
		if (after.State == AttemptRunning || after.State == AttemptCancelled) && after.Failure == "" {
			return true
		}
		return after.State == AttemptFailed && preflightFailureAllowed(after.Failure)
	case AttemptRunning:
		if after.State == AttemptSucceeded && after.Failure == "" {
			return true
		}
		if after.State == AttemptFailed {
			_, ok := ContinuationForFailure(after.Failure)
			return ok
		}
		return after.State == AttemptCancelled && after.Failure == ""
	default:
		return false
	}
}

func validObjectiveStateTransition(before, after ObjectiveState) bool {
	if before == after {
		return true
	}
	switch before {
	case ObjectivePlanned:
		return after == ObjectiveRunning || after == ObjectiveCancelled
	case ObjectiveRunning:
		return after == ObjectiveAcceptancePending ||
			after == ObjectiveReconciliationRequired ||
			after == ObjectiveFailed ||
			after == ObjectiveCancelled
	case ObjectiveAcceptancePending:
		return after == ObjectiveAccepted || after == ObjectiveCancelled
	case ObjectiveReconciliationRequired:
		return after == ObjectiveRunning || after == ObjectiveFailed || after == ObjectiveCancelled
	default:
		return false
	}
}

func validStepStateTransition(before, after StepState) bool {
	if before == after {
		return true
	}
	switch before {
	case StepPlanned:
		return after == StepRunning || after == StepFailed || after == StepCancelled
	case StepRunning:
		return after == StepSucceeded || after == StepFailed || after == StepCancelled
	case StepFailed:
		return after == StepPlanned
	default:
		return false
	}
}
