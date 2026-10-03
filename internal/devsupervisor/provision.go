package devsupervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

var (
	ErrProvisionUnavailable            = errors.New("development supervisor: governed provisioning unavailable")
	ErrProvisionUnsupported            = errors.New("development supervisor: requirements are unsupported by governed provisioning")
	ErrProvisionConflict               = errors.New("development supervisor: provisioning identity conflict")
	ErrProvisionUnverified             = errors.New("development supervisor: provisioning receipt requires re-attestation")
	ErrProvisionReconciliationRequired = errors.New("development supervisor: provision cancellation requires reconciliation")
)

// Provisioner is registered by the administrator, never supplied by an MCP
// request or repository. Plans contain no executable, credential or host path.
type Provisioner interface {
	Plans(context.Context, development.Objective, development.ObjectiveStep, development.EnvironmentCatalog) ([]development.ProvisionPlan, error)
}

type ProvisionQueue interface {
	Enqueue(workqueue.Spec) (workqueue.Job, bool, error)
	Get(string) (workqueue.Job, bool, error)
	Cancel(string) (workqueue.Job, error)
}

func (supervisor *Supervisor) WithProvisioning(queue ProvisionQueue, providers ...Provisioner) (*Supervisor, error) {
	if supervisor == nil || queue == nil || len(providers) == 0 || len(providers) > MaxAttestationProviders {
		return nil, ErrProvisionUnavailable
	}
	for _, provider := range providers {
		if provider == nil {
			return nil, ErrProvisionUnavailable
		}
	}
	next := *supervisor
	next.provisionQueue = queue
	next.provisioners = append([]Provisioner(nil), providers...)
	return &next, nil
}

type ProvisionResult struct {
	Objective development.Objective
	Provision development.ProvisioningAttempt
	Ready     bool
}

// ProvisionStep advances one durable effect. A retry always recovers the exact
// queued job. Workqueue lease/fence semantics remain the sole scheduler.
func (supervisor *Supervisor) ProvisionStep(ctx context.Context, objectiveID, stepID, sourceDigest string) (ProvisionResult, error) {
	objective, err := supervisor.Status(ctx, objectiveID)
	if err != nil {
		return ProvisionResult{}, err
	}
	if supervisor.provisionQueue == nil {
		return ProvisionResult{}, ErrProvisionUnavailable
	}
	step, found := objectiveStep(objective, stepID)
	if !found {
		return ProvisionResult{}, ErrProvisionConflict
	}
	catalog, err := supervisor.catalogSnapshot(ctx, objective)
	if err != nil {
		return ProvisionResult{}, err
	}
	var provision development.ProvisioningAttempt
	if len(step.Provisioning) != 0 {
		provision = step.Provisioning[len(step.Provisioning)-1]
		if provision.SourceDigest != sourceDigest && (provision.State == development.ProvisioningPlanned || provision.State == development.ProvisioningQueued) {
			return ProvisionResult{}, ErrProvisionConflict
		}
		if provision.State == development.ProvisioningSucceeded {
			if provision.SourceDigest == sourceDigest && provisionReady(objective, step, provision, catalog) {
				return ProvisionResult{Objective: objective, Provision: provision, Ready: true}, nil
			}
			return ProvisionResult{}, ErrProvisionUnverified
		}
		if provision.State == development.ProvisioningCancelled {
			return ProvisionResult{}, ErrProvisionConflict
		}
	}
	if provision.ProvisionID == "" || provision.State == development.ProvisioningFailed {
		plan, err := supervisor.selectProvisionPlan(ctx, objective, step, catalog)
		if err != nil {
			return ProvisionResult{}, err
		}
		provisionID := provisionIdentity(objective, step, sourceDigest, plan)
		next, err := objective.PlanProvisioning(stepID, provisionID, sourceDigest, plan)
		if err != nil {
			return ProvisionResult{}, err
		}
		objective, err = supervisor.persistProvision(next, stepID, provisionID)
		if err != nil {
			return ProvisionResult{}, err
		}
		step, _ = objectiveStep(objective, stepID)
		provision = step.Provisioning[len(step.Provisioning)-1]
	}
	if err := ctx.Err(); err != nil {
		return ProvisionResult{}, err
	}
	spec := provisionSpec(objective, provision)
	if provision.State == development.ProvisioningPlanned {
		job, _, err := supervisor.provisionQueue.Enqueue(spec)
		// An enqueue lost ACK is recovered by the queue's identical-key CAS on
		// the next call. Do not invent a second key or a second job here.
		if err != nil {
			return ProvisionResult{}, err
		}
		if !matchesProvisionJob(job, spec) {
			return ProvisionResult{}, ErrProvisionConflict
		}
		next, err := objective.BindProvisionJob(stepID, provision.ProvisionID, job.ID)
		if err != nil {
			return ProvisionResult{}, err
		}
		objective, err = supervisor.persistProvision(next, stepID, provision.ProvisionID)
		if err != nil {
			return ProvisionResult{}, err
		}
		step, _ = objectiveStep(objective, stepID)
		provision = step.Provisioning[len(step.Provisioning)-1]
	}
	job, found, err := supervisor.provisionQueue.Get(provision.JobID)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !found || !matchesProvisionJob(job, spec) || job.Fence < provision.JobFence {
		return ProvisionResult{}, ErrProvisionConflict
	}
	if job.Fence > provision.JobFence {
		next, err := objective.BindProvisionFence(stepID, provision.ProvisionID, job.Fence)
		if err != nil {
			return ProvisionResult{}, err
		}
		objective, err = supervisor.persistProvision(next, stepID, provision.ProvisionID)
		if err != nil {
			return ProvisionResult{}, err
		}
		step, _ = objectiveStep(objective, stepID)
		provision = step.Provisioning[len(step.Provisioning)-1]
	}
	if provision.State != development.ProvisioningQueued {
		return ProvisionResult{Objective: objective, Provision: provision}, nil
	}
	var next development.Objective
	switch job.State {
	case workqueue.StateQueued, workqueue.StateLeased:
		return ProvisionResult{Objective: objective, Provision: provision}, nil
	case workqueue.StateSucceeded:
		if job.Fence == 0 {
			return ProvisionResult{}, ErrProvisionConflict
		}
		next, err = objective.CompleteProvision(stepID, provision.ProvisionID, provisionReceiptDigest(job))
	case workqueue.StateFailed:
		class := development.FailureClass(job.Summary)
		if _, ok := development.ContinuationForFailure(class); !ok {
			class = development.FailureReconciliationNeeded
		}
		next, err = objective.FailProvision(stepID, provision.ProvisionID, class)
	case workqueue.StateCancelled:
		return ProvisionResult{}, ErrProvisionConflict
	default:
		return ProvisionResult{}, ErrProvisionConflict
	}
	if err != nil {
		return ProvisionResult{}, err
	}
	objective, err = supervisor.persistProvision(next, stepID, provision.ProvisionID)
	if err != nil {
		return ProvisionResult{}, err
	}
	step, _ = objectiveStep(objective, stepID)
	provision = step.Provisioning[len(step.Provisioning)-1]
	result := ProvisionResult{Objective: objective, Provision: provision}
	if provision.State == development.ProvisioningSucceeded {
		// Completion proves only the queued effect ended. Re-read independently
		// attested capabilities before allowing any execution attempt.
		catalog, err := supervisor.catalogSnapshot(ctx, objective)
		if err != nil {
			return ProvisionResult{}, err
		}
		result.Ready = provisionReady(objective, step, provision, catalog)
		if !result.Ready {
			return result, ErrProvisionUnverified
		}
	}
	return result, nil
}

func (supervisor *Supervisor) selectProvisionPlan(ctx context.Context, objective development.Objective, step development.ObjectiveStep, catalog development.EnvironmentCatalog) (development.ProvisionPlan, error) {
	var candidates []development.ProvisionPlan
	unsupported := 0
	for _, provider := range supervisor.provisioners {
		plans, err := provider.Plans(ctx, objective, step, catalog)
		if errors.Is(err, ErrProvisionUnsupported) && len(plans) == 0 {
			unsupported++
			continue
		}
		if err != nil || len(plans) > development.MaxEnvironmentCatalogEntries-len(candidates) {
			return development.ProvisionPlan{}, ErrProvisionUnavailable
		}
		for _, plan := range plans {
			if !plan.Valid() {
				return development.ProvisionPlan{}, ErrProvisionUnavailable
			}
			if !objective.Policy.AllowsClass(plan.OutputClass) || !plan.Covers(requirementIDs(step.Requirements)) {
				continue
			}
			if plan.BaseEnvironmentDigest != "" {
				base, found := catalog.FindDigest(plan.BaseEnvironmentDigest)
				if !found || !objective.Policy.Allows(base) {
					continue
				}
			}
			candidates = append(candidates, plan)
		}
	}
	if len(candidates) == 0 {
		// Only unanimous explicit rejection is definitive. Empty offers or
		// unavailable providers cannot establish that no governed route exists.
		if unsupported > 0 && unsupported == len(supervisor.provisioners) {
			return development.ProvisionPlan{}, ErrProvisionUnsupported
		}
		return development.ProvisionPlan{}, ErrProvisionUnavailable
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, _ := candidates[i].OutputClass.Tier()
		right, _ := candidates[j].OutputClass.Tier()
		if left != right {
			return left < right
		}
		return candidates[i].Digest < candidates[j].Digest
	})
	return candidates[0], nil
}

func provisionReady(objective development.Objective, step development.ObjectiveStep, provision development.ProvisioningAttempt, catalog development.EnvironmentCatalog) bool {
	for _, environment := range catalog.Environments() {
		if environment.Class == provision.Plan.OutputClass && objective.Policy.Allows(environment) &&
			environment.Digest != provision.Plan.BaseEnvironmentDigest && len(environment.Capabilities.Missing(step.Requirements)) == 0 {
			return true
		}
	}
	return false
}

func (supervisor *Supervisor) persistProvision(next development.Objective, stepID, provisionID string) (development.Objective, error) {
	persisted, _, err := supervisor.store.SaveDevelopmentObjective(next)
	if err == nil {
		return persisted, nil
	}
	current, found, readErr := supervisor.store.DevelopmentObjective(next.ObjectiveID)
	if readErr == nil && found {
		expected, ok := objectiveStep(next, stepID)
		actual, present := objectiveStep(current, stepID)
		if ok && present && len(expected.Provisioning) > 0 && len(actual.Provisioning) > 0 {
			left := expected.Provisioning[len(expected.Provisioning)-1]
			right := actual.Provisioning[len(actual.Provisioning)-1]
			if left.ProvisionID == provisionID && right.ProvisionID == provisionID && left.SourceDigest == right.SourceDigest &&
				left.Plan.Digest == right.Plan.Digest && left.JobID == right.JobID && left.JobFence == right.JobFence &&
				left.State == right.State && left.ReceiptDigest == right.ReceiptDigest && left.Failure == right.Failure {
				return current, nil
			}
		}
	}
	return development.Objective{}, err
}

func requirementIDs(requirements []development.Requirement) []development.CapabilityID {
	ids := make([]development.CapabilityID, len(requirements))
	for i, requirement := range requirements {
		ids[i] = requirement.ID
	}
	return ids
}

func provisionIdentity(objective development.Objective, step development.ObjectiveStep, sourceDigest string, plan development.ProvisionPlan) string {
	parent := ""
	if len(step.Provisioning) != 0 {
		parent = step.Provisioning[len(step.Provisioning)-1].ProvisionID
	}
	return "provision-" + digestStrings("aeontra-provision-identity-v1", objective.ObjectiveID, objective.Scope.Project, objective.Scope.Target,
		step.StepID, sourceDigest, parent, plan.Digest)[:32]
}

func provisionSpec(objective development.Objective, provision development.ProvisioningAttempt) workqueue.Spec {
	return workqueue.Spec{IdempotencyKey: "development:" + provision.ProvisionID,
		Workspace: "development-" + digestStrings("aeontra-provision-workspace-v1", objective.Scope.Project, objective.Scope.Target)[:32],
		Pool:      provision.Plan.Pool, Profile: provision.Plan.Profile,
		PayloadHash: "sha256:" + digestStrings("aeontra-provision-payload-v1", objective.ObjectiveID, objective.Scope.Project, objective.Scope.Target,
			provision.ProvisionID, provision.SourceDigest, provision.Plan.Digest)}
}

func matchesProvisionJob(job workqueue.Job, spec workqueue.Spec) bool {
	return job.ID != "" && job.IdempotencyKey == spec.IdempotencyKey && job.Workspace == spec.Workspace &&
		job.Pool == spec.Pool && job.Profile == spec.Profile && job.PayloadHash == spec.PayloadHash
}

func provisionReceiptDigest(job workqueue.Job) string {
	return "sha256:" + digestStrings("aeontra-provision-receipt-v1", job.ID, job.PayloadHash, fmt.Sprint(job.Fence), string(job.State), job.ResultRef)
}

func digestStrings(domain string, values ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write([]byte{0})
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
