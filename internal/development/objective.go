package development

import (
	"errors"
	"regexp"
	"strings"
)

const (
	ObjectiveVersion        = 1
	MaxObjectiveSteps       = 64
	MaxAttemptsPerStep      = 32
	MaxObjectiveRecordBytes = 256 << 10
)

var (
	projectScopePattern = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
	targetScopePattern  = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$")
)

func ValidObjectiveID(value string) bool {
	return identityPattern.MatchString(strings.TrimSpace(value))
}

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
	StepCancelled StepState = "cancelled"
)

type StepSpec struct {
	StepID             string
	Requirements       []Requirement
	AcceptanceContract *CommandAcceptanceContract
}

type ObjectiveScope struct {
	Project string
	Target  string
	Anchor  WorkspaceAnchor
}

func NewObjectiveScope(project, target string) (ObjectiveScope, error) {
	scope := ObjectiveScope{
		Project: strings.ToLower(strings.TrimSpace(project)),
		Target:  strings.ToLower(strings.TrimSpace(target)),
	}
	if !scope.Bound() {
		return ObjectiveScope{}, errors.New("development objective scope is invalid")
	}
	return scope, nil
}

func (scope ObjectiveScope) Bound() bool {
	return projectScopePattern.MatchString(scope.Project) && targetScopePattern.MatchString(scope.Target) &&
		(scope.Anchor == (WorkspaceAnchor{}) || scope.Anchor.Valid())
}

func (scope ObjectiveScope) validOrEmpty() bool {
	return (scope == (ObjectiveScope{})) || scope.Bound()
}

type ObjectiveStep struct {
	StepID             string
	Requirements       []Requirement
	AcceptanceContract *CommandAcceptanceContract
	AcceptanceReceipt  *CommandAcceptanceReceipt
	Attempts           []ExecutionAttempt
	Provisioning       []ProvisioningAttempt
	State              StepState
}

type Objective struct {
	Version     int
	ObjectiveID string
	Revision    uint64
	State       ObjectiveState
	Scope       ObjectiveScope
	Policy      ResolutionPolicy
	Steps       []ObjectiveStep
}

func NewObjective(objectiveID string, policy ResolutionPolicy, specs []StepSpec) (Objective, error) {
	objectiveID = strings.TrimSpace(objectiveID)
	if !identityPattern.MatchString(objectiveID) || !policy.valid() || len(specs) == 0 || len(specs) > MaxObjectiveSteps {
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
		step := ObjectiveStep{
			StepID:       stepID,
			Requirements: requirements,
			State:        StepPlanned,
		}
		if spec.AcceptanceContract != nil {
			contract := spec.AcceptanceContract.clone()
			if !contract.validFor(policy, requirements) {
				return Objective{}, errors.New("development command acceptance contract does not match objective")
			}
			step.AcceptanceContract = &contract
		}
		steps = append(steps, step)
	}
	if !sameCommandAcceptanceContractMode(steps) {
		return Objective{}, errors.New("development objective cannot mix command and semantic acceptance")
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

func NewScopedObjective(objectiveID string, scope ObjectiveScope, policy ResolutionPolicy, specs []StepSpec) (Objective, error) {
	if !scope.Bound() {
		return Objective{}, errors.New("development objective scope is invalid")
	}
	objective, err := NewObjective(objectiveID, policy, specs)
	if err != nil {
		return Objective{}, err
	}
	objective.Scope = scope
	return objective, nil
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
	if step.State == StepRunning || step.State == StepSucceeded || activeProvisioning(step) || step.AcceptanceContract != nil {
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
	if step.AcceptanceContract != nil && strings.TrimSpace(sourceDigest) != step.AcceptanceContract.SourceDigest {
		return Objective{}, Resolution{}, errors.New("development command acceptance source digest changed")
	}
	if activeProvisioning(*step) {
		return Objective{}, Resolution{}, errors.New("development provisioning is still active")
	}
	if step.State == StepSucceeded {
		return Objective{}, Resolution{}, errors.New("development objective step already succeeded")
	}
	if len(step.Attempts) >= MaxAttemptsPerStep {
		return Objective{}, Resolution{}, errors.New("development objective attempt budget is exhausted")
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
			if len(environment.Capabilities.Missing(step.Requirements)) != 0 {
				return Objective{}, Resolution{}, errors.New("development objective previous environment no longer satisfies requirements")
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

func (objective Objective) RejectAttempt(stepID string, class FailureClass) (Objective, ContinuationAction, error) {
	index := objective.stepIndex(stepID)
	if index < 0 || objective.terminal() {
		return Objective{}, "", errors.New("development objective cannot reject attempt")
	}
	action, ok := ContinuationForFailure(class)
	if !ok || !preflightFailureAllowed(class) {
		return Objective{}, "", errors.New("development objective preflight failure class is invalid")
	}
	copy := objective.clone()
	step := &copy.Steps[index]
	if len(step.Attempts) == 0 {
		return Objective{}, "", errors.New("development objective attempt is missing")
	}
	last := step.Attempts[len(step.Attempts)-1]
	failed, err := last.RejectPreflight(class)
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
	if objective.State != ObjectiveAcceptancePending || !sameCommandAcceptanceContractMode(objective.Steps) ||
		len(objective.Steps) == 0 || objective.Steps[0].AcceptanceContract != nil {
		return Objective{}, errors.New("development objective is not ready for acceptance")
	}
	copy := objective.clone()
	copy.State = ObjectiveAccepted
	copy.Revision++
	return copy, nil
}

// AcceptWithEvidence accepts a command-only objective after a trusted internal
// dispatcher has authenticated each terminal Edge operation and constructed an
// exact receipt for its successful attempt. It is not a public caller receipt
// path and does not evaluate natural-language goals.
func (objective Objective) AcceptWithEvidence(receipts []CommandAcceptanceReceipt) (Objective, error) {
	if !objective.Valid() || objective.State != ObjectiveAcceptancePending || len(objective.Steps) == 0 ||
		!sameCommandAcceptanceContractMode(objective.Steps) || objective.Steps[0].AcceptanceContract == nil {
		return Objective{}, errors.New("development objective is not ready for command acceptance")
	}
	if len(receipts) != len(objective.Steps) {
		return Objective{}, errors.New("development command acceptance evidence is incomplete")
	}
	copy := objective.clone()
	seenOperations := make(map[string]struct{}, len(receipts))
	for _, receipt := range receipts {
		index := objective.stepIndex(receipt.StepID)
		if index < 0 || !receipt.validFor(objective, index) {
			return Objective{}, errors.New("development command acceptance evidence does not match objective")
		}
		if _, duplicate := seenOperations[receipt.OperationID]; duplicate {
			return Objective{}, errors.New("development command acceptance evidence is duplicated")
		}
		seenOperations[receipt.OperationID] = struct{}{}
		copy.Steps[index].AcceptanceReceipt = receipt.clonePointer()
	}
	copy.State = ObjectiveAccepted
	copy.Revision++
	if !copy.Valid() {
		return Objective{}, errors.New("development command acceptance evidence is invalid")
	}
	return copy, nil
}

func (objective Objective) Cancel() (Objective, error) {
	if objective.terminal() {
		return Objective{}, errors.New("development objective is terminal")
	}
	copy := objective.clone()
	for index := range copy.Steps {
		step := &copy.Steps[index]
		if len(step.Provisioning) != 0 {
			last := len(step.Provisioning) - 1
			if step.Provisioning[last].State == ProvisioningPlanned || step.Provisioning[last].State == ProvisioningQueued {
				cancelled, err := step.Provisioning[last].Cancel()
				if err != nil {
					return Objective{}, err
				}
				step.Provisioning[last] = cancelled
			}
		}
		if step.State == StepSucceeded || step.State == StepFailed || step.State == StepCancelled {
			continue
		}
		if len(step.Attempts) == 0 {
			step.State = StepCancelled
			continue
		}
		last := step.Attempts[len(step.Attempts)-1]
		if last.State != AttemptPlanned && last.State != AttemptRunning {
			return Objective{}, errors.New("development objective cannot cancel attempt")
		}
		cancelled, err := last.Transition(AttemptCancelled)
		if err != nil {
			return Objective{}, err
		}
		step.Attempts[len(step.Attempts)-1] = cancelled
		step.State = StepCancelled
	}
	copy.State = ObjectiveCancelled
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
		if step.AcceptanceContract != nil {
			contract := step.AcceptanceContract.clone()
			copy.Steps[index].AcceptanceContract = &contract
		}
		if step.AcceptanceReceipt != nil {
			receipt := step.AcceptanceReceipt.clone()
			copy.Steps[index].AcceptanceReceipt = &receipt
		}
		copy.Steps[index].Attempts = append([]ExecutionAttempt(nil), step.Attempts...)
		copy.Steps[index].Provisioning = make([]ProvisioningAttempt, len(step.Provisioning))
		for j, provision := range step.Provisioning {
			copy.Steps[index].Provisioning[j] = cloneProvisionAttempt(provision)
		}
	}
	return copy
}
