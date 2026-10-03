package development

import "errors"

// PlanProvisioning persists only server-selected coordination metadata. A
// provision is not an execution attempt and cannot satisfy semantic acceptance.
func (objective Objective) PlanProvisioning(stepID, provisionID, sourceDigest string, plan ProvisionPlan) (Objective, error) {
	index := objective.stepIndex(stepID)
	if !objective.Valid() || index < 0 || objective.terminal() || !objective.Scope.Bound() ||
		!objective.Policy.AllowsClass(plan.OutputClass) {
		return Objective{}, errors.New("development objective cannot provision")
	}
	step := objective.Steps[index]
	if step.State != StepPlanned && step.State != StepFailed || activeProvisioning(step) ||
		len(step.Provisioning) >= MaxProvisioningAttemptsPerStep || !plan.Covers(requirementIDs(step.Requirements)) {
		return Objective{}, errors.New("development objective provisioning contract is invalid")
	}
	if !canPlanProvisioning(step) {
		return Objective{}, errors.New("development objective failure does not authorize provisioning")
	}
	provision, err := NewProvisioningAttempt(provisionID, sourceDigest, plan)
	if err != nil {
		return Objective{}, err
	}
	next := objective.clone()
	next.Steps[index].Provisioning = append(next.Steps[index].Provisioning, provision)
	next.State = ObjectiveRunning
	next.Revision++
	if !next.Valid() {
		return Objective{}, errors.New("development provisioning snapshot is invalid")
	}
	return next, nil
}

func canPlanProvisioning(step ObjectiveStep) bool {
	if step.State != StepPlanned && step.State != StepFailed || activeProvisioning(step) {
		return false
	}
	if len(step.Attempts) > 0 {
		last := step.Attempts[len(step.Attempts)-1]
		action, ok := ContinuationForFailure(last.Failure)
		if last.State != AttemptFailed || !ok || action != ActionProvisionOrMigrate {
			return false
		}
	}
	if len(step.Provisioning) > 0 {
		last := step.Provisioning[len(step.Provisioning)-1]
		if last.State == ProvisioningCancelled {
			return false
		}
		if last.State == ProvisioningFailed {
			action, ok := ContinuationForFailure(last.Failure)
			return ok && (action == ActionProvisionOrMigrate || action == ActionRetrySameEnvironment)
		}
	}
	return true
}

func (objective Objective) BindProvisionJob(stepID, provisionID, jobID string) (Objective, error) {
	return objective.advanceProvision(stepID, provisionID, func(last ProvisioningAttempt) (ProvisioningAttempt, error) { return last.BindJob(jobID) })
}

func (objective Objective) BindProvisionFence(stepID, provisionID string, fence uint64) (Objective, error) {
	return objective.advanceProvision(stepID, provisionID, func(last ProvisioningAttempt) (ProvisioningAttempt, error) { return last.BindFence(fence) })
}

func (objective Objective) CompleteProvision(stepID, provisionID, receiptDigest string) (Objective, error) {
	return objective.advanceProvision(stepID, provisionID, func(last ProvisioningAttempt) (ProvisioningAttempt, error) { return last.Succeed(receiptDigest) })
}

func (objective Objective) FailProvision(stepID, provisionID string, class FailureClass) (Objective, error) {
	return objective.advanceProvision(stepID, provisionID, func(last ProvisioningAttempt) (ProvisioningAttempt, error) { return last.Fail(class) })
}

func (objective Objective) advanceProvision(stepID, provisionID string, change func(ProvisioningAttempt) (ProvisioningAttempt, error)) (Objective, error) {
	index := objective.stepIndex(stepID)
	if !objective.Valid() || objective.terminal() || index < 0 || len(objective.Steps[index].Provisioning) == 0 {
		return Objective{}, errors.New("development provisioning is unavailable")
	}
	lastIndex := len(objective.Steps[index].Provisioning) - 1
	last := objective.Steps[index].Provisioning[lastIndex]
	if last.ProvisionID != provisionID {
		return Objective{}, errors.New("development provisioning identity mismatch")
	}
	advanced, err := change(last)
	if err != nil || !validProvisionRevision(last, advanced) {
		return Objective{}, errors.New("development provisioning transition is invalid")
	}
	if sameProvisionAttempt(last, advanced) {
		return objective, nil
	}
	next := objective.clone()
	next.Steps[index].Provisioning[lastIndex] = advanced
	next.Revision++
	if !next.Valid() {
		return Objective{}, errors.New("development provisioning snapshot is invalid")
	}
	return next, nil
}
