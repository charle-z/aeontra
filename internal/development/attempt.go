package development

import (
	"errors"
	"regexp"
	"strings"
)

var sourceDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type FailureClass string

const (
	FailureCode                 FailureClass = "code_failure"
	FailureCapabilityMissing    FailureClass = "capability_missing"
	FailureCapabilityDrift      FailureClass = "capability_drift"
	FailureDependencyMissing    FailureClass = "dependency_missing"
	FailureServiceUnavailable   FailureClass = "service_unavailable"
	FailureKernelSemantics      FailureClass = "kernel_semantics_missing"
	FailureCIContract           FailureClass = "ci_contract_missing"
	FailureFilesystemMapping    FailureClass = "filesystem_mapping_missing"
	FailurePlatformMismatch     FailureClass = "platform_mismatch"
	FailureExternalTransient    FailureClass = "external_transient"
	FailurePolicyDenied         FailureClass = "policy_denied"
	FailureReconciliationNeeded FailureClass = "reconciliation_required"
)

type ContinuationAction string

const (
	ActionFixCode              ContinuationAction = "fix_code"
	ActionProvisionOrMigrate   ContinuationAction = "provision_or_migrate"
	ActionRetrySameEnvironment ContinuationAction = "retry_same_environment"
	ActionStopPolicy           ContinuationAction = "stop_policy"
	ActionReconcile            ContinuationAction = "reconcile"
)

func ContinuationForFailure(class FailureClass) (ContinuationAction, bool) {
	switch class {
	case FailureCode:
		return ActionFixCode, true
	case FailureCapabilityMissing, FailureCapabilityDrift, FailureDependencyMissing,
		FailureServiceUnavailable, FailureKernelSemantics, FailureCIContract,
		FailureFilesystemMapping, FailurePlatformMismatch:
		return ActionProvisionOrMigrate, true
	case FailureExternalTransient:
		return ActionRetrySameEnvironment, true
	case FailurePolicyDenied:
		return ActionStopPolicy, true
	case FailureReconciliationNeeded:
		return ActionReconcile, true
	default:
		return "", false
	}
}

type AttemptState string

const (
	AttemptPlanned   AttemptState = "planned"
	AttemptRunning   AttemptState = "running"
	AttemptSucceeded AttemptState = "succeeded"
	AttemptFailed    AttemptState = "failed"
	AttemptCancelled AttemptState = "cancelled"
)

// ExecutionAttempt binds one immutable source identity and environment
// attestation to one step. Retrying with changed authority or source always
// creates another attempt; an existing attempt is never retargeted.
type ExecutionAttempt struct {
	AttemptID             string
	StepID                string
	ParentAttemptID       string
	SourceDigest          string
	EnvironmentID         string
	EnvironmentDigest     string
	EnvironmentGeneration uint64
	Class                 ExecutionClass
	Tier                  AuthorityTier
	State                 AttemptState
	Failure               FailureClass
}

func NewExecutionAttempt(attemptID, stepID, sourceDigest string, environment EnvironmentAttestation) (ExecutionAttempt, error) {
	attemptID = strings.TrimSpace(attemptID)
	stepID = strings.TrimSpace(stepID)
	sourceDigest = strings.ToLower(strings.TrimSpace(sourceDigest))
	if !identityPattern.MatchString(attemptID) || !identityPattern.MatchString(stepID) ||
		!sourceDigestPattern.MatchString(sourceDigest) || !environment.Valid() {
		return ExecutionAttempt{}, errors.New("development execution attempt is invalid")
	}
	tier, _ := environment.Class.Tier()
	return ExecutionAttempt{
		AttemptID:             attemptID,
		StepID:                stepID,
		SourceDigest:          sourceDigest,
		EnvironmentID:         environment.EnvironmentID,
		EnvironmentDigest:     environment.Digest,
		EnvironmentGeneration: environment.Generation,
		Class:                 environment.Class,
		Tier:                  tier,
		State:                 AttemptPlanned,
	}, nil
}

func (attempt ExecutionAttempt) Transition(next AttemptState) (ExecutionAttempt, error) {
	if !attempt.validIdentity() {
		return ExecutionAttempt{}, errors.New("development execution attempt is invalid")
	}
	if next == attempt.State {
		return attempt, nil
	}
	allowed := false
	switch attempt.State {
	case AttemptPlanned:
		allowed = next == AttemptRunning || next == AttemptCancelled
	case AttemptRunning:
		allowed = next == AttemptSucceeded || next == AttemptFailed || next == AttemptCancelled
	}
	if !allowed {
		return ExecutionAttempt{}, errors.New("development execution attempt transition is invalid")
	}
	copy := attempt
	copy.State = next
	if next != AttemptFailed {
		copy.Failure = ""
	}
	return copy, nil
}

func (attempt ExecutionAttempt) Fail(class FailureClass) (ExecutionAttempt, error) {
	if _, ok := ContinuationForFailure(class); !ok || attempt.State != AttemptRunning {
		return ExecutionAttempt{}, errors.New("development execution attempt failure is invalid")
	}
	copy := attempt
	copy.State = AttemptFailed
	copy.Failure = class
	return copy, nil
}

// ContinueExecutionAttempt creates the only legal next attempt for a classified
// failure. Code failures require changed source at unchanged authority.
// Capability failures require a changed environment attestation. Transient
// external failures may retry the exact source/environment pair.
func ContinueExecutionAttempt(newAttemptID, sourceDigest string, previous ExecutionAttempt, environment EnvironmentAttestation) (ExecutionAttempt, error) {
	if previous.State != AttemptFailed || !previous.validIdentity() || !environment.Valid() {
		return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
	}
	action, ok := ContinuationForFailure(previous.Failure)
	if !ok {
		return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
	}
	sourceDigest = strings.ToLower(strings.TrimSpace(sourceDigest))
	if !sourceDigestPattern.MatchString(sourceDigest) {
		return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
	}
	switch action {
	case ActionFixCode:
		if sourceDigest == previous.SourceDigest || environment.Digest != previous.EnvironmentDigest {
			return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
		}
	case ActionProvisionOrMigrate:
		if environment.Digest == previous.EnvironmentDigest {
			return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
		}
	case ActionRetrySameEnvironment:
		if sourceDigest != previous.SourceDigest || environment.Digest != previous.EnvironmentDigest {
			return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
		}
	default:
		return ExecutionAttempt{}, errors.New("development execution attempt cannot continue")
	}
	next, err := NewExecutionAttempt(newAttemptID, previous.StepID, sourceDigest, environment)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	next.ParentAttemptID = previous.AttemptID
	return next, nil
}

func MigrateExecutionAttempt(newAttemptID, sourceDigest string, previous ExecutionAttempt, environment EnvironmentAttestation) (ExecutionAttempt, error) {
	action, ok := ContinuationForFailure(previous.Failure)
	if !ok || action != ActionProvisionOrMigrate {
		return ExecutionAttempt{}, errors.New("development execution attempt cannot migrate")
	}
	return ContinueExecutionAttempt(newAttemptID, sourceDigest, previous, environment)
}

func (attempt ExecutionAttempt) validIdentity() bool {
	if !identityPattern.MatchString(attempt.AttemptID) || !identityPattern.MatchString(attempt.StepID) ||
		!identityPattern.MatchString(attempt.EnvironmentID) || !sourceDigestPattern.MatchString(attempt.SourceDigest) ||
		attempt.EnvironmentGeneration == 0 || attempt.EnvironmentDigest == "" {
		return false
	}
	tier, ok := attempt.Class.Tier()
	return ok && tier == attempt.Tier
}
