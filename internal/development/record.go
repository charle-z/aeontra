package development

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const objectiveRecordDigestDomain = "aeontra-development-objective-record-v1\x00"

type objectiveRecord struct {
	Version     int                   `json:"version"`
	ObjectiveID string                `json:"objective_id"`
	Revision    uint64                `json:"revision"`
	State       ObjectiveState        `json:"state"`
	Scope       *objectiveScopeRecord `json:"scope,omitempty"`
	Policy      objectivePolicyRecord `json:"policy"`
	Steps       []objectiveStepRecord `json:"steps"`
}

type objectiveScopeRecord struct {
	Project string           `json:"project"`
	Target  string           `json:"target"`
	Anchor  *WorkspaceAnchor `json:"anchor,omitempty"`
}

type objectivePolicyRecord struct {
	MaxTier        AuthorityTier    `json:"max_tier"`
	AllowedClasses []ExecutionClass `json:"allowed_classes"`
}

type objectiveStepRecord struct {
	StepID             string                           `json:"step_id"`
	State              StepState                        `json:"state"`
	Requirements       []string                         `json:"requirements"`
	Attempts           []objectiveAttemptRecord         `json:"attempts"`
	Provisioning       []ProvisioningAttempt            `json:"provisioning,omitempty"`
	AcceptanceContract *commandAcceptanceContractRecord `json:"acceptance_contract,omitempty"`
	AcceptanceReceipt  *CommandAcceptanceReceipt        `json:"acceptance_receipt,omitempty"`
}

type commandAcceptanceContractRecord struct {
	CommandDigest        string   `json:"command_digest"`
	SourceDigest         string   `json:"source_digest"`
	PrivateBodyRef       string   `json:"private_body_ref"`
	PrivateBodyDigest    string   `json:"private_body_digest"`
	RequiredCapabilities []string `json:"required_capabilities"`
	ArtifactRefs         []string `json:"artifact_refs,omitempty"`
}

type objectiveAttemptRecord struct {
	AttemptID             string         `json:"attempt_id"`
	ParentAttemptID       string         `json:"parent_attempt_id,omitempty"`
	SourceDigest          string         `json:"source_digest"`
	EnvironmentID         string         `json:"environment_id"`
	EnvironmentDigest     string         `json:"environment_digest"`
	EnvironmentGeneration uint64         `json:"environment_generation"`
	Class                 ExecutionClass `json:"execution_class"`
	Tier                  AuthorityTier  `json:"authority_tier"`
	State                 AttemptState   `json:"state"`
	Failure               FailureClass   `json:"failure_class,omitempty"`
}

func (objective Objective) Valid() bool {
	return validateObjectiveSnapshot(objective) == nil
}

// MarshalRecord returns a bounded canonical JSON record and a domain-separated
// digest. The record contains coordination metadata only: no source body,
// command, prompt, credential, filesystem path, or raw tool output.
func (objective Objective) MarshalRecord() ([]byte, string, error) {
	if err := validateObjectiveSnapshot(objective); err != nil {
		return nil, "", err
	}
	record := objectiveRecord{
		Version:     objective.Version,
		ObjectiveID: objective.ObjectiveID,
		Revision:    objective.Revision,
		State:       objective.State,
		Policy: objectivePolicyRecord{
			MaxTier:        objective.Policy.MaxTier(),
			AllowedClasses: objective.Policy.AllowedClasses(),
		},
		Steps: make([]objectiveStepRecord, 0, len(objective.Steps)),
	}
	if objective.Scope.Bound() {
		record.Scope = &objectiveScopeRecord{
			Project: objective.Scope.Project,
			Target:  objective.Scope.Target,
		}
		if objective.Scope.Anchor.Valid() {
			anchor := objective.Scope.Anchor
			record.Scope.Anchor = &anchor
		}
	}
	for _, step := range objective.Steps {
		stepRecord := objectiveStepRecord{
			StepID:       step.StepID,
			State:        step.State,
			Requirements: make([]string, 0, len(step.Requirements)),
			Attempts:     make([]objectiveAttemptRecord, 0, len(step.Attempts)),
		}
		if step.AcceptanceContract != nil {
			contract := commandAcceptanceContractRecord{
				CommandDigest:        step.AcceptanceContract.CommandDigest,
				SourceDigest:         step.AcceptanceContract.SourceDigest,
				PrivateBodyRef:       step.AcceptanceContract.PrivateBodyRef,
				PrivateBodyDigest:    step.AcceptanceContract.PrivateBodyDigest,
				RequiredCapabilities: make([]string, 0, len(step.AcceptanceContract.RequiredCapabilities)),
				ArtifactRefs:         append([]string(nil), step.AcceptanceContract.ArtifactRefs...),
			}
			for _, requirement := range step.AcceptanceContract.RequiredCapabilities {
				contract.RequiredCapabilities = append(contract.RequiredCapabilities, string(requirement.ID))
			}
			stepRecord.AcceptanceContract = &contract
		}
		if step.AcceptanceReceipt != nil {
			receipt := step.AcceptanceReceipt.clone()
			stepRecord.AcceptanceReceipt = &receipt
		}
		for _, provision := range step.Provisioning {
			stepRecord.Provisioning = append(stepRecord.Provisioning, cloneProvisionAttempt(provision))
		}
		for _, requirement := range step.Requirements {
			stepRecord.Requirements = append(stepRecord.Requirements, string(requirement.ID))
		}
		for _, attempt := range step.Attempts {
			stepRecord.Attempts = append(stepRecord.Attempts, objectiveAttemptRecord{
				AttemptID:             attempt.AttemptID,
				ParentAttemptID:       attempt.ParentAttemptID,
				SourceDigest:          attempt.SourceDigest,
				EnvironmentID:         attempt.EnvironmentID,
				EnvironmentDigest:     attempt.EnvironmentDigest,
				EnvironmentGeneration: attempt.EnvironmentGeneration,
				Class:                 attempt.Class,
				Tier:                  attempt.Tier,
				State:                 attempt.State,
				Failure:               attempt.Failure,
			})
		}
		record.Steps = append(record.Steps, stepRecord)
	}
	body, err := json.Marshal(record)
	if err != nil || len(body) == 0 || len(body) > MaxObjectiveRecordBytes {
		return nil, "", errors.New("development objective record is unavailable")
	}
	return body, objectiveRecordDigest(body), nil
}

func ParseObjectiveRecord(body []byte) (Objective, error) {
	if len(body) == 0 || len(body) > MaxObjectiveRecordBytes {
		return Objective{}, errors.New("development objective record is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var record objectiveRecord
	if err := decoder.Decode(&record); err != nil {
		return Objective{}, errors.New("development objective record is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Objective{}, errors.New("development objective record is invalid")
	}
	policy, err := NewResolutionPolicy(record.Policy.MaxTier, record.Policy.AllowedClasses...)
	if err != nil || !sameExecutionClasses(record.Policy.AllowedClasses, policy.AllowedClasses()) {
		return Objective{}, errors.New("development objective record is invalid")
	}
	objective := Objective{
		Version:     record.Version,
		ObjectiveID: record.ObjectiveID,
		Revision:    record.Revision,
		State:       record.State,
		Policy:      policy,
		Steps:       make([]ObjectiveStep, 0, len(record.Steps)),
	}
	if record.Scope != nil {
		scope, err := NewObjectiveScope(record.Scope.Project, record.Scope.Target)
		if err != nil || scope.Project != record.Scope.Project || scope.Target != record.Scope.Target {
			return Objective{}, errors.New("development objective record is invalid")
		}
		objective.Scope = scope
		if record.Scope.Anchor != nil {
			if !record.Scope.Anchor.Valid() {
				return Objective{}, errors.New("development workspace anchor is invalid")
			}
			objective.Scope.Anchor = *record.Scope.Anchor
		}
	}
	for _, stepRecord := range record.Steps {
		requirements, err := Requirements(stepRecord.Requirements...)
		if err != nil || !sameRequirementStrings(stepRecord.Requirements, requirements) {
			return Objective{}, errors.New("development objective record is invalid")
		}
		step := ObjectiveStep{
			StepID:       stepRecord.StepID,
			State:        stepRecord.State,
			Requirements: requirements,
			Attempts:     make([]ExecutionAttempt, 0, len(stepRecord.Attempts)),
		}
		if stepRecord.AcceptanceContract != nil {
			requirements, err := Requirements(stepRecord.AcceptanceContract.RequiredCapabilities...)
			if err != nil || !sameRequirementStrings(stepRecord.AcceptanceContract.RequiredCapabilities, requirements) {
				return Objective{}, errors.New("development objective record is invalid")
			}
			contract := CommandAcceptanceContract{
				CommandDigest:        stepRecord.AcceptanceContract.CommandDigest,
				SourceDigest:         stepRecord.AcceptanceContract.SourceDigest,
				PrivateBodyRef:       stepRecord.AcceptanceContract.PrivateBodyRef,
				PrivateBodyDigest:    stepRecord.AcceptanceContract.PrivateBodyDigest,
				RequiredCapabilities: requirements,
				ArtifactRefs:         append([]string(nil), stepRecord.AcceptanceContract.ArtifactRefs...),
			}
			step.AcceptanceContract = &contract
		}
		if stepRecord.AcceptanceReceipt != nil {
			receipt := stepRecord.AcceptanceReceipt.clone()
			step.AcceptanceReceipt = &receipt
		}
		for _, provision := range stepRecord.Provisioning {
			step.Provisioning = append(step.Provisioning, cloneProvisionAttempt(provision))
		}
		for _, attemptRecord := range stepRecord.Attempts {
			attempt := ExecutionAttempt{
				AttemptID:             attemptRecord.AttemptID,
				StepID:                stepRecord.StepID,
				ParentAttemptID:       attemptRecord.ParentAttemptID,
				SourceDigest:          attemptRecord.SourceDigest,
				EnvironmentID:         attemptRecord.EnvironmentID,
				EnvironmentDigest:     attemptRecord.EnvironmentDigest,
				EnvironmentGeneration: attemptRecord.EnvironmentGeneration,
				Class:                 attemptRecord.Class,
				Tier:                  attemptRecord.Tier,
				State:                 attemptRecord.State,
				Failure:               attemptRecord.Failure,
			}
			step.Attempts = append(step.Attempts, attempt)
		}
		objective.Steps = append(objective.Steps, step)
	}
	if err := validateObjectiveSnapshot(objective); err != nil {
		return Objective{}, errors.New("development objective record is invalid")
	}
	return objective, nil
}

func ObjectiveRecordDigest(body []byte) string {
	if len(body) == 0 || len(body) > MaxObjectiveRecordBytes {
		return ""
	}
	return objectiveRecordDigest(body)
}

func objectiveRecordDigest(body []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(objectiveRecordDigestDomain))
	_, _ = hash.Write(body)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validateObjectiveSnapshot(objective Objective) error {
	if objective.Version != ObjectiveVersion || !identityPattern.MatchString(objective.ObjectiveID) ||
		objective.Revision == 0 || !objective.Scope.validOrEmpty() || !objective.Policy.valid() || len(objective.Steps) == 0 ||
		len(objective.Steps) > MaxObjectiveSteps || !validObjectiveState(objective.State) {
		return errors.New("development objective snapshot is invalid")
	}
	stepIDs := make(map[string]struct{}, len(objective.Steps))
	attemptIDs := make(map[string]struct{})
	operationIDs := make(map[string]struct{})
	allSucceeded := true
	if !sameCommandAcceptanceContractMode(objective.Steps) {
		return errors.New("development objective acceptance mode is invalid")
	}
	contracted := len(objective.Steps) != 0 && objective.Steps[0].AcceptanceContract != nil
	receiptCount := 0
	for index, step := range objective.Steps {
		if !identityPattern.MatchString(step.StepID) || !validStepState(step.State) ||
			len(step.Attempts) > MaxAttemptsPerStep {
			return errors.New("development objective snapshot is invalid")
		}
		if _, duplicate := stepIDs[step.StepID]; duplicate {
			return errors.New("development objective snapshot is invalid")
		}
		stepIDs[step.StepID] = struct{}{}
		normalized, err := normalizeRequirementList(step.Requirements)
		if err != nil || !sameRequirements(step.Requirements, normalized) {
			return errors.New("development objective snapshot is invalid")
		}
		if err := validateObjectiveAttempts(step, attemptIDs); err != nil {
			return err
		}
		if step.AcceptanceContract != nil && !step.AcceptanceContract.validFor(objective.Policy, step.Requirements) {
			return errors.New("development command acceptance contract is invalid")
		}
		if step.AcceptanceContract != nil {
			for _, attempt := range step.Attempts {
				if attempt.SourceDigest != step.AcceptanceContract.SourceDigest {
					return errors.New("development command acceptance source changed")
				}
			}
		}
		if step.AcceptanceReceipt != nil {
			if step.AcceptanceContract == nil || objective.State != ObjectiveAccepted ||
				!step.AcceptanceReceipt.validFor(objective, index) {
				return errors.New("development command acceptance receipt is invalid")
			}
			if _, duplicate := operationIDs[step.AcceptanceReceipt.OperationID]; duplicate {
				return errors.New("development command acceptance receipt is duplicated")
			}
			operationIDs[step.AcceptanceReceipt.OperationID] = struct{}{}
			receiptCount++
		}
		for _, attempt := range step.Attempts {
			if !objective.Policy.AllowsClass(attempt.Class) {
				return errors.New("development attempt exceeds authority policy")
			}
		}
		if err := validateObjectiveProvisioning(step, objective.Policy, attemptIDs); err != nil {
			return err
		}
		if !stepStateMatchesAttempts(step) {
			return errors.New("development objective snapshot is invalid")
		}
		allSucceeded = allSucceeded && step.State == StepSucceeded
	}
	switch objective.State {
	case ObjectivePlanned:
		for _, step := range objective.Steps {
			if step.State != StepPlanned || len(step.Attempts) != 0 || len(step.Provisioning) != 0 {
				return errors.New("development objective snapshot is invalid")
			}
		}
	case ObjectiveRunning:
		if allSucceeded {
			return errors.New("development objective snapshot is invalid")
		}
	case ObjectiveAcceptancePending, ObjectiveAccepted:
		if !allSucceeded {
			return errors.New("development objective snapshot is invalid")
		}
		if objective.State == ObjectiveAcceptancePending && receiptCount != 0 {
			return errors.New("development acceptance evidence is premature")
		}
		if objective.State == ObjectiveAccepted && ((contracted && receiptCount != len(objective.Steps)) || (!contracted && receiptCount != 0)) {
			return errors.New("development accepted objective lacks required evidence")
		}
	case ObjectiveReconciliationRequired:
		if !objectiveHasFailureClass(objective, FailureReconciliationNeeded) {
			return errors.New("development objective snapshot is invalid")
		}
	case ObjectiveFailed:
		hasFailed := false
		for _, step := range objective.Steps {
			hasFailed = hasFailed || step.State == StepFailed
		}
		if !hasFailed {
			return errors.New("development objective snapshot is invalid")
		}
	case ObjectiveCancelled:
		for _, step := range objective.Steps {
			if step.State == StepRunning || step.State == StepPlanned {
				return errors.New("development objective snapshot is invalid")
			}
			if activeProvisioning(step) {
				return errors.New("development objective snapshot is invalid")
			}
		}
	}
	return nil
}

func validateObjectiveProvisioning(step ObjectiveStep, policy ResolutionPolicy, global map[string]struct{}) error {
	if len(step.Provisioning) > MaxProvisioningAttemptsPerStep {
		return errors.New("development provisioning bound exceeded")
	}
	for index, provision := range step.Provisioning {
		if !provision.Valid() || !policy.AllowsClass(provision.Plan.OutputClass) {
			return errors.New("development provisioning snapshot is invalid")
		}
		if _, duplicate := global[provision.ProvisionID]; duplicate {
			return errors.New("development provisioning identity duplicated")
		}
		global[provision.ProvisionID] = struct{}{}
		if index < len(step.Provisioning)-1 && (provision.State == ProvisioningPlanned || provision.State == ProvisioningQueued) {
			return errors.New("development provisioning history is active")
		}
	}
	if activeProvisioning(step) && (step.State == StepRunning || step.State == StepSucceeded || step.State == StepCancelled) {
		return errors.New("development provisioning overlaps execution")
	}
	if activeProvisioning(step) && !step.Provisioning[len(step.Provisioning)-1].Plan.Covers(requirementIDs(step.Requirements)) {
		return errors.New("development provisioning no longer covers requirements")
	}
	return nil
}

func activeProvisioning(step ObjectiveStep) bool {
	if len(step.Provisioning) == 0 {
		return false
	}
	state := step.Provisioning[len(step.Provisioning)-1].State
	return state == ProvisioningPlanned || state == ProvisioningQueued
}

func validateObjectiveAttempts(step ObjectiveStep, global map[string]struct{}) error {
	for index, attempt := range step.Attempts {
		if !attempt.validIdentity() || attempt.StepID != step.StepID {
			return errors.New("development objective attempt snapshot is invalid")
		}
		if _, duplicate := global[attempt.AttemptID]; duplicate {
			return errors.New("development objective attempt snapshot is invalid")
		}
		global[attempt.AttemptID] = struct{}{}
		if index == 0 {
			if attempt.ParentAttemptID != "" {
				return errors.New("development objective attempt snapshot is invalid")
			}
		} else {
			previous := step.Attempts[index-1]
			if !validAttemptContinuation(previous, attempt) {
				return errors.New("development objective attempt snapshot is invalid")
			}
		}
		if attempt.State == AttemptFailed {
			if _, ok := ContinuationForFailure(attempt.Failure); !ok {
				return errors.New("development objective attempt snapshot is invalid")
			}
		} else if attempt.Failure != "" {
			return errors.New("development objective attempt snapshot is invalid")
		}
	}
	return nil
}

func stepStateMatchesAttempts(step ObjectiveStep) bool {
	if len(step.Attempts) == 0 {
		return step.State == StepPlanned || step.State == StepCancelled
	}
	switch step.Attempts[len(step.Attempts)-1].State {
	case AttemptPlanned:
		return step.State == StepPlanned
	case AttemptRunning:
		return step.State == StepRunning
	case AttemptSucceeded:
		return step.State == StepSucceeded
	case AttemptFailed:
		return step.State == StepFailed
	case AttemptCancelled:
		return step.State == StepCancelled
	default:
		return false
	}
}

func validObjectiveState(state ObjectiveState) bool {
	switch state {
	case ObjectivePlanned, ObjectiveRunning, ObjectiveAcceptancePending, ObjectiveAccepted,
		ObjectiveReconciliationRequired, ObjectiveFailed, ObjectiveCancelled:
		return true
	default:
		return false
	}
}

func validStepState(state StepState) bool {
	switch state {
	case StepPlanned, StepRunning, StepSucceeded, StepFailed, StepCancelled:
		return true
	default:
		return false
	}
}

func sameExecutionClasses(left, right []ExecutionClass) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameRequirementStrings(raw []string, requirements []Requirement) bool {
	if len(raw) != len(requirements) {
		return false
	}
	for index := range raw {
		if raw[index] != string(requirements[index].ID) {
			return false
		}
	}
	return true
}

func objectiveHasFailureClass(objective Objective, class FailureClass) bool {
	for _, step := range objective.Steps {
		for _, attempt := range step.Attempts {
			if attempt.State == AttemptFailed && attempt.Failure == class {
				return true
			}
		}
	}
	return false
}
