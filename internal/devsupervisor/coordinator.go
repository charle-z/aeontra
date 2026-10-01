package devsupervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const (
	attemptIdentityDomain   = "aeontra-development-supervisor-attempt-v1\x00"
	MaxAutomaticTransitions = 4
)

var (
	ErrObjectiveNotFound         = errors.New("development supervisor: objective not found")
	ErrObjectiveScopeUnavailable = errors.New("development supervisor: objective scope unavailable")
	ErrAttemptConflict           = errors.New("development supervisor: attempt conflict")
	ErrCatalogUnavailable        = errors.New("development supervisor: environment catalog unavailable")
	ErrAutomaticBudget           = errors.New("development supervisor: automatic transition budget exhausted")
)

type ObjectiveStore interface {
	SaveDevelopmentObjective(development.Objective) (development.Objective, bool, error)
	DevelopmentObjective(string) (development.Objective, bool, error)
}

type CatalogSource interface {
	Catalog(context.Context, development.Objective) (development.EnvironmentCatalog, error)
}

type Supervisor struct {
	store   ObjectiveStore
	catalog CatalogSource
}

func New(store ObjectiveStore, catalog CatalogSource) (*Supervisor, error) {
	if store == nil || catalog == nil {
		return nil, errors.New("development supervisor: configuration is invalid")
	}
	return &Supervisor{store: store, catalog: catalog}, nil
}

type PlanResult struct {
	Objective     development.Objective
	Resolution    development.Resolution
	CatalogDigest string
	Reused        bool
}

type StartResult struct {
	Objective     development.Objective
	Environment   development.EnvironmentAttestation
	CatalogDigest string
	Started       bool
	Reused        bool
	Rejected      development.FailureClass
	Action        development.ContinuationAction
}

type FailureResult struct {
	Objective development.Objective
	Action    development.ContinuationAction
	Reused    bool
}

type RevisionResult struct {
	Objective development.Objective
	Reused    bool
}

func (supervisor *Supervisor) Create(ctx context.Context, objective development.Objective) (development.Objective, bool, error) {
	if supervisor == nil || ctx == nil || ctx.Err() != nil {
		return development.Objective{}, false, errors.New("development supervisor: unavailable")
	}
	if !objective.Scope.Bound() {
		return development.Objective{}, false, ErrObjectiveScopeUnavailable
	}
	return supervisor.store.SaveDevelopmentObjective(objective)
}

func (supervisor *Supervisor) Status(ctx context.Context, objectiveID string) (development.Objective, error) {
	if supervisor == nil || ctx == nil || ctx.Err() != nil {
		return development.Objective{}, errors.New("development supervisor: unavailable")
	}
	objective, found, err := supervisor.store.DevelopmentObjective(objectiveID)
	if err != nil {
		return development.Objective{}, err
	}
	if !found {
		return development.Objective{}, ErrObjectiveNotFound
	}
	if !objective.Scope.Bound() {
		return development.Objective{}, ErrObjectiveScopeUnavailable
	}
	return objective, nil
}

func (supervisor *Supervisor) PlanStep(ctx context.Context, objectiveID, stepID, sourceDigest string) (PlanResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return PlanResult{}, err
	}
	catalog, err := supervisor.catalogSnapshot(ctx, objective)
	if err != nil {
		return PlanResult{}, err
	}
	step, found := objectiveStep(objective, stepID)
	if !found {
		return PlanResult{}, errors.New("development supervisor: step not found")
	}

	if len(step.Attempts) != 0 {
		last := step.Attempts[len(step.Attempts)-1]
		if (last.State == development.AttemptPlanned || last.State == development.AttemptRunning) && last.SourceDigest == sourceDigest {
			environment, found := catalog.FindDigest(last.EnvironmentDigest)
			if !found || !objective.Policy.Allows(environment) ||
				len(environment.Capabilities.Missing(step.Requirements)) != 0 {
				return PlanResult{}, ErrAttemptConflict
			}
			return PlanResult{
				Objective:     objective,
				Resolution:    development.Resolution{Environment: environment, Migrated: last.ParentAttemptID != ""},
				CatalogDigest: catalog.Digest(),
				Reused:        true,
			}, nil
		}
	}

	resolution, err := supervisorPlanResolution(objective, step, catalog)
	if err != nil {
		return PlanResult{}, err
	}
	attemptID := supervisorAttemptID(objective, step, sourceDigest, resolution.Environment.Digest)
	next, plannedResolution, err := objective.PlanAttempt(stepID, attemptID, sourceDigest, catalog.Environments())
	if err != nil {
		return PlanResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		// A lost-ack or concurrent identical plan can be recovered from the
		// durable revision rather than minting another attempt.
		current, found, readErr := supervisor.store.DevelopmentObjective(objectiveID)
		if readErr == nil && found {
			if recovered, ok := matchingPlannedAttempt(current, stepID, attemptID, sourceDigest, catalog); ok {
				return PlanResult{
					Objective: current, Resolution: recovered,
					CatalogDigest: catalog.Digest(), Reused: true,
				}, nil
			}
		}
		return PlanResult{}, err
	}
	return PlanResult{
		Objective: persisted, Resolution: plannedResolution,
		CatalogDigest: catalog.Digest(),
	}, nil
}

func (supervisor *Supervisor) StartStep(ctx context.Context, objectiveID, stepID string) (StartResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return StartResult{}, err
	}
	step, found := objectiveStep(objective, stepID)
	if !found || len(step.Attempts) == 0 {
		return StartResult{}, errors.New("development supervisor: planned attempt is missing")
	}
	last := step.Attempts[len(step.Attempts)-1]
	catalog, err := supervisor.catalogSnapshot(ctx, objective)
	if err != nil {
		return StartResult{}, err
	}
	environment, present := catalog.FindDigest(last.EnvironmentDigest)

	if last.State == development.AttemptRunning {
		if !present || !objective.Policy.Allows(environment) ||
			len(environment.Capabilities.Missing(step.Requirements)) != 0 {
			return StartResult{}, ErrAttemptConflict
		}
		return StartResult{
			Objective: objective, Environment: environment,
			CatalogDigest: catalog.Digest(), Started: true, Reused: true,
		}, nil
	}
	if last.State != development.AttemptPlanned {
		return StartResult{}, ErrAttemptConflict
	}

	if !present {
		return supervisor.rejectPlannedAttempt(objective, stepID, catalog, development.FailureCapabilityDrift)
	}
	if !objective.Policy.Allows(environment) {
		return supervisor.rejectPlannedAttempt(objective, stepID, catalog, development.FailurePolicyDenied)
	}
	if len(environment.Capabilities.Missing(step.Requirements)) != 0 {
		return supervisor.rejectPlannedAttempt(objective, stepID, catalog, development.FailureCapabilityMissing)
	}

	next, err := objective.StartAttempt(stepID)
	if err != nil {
		return StartResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		current, found, readErr := supervisor.store.DevelopmentObjective(objectiveID)
		if readErr == nil && found {
			if currentStep, ok := objectiveStep(current, stepID); ok && len(currentStep.Attempts) != 0 {
				currentAttempt := currentStep.Attempts[len(currentStep.Attempts)-1]
				if currentAttempt.AttemptID == last.AttemptID && currentAttempt.State == development.AttemptRunning {
					if currentEnvironment, ok := catalog.FindDigest(currentAttempt.EnvironmentDigest); ok {
						return StartResult{
							Objective: current, Environment: currentEnvironment,
							CatalogDigest: catalog.Digest(), Started: true, Reused: true,
						}, nil
					}
				}
			}
		}
		return StartResult{}, err
	}
	return StartResult{
		Objective: persisted, Environment: environment,
		CatalogDigest: catalog.Digest(), Started: true,
	}, nil
}

func (supervisor *Supervisor) EnsureStepRunning(ctx context.Context, objectiveID, stepID, sourceDigest string) (StartResult, error) {
	for transition := 0; transition < MaxAutomaticTransitions; transition++ {
		objective, err := supervisor.Status(ctx, objectiveID)
		if err != nil {
			return StartResult{}, err
		}
		step, found := objectiveStep(objective, stepID)
		if !found {
			return StartResult{}, errors.New("development supervisor: step not found")
		}
		if len(step.Attempts) == 0 || step.Attempts[len(step.Attempts)-1].State == development.AttemptFailed {
			if _, err := supervisor.PlanStep(ctx, objectiveID, stepID, sourceDigest); err != nil {
				return StartResult{}, err
			}
		}
		started, err := supervisor.StartStep(ctx, objectiveID, stepID)
		if err != nil {
			return StartResult{}, err
		}
		if started.Started {
			return started, nil
		}
		if started.Action != development.ActionProvisionOrMigrate {
			return started, nil
		}
	}
	return StartResult{}, ErrAutomaticBudget
}

func (supervisor *Supervisor) RefineRequirements(ctx context.Context, objectiveID, stepID string, additional []development.Requirement) (RevisionResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return RevisionResult{}, err
	}
	next, err := objective.RefineRequirements(stepID, additional)
	if err != nil {
		return RevisionResult{}, err
	}
	if next.Revision == objective.Revision {
		return RevisionResult{Objective: objective, Reused: true}, nil
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		return RevisionResult{}, err
	}
	return RevisionResult{Objective: persisted}, nil
}

func (supervisor *Supervisor) FailStep(ctx context.Context, objectiveID, stepID string, class development.FailureClass) (FailureResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return FailureResult{}, err
	}
	step, found := objectiveStep(objective, stepID)
	if !found || len(step.Attempts) == 0 {
		return FailureResult{}, ErrAttemptConflict
	}
	last := step.Attempts[len(step.Attempts)-1]
	if last.State == development.AttemptFailed {
		if last.Failure != class {
			return FailureResult{}, ErrAttemptConflict
		}
		action, _ := development.ContinuationForFailure(class)
		return FailureResult{Objective: objective, Action: action, Reused: true}, nil
	}
	next, action, err := objective.FailAttempt(stepID, class)
	if err != nil {
		return FailureResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		return FailureResult{}, err
	}
	return FailureResult{Objective: persisted, Action: action}, nil
}

func (supervisor *Supervisor) CompleteStep(ctx context.Context, objectiveID, stepID string) (RevisionResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return RevisionResult{}, err
	}
	step, found := objectiveStep(objective, stepID)
	if !found || len(step.Attempts) == 0 {
		return RevisionResult{}, ErrAttemptConflict
	}
	last := step.Attempts[len(step.Attempts)-1]
	if last.State == development.AttemptSucceeded && step.State == development.StepSucceeded {
		return RevisionResult{Objective: objective, Reused: true}, nil
	}
	next, err := objective.CompleteAttempt(stepID)
	if err != nil {
		return RevisionResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		return RevisionResult{}, err
	}
	return RevisionResult{Objective: persisted}, nil
}

func (supervisor *Supervisor) Cancel(ctx context.Context, objectiveID string) (RevisionResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return RevisionResult{}, err
	}
	if objective.State == development.ObjectiveCancelled {
		return RevisionResult{Objective: objective, Reused: true}, nil
	}
	next, err := objective.Cancel()
	if err != nil {
		return RevisionResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		return RevisionResult{}, err
	}
	return RevisionResult{Objective: persisted}, nil
}

func (supervisor *Supervisor) rejectPlannedAttempt(objective development.Objective, stepID string, catalog development.EnvironmentCatalog, class development.FailureClass) (StartResult, error) {
	next, action, err := objective.RejectAttempt(stepID, class)
	if err != nil {
		return StartResult{}, err
	}
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		Objective: persisted, CatalogDigest: catalog.Digest(),
		Rejected: class, Action: action,
	}, nil
}

func (supervisor *Supervisor) catalogSnapshot(ctx context.Context, objective development.Objective) (development.EnvironmentCatalog, error) {
	catalog, err := supervisor.catalog.Catalog(ctx, objective)
	if err != nil || !catalog.Valid() {
		return development.EnvironmentCatalog{}, ErrCatalogUnavailable
	}
	return catalog, nil
}

func objectiveStep(objective development.Objective, stepID string) (development.ObjectiveStep, bool) {
	stepID = strings.TrimSpace(stepID)
	for _, step := range objective.Steps {
		if step.StepID == stepID {
			return step, true
		}
	}
	return development.ObjectiveStep{}, false
}

func matchingPlannedAttempt(objective development.Objective, stepID, attemptID, sourceDigest string, catalog development.EnvironmentCatalog) (development.Resolution, bool) {
	step, found := objectiveStep(objective, stepID)
	if !found || len(step.Attempts) == 0 {
		return development.Resolution{}, false
	}
	last := step.Attempts[len(step.Attempts)-1]
	if last.AttemptID != attemptID || last.SourceDigest != sourceDigest ||
		(last.State != development.AttemptPlanned && last.State != development.AttemptRunning) {
		return development.Resolution{}, false
	}
	environment, found := catalog.FindDigest(last.EnvironmentDigest)
	if !found || !objective.Policy.Allows(environment) ||
		len(environment.Capabilities.Missing(step.Requirements)) != 0 {
		return development.Resolution{}, false
	}
	return development.Resolution{
		Environment: environment,
		Migrated:    last.ParentAttemptID != "",
	}, true
}

func supervisorPlanResolution(objective development.Objective, step development.ObjectiveStep, catalog development.EnvironmentCatalog) (development.Resolution, error) {
	if len(step.Attempts) == 0 {
		return development.Resolve(step.Requirements, nil, catalog.Environments(), objective.Policy)
	}
	previous := step.Attempts[len(step.Attempts)-1]
	if previous.State != development.AttemptFailed {
		return development.Resolution{}, ErrAttemptConflict
	}
	action, ok := development.ContinuationForFailure(previous.Failure)
	if !ok || action == development.ActionStopPolicy || action == development.ActionReconcile {
		return development.Resolution{}, ErrAttemptConflict
	}
	if action == development.ActionFixCode || action == development.ActionRetrySameEnvironment {
		environment, found := catalog.FindDigest(previous.EnvironmentDigest)
		if !found || !objective.Policy.Allows(environment) ||
			len(environment.Capabilities.Missing(step.Requirements)) != 0 {
			return development.Resolution{}, ErrAttemptConflict
		}
		return development.Resolution{Environment: environment}, nil
	}
	return development.Resolve(step.Requirements, nil, catalog.Environments(), objective.Policy)
}

func supervisorAttemptID(objective development.Objective, step development.ObjectiveStep, sourceDigest, environmentDigest string) string {
	parent := ""
	if len(step.Attempts) != 0 {
		parent = step.Attempts[len(step.Attempts)-1].AttemptID
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(attemptIdentityDomain))
	for _, value := range []string{objective.ObjectiveID, step.StepID, parent, sourceDigest, environmentDigest} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, requirement := range step.Requirements {
		_, _ = hash.Write([]byte(requirement.ID))
		_, _ = hash.Write([]byte{0})
	}
	return "attempt-" + hex.EncodeToString(hash.Sum(nil))[:32]
}
