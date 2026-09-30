package development

import (
	"errors"
	"strings"
)

const ObjectiveVersion = 1

type ObjectiveState string

const (
	ObjectivePlanned                ObjectiveState = "planned"
	ObjectiveRunning                ObjectiveState = "running"
	ObjectiveAcceptancePending      ObjectiveState = "acceptance_pending"
	ObjectiveAccepted               ObjectiveState = "accepted"
	ObjectiveReconciliationRequired ObjectiveState = "reconciliation_required"
	ObjectiveFailed                 ObjectiveState = "failed"
	ObjectiveCancelled              ObjectiveState = "cancelled"
)

type StepState string

const (
	StepPlanned   StepState = "planned"
	StepRunning   StepState = "running"
	StepSucceeded StepState = "succeeded"
	StepFailed    StepState = "failed"
)

type StepSpec struct {
	StepID       string
	Requirements []Requirement
}

type ObjectiveStep struct {
	StepID       string
	Requirements []Requirement
	Attempts     []ExecutionAttempt
	State        StepState
}

type Objective struct {
	Version     int
	ObjectiveID string
	Revision    uint64
	State       ObjectiveState
	Policy      ResolutionPolicy
	Steps       []ObjectiveStep
}

func NewObjective(objectiveID string, policy ResolutionPolicy, specs []StepSpec) (Objective, error) {
	objectiveID = strings.TrimSpace(objectiveID)
	if !identityPattern.MatchString(objectiveID) || !policy.valid() || len(specs) == 0 || len(specs) > 64 {
		return Objective{}, errors.New("development objective is invalid")
	}
	seen := make(map[string]struct{}, len(specs))
	steps := make([]ObjectiveStep, 0, len(specs))
	for _, spec := range specs {
		stepID := strings.TrimSpace(spec.StepID)
		if !identityPattern.MatchString(stepID) {
			return Objective{}, errors.New("development objective step is invalid")
		}
		if _, duplicate := seen[stepID]; duplicate {
			return Objective{}, errors.New("development objective step is duplicated")
		}
		seen[stepID] = struct{}{}
		requirements, err := normalizeRequirementList(spec.Requirements)
		if err != nil {
			return Objective{}, err
		}
		steps = append(steps, ObjectiveStep{
			StepID:       stepID,
			Requirements: requirements,
			State:        StepPlanned,
		})
	}
	return Objective{
		Version:     ObjectiveVersion,
		ObjectiveID: objectiveID,
		Revision:    1,
		State:       ObjectivePlanned,
		Policy:      policy,
		Steps:       steps,
	}, nil
}

// RefineRequirements only adds requirements. Runtime failure diagnosis may
// discover a missing kernel/CI/service property, but it cannot silently remove
// an earlier requirement or enlarge the objective's authority policy.
func (objective Objective) RefineRequirements(stepID string, additional []Requirement) (Objective, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, errors.New("development objective cannot refine requirements")
	}
	step := objective.Steps[index]
	if step.State == StepRunning || step.State == StepSucceeded {
		return Objective{}, errors.New("development objective cannot refine active or succeeded step")
	}
	normalized, err := normalizeRequirementList(append(append([]Requirement(nil), step.Requirements...), additional...))
	if err != nil {
		return Objective{}, err
	}
	if sameRequirements(step.Requirements, normalized) {
		return objective, nil
	}
	copy := objective.clone()
	copy.Steps[index].Requirements = normalized
	copy.Revision++
	return copy, nil
}

// PlanAttempt resolves one step inside the objective's immutable policy envelope.
// Code and transient retries stay on the exact prior environment. Capability
// failures may resolve to a changed attestation or a higher allowed class.
func (objective Objective) PlanAttempt(stepID, attemptID, sourceDigest string, candidates []EnvironmentAttestation) (Objective, Resolution, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, Resolution{}, errors.New("development objective cannot plan attempt")
	}
	copy := objective.clone()
	step := &copy.Steps[index]
	if step.State == StepSucceeded {
		return Objective{}, Resolution{}, errors.New("development objective step already succeeded")
	}

	var (
		resolution Resolution
		err        error
	)
	if len(step.Attempts) == 0 {
		resolution, err = Resolve(step.Requirements, nil, candidates, copy.Policy)
		if err != nil {
			return Objective{}, Resolution{}, err
		}
		attempt, createErr := NewExecutionAttempt(attemptID, step.StepID, sourceDigest, resolution.Environment)
		if createErr != nil {
			return Objective{}, Resolution{}, createErr
		}
		step.Attempts = append(step.Attempts, attempt)
	} else {
		previous := step.Attempts[len(step.Attempts)-1]
		if previous.State != AttemptFailed {
			return Objective{}, Resolution{}, errors.New("development objective previous attempt is not failed")
		}
		action, ok := ContinuationForFailure(previous.Failure)
		if !ok || action == ActionStopPolicy || action == ActionReconcile {
			return Objective{}, Resolution{}, errors.New("development objective failure requires external resolution")
		}
		switch action {
		case ActionFixCode, ActionRetrySameEnvironment:
			environment, found := exactEnvironmentByDigest(candidates, previous.EnvironmentDigest)
			if !found || !copy.Policy.Allows(environment) {
				return Objective{}, Resolution{}, errors.New("development objective previous environment is unavailable")
			}
			resolution = Resolution{Environment: environment}
		case ActionProvisionOrMigrate:
			resolution, err = Resolve(step.Requirements, nil, candidates, copy.Policy)
			if err != nil {
				return Objective{}, Resolution{}, err
			}
		}
		attempt, continueErr := ContinueExecutionAttempt(attemptID, sourceDigest, previous, resolution.Environment)
		if continueErr != nil {
			return Objective{}, Resolution{}, continueErr
		}
		resolution.Migrated = previous.EnvironmentDigest != resolution.Environment.Digest
		step.Attempts = append(step.Attempts, attempt)
	}
	step.State = StepPlanned
	copy.State = ObjectiveRunning
	copy.Revision++
	return copy, resolution, nil
}

func (objective Objective) StartAttempt(stepID string) (Objective, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, errors.New("development objective cannot start attempt")
	}
	copy := objective.clone()
	step := &copy.Steps[index]
	if len(step.Attempts) == 0 {
		return Objective{}, errors.New("development objective attempt is missing")
	}
	last := step.Attempts[len(step.Attempts)-1]
	next, err := last.Transition(AttemptRunning)
	if err != nil {
		return Objective{}, err
	}
	step.Attempts[len(step.Attempts)-1] = next
	step.State = StepRunning
	copy.State = ObjectiveRunning
	copy.Revision++
	return copy, nil
}

func (objective Objective) FailAttempt(stepID string, class FailureClass) (Objective, ContinuationAction, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, "", errors.New("development objective cannot fail attempt")
	}
	action, ok := ContinuationForFailure(class)
	if !ok {
		return Objective{}, "", errors.New("development objective failure class is invalid")
	}
	copy := objective.clone()
	step := &copy.Steps[index]
	if len(step.Attempts) == 0 {
		return Objective{}, "", errors.New("development objective attempt is missing")
	}
	last := step.Attempts[len(step.Attempts)-1]
	failed, err := last.Fail(class)
	if err != nil {
		return Objective{}, "", err
	}
	step.Attempts[len(step.Attempts)-1] = failed
	step.State = StepFailed
	switch action {
	case ActionReconcile:
		copy.State = ObjectiveReconciliationRequired
	case ActionStopPolicy:
		copy.State = ObjectiveFailed
	default:
		copy.State = ObjectiveRunning
	}
	copy.Revision++
	return copy, action, nil
}

func (objective Objective) CompleteAttempt(stepID string) (Objective, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, errors.New("development objective cannot complete attempt")
	}
	copy := objective.clone()
	step := &copy.Steps[index]
	if len(step.Attempts) == 0 {
		return Objective{}, errors.New("development objective attempt is missing")
	}
	last := step.Attempts[len(step.Attempts)-1]
	completed, err := last.Transition(AttemptSucceeded)
	if err != nil {
		return Objective{}, err
	}
	step.Attempts[len(step.Attempts)-1] = completed
	step.State = StepSucceeded
	allSucceeded := true
	for _, candidate := range copy.Steps {
		allSucceeded = allSucceeded && candidate.State == StepSucceeded
	}
	if allSucceeded {
		copy.State = ObjectiveAcceptancePending
	} else {
		copy.State = ObjectiveRunning
	}
	copy.Revision++
	return copy, nil
}

func (objective Objective) Accept() (Objective, error) {
	if objective.State != ObjectiveAcceptancePending {
		return Objective{}, errors.New("development objective is not ready for acceptance")
	}
	copy := objective.clone()
	copy.State = ObjectiveAccepted
	copy.Revision++
	return copy, nil
}

func exactEnvironmentByDigest(candidates []EnvironmentAttestation, digest string) (EnvironmentAttestation, bool) {
	for _, candidate := range candidates {
		if candidate.Valid() && candidate.Digest == digest {
			return candidate, true
		}
	}
	return EnvironmentAttestation{}, false
}

func sameRequirements(left, right []Requirement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}

func (objective Objective) stepIndex(stepID string) int {
	stepID = strings.TrimSpace(stepID)
	for index := range objective.Steps {
		if objective.Steps[index].StepID == stepID {
			return index
		}
	}
	return -1
}

func (objective Objective) terminal() bool {
	return objective.State == ObjectiveAccepted || objective.State == ObjectiveFailed || objective.State == ObjectiveCancelled
}

func (objective Objective) clone() Objective {
	copy := objective
	copy.Policy.allowed = append([]ExecutionClass(nil), objective.Policy.allowed...)
	copy.Steps = make([]ObjectiveStep, len(objective.Steps))
	for index, step := range objective.Steps {
		copy.Steps[index] = step
		copy.Steps[index].Requirements = append([]Requirement(nil), step.Requirements...)
		copy.Steps[index].Attempts = append([]ExecutionAttempt(nil), step.Attempts...)
	}
	return copy
}
