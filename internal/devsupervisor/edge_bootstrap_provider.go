package devsupervisor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

const (
	edgeBootstrapProvider    = "edge-toolchain"
	edgeBootstrapPool        = "development.toolchain"
	edgeBootstrapProfile     = "official-toolchain-v1"
	edgeBootstrapRuntime     = "linux-workcell"
	edgeBootstrapStatusLimit = 8192
)

var bootstrapNumber = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})$`)

// EdgeBootstrapOperations is the existing durable Edge operation journal. The
// provider creates only closed bootstrap and process operations in the pinned
// workcell; it does not own a scheduler or a second journal.
type EdgeBootstrapOperations interface {
	CreateOperation(string, edge.OperationKind, edge.OperationRequest) (edge.Operation, bool, error)
	OperationStatus(string) (edge.Operation, error)
	OperationByIdempotency(string, edge.OperationKind, string) (edge.Operation, bool, error)
	LatestDevelopmentProcessOperation(string, edge.OperationKind, edge.OperationRequest) (edge.Operation, bool, error)
	RequestOperationCancel(string) (edge.Operation, error)
}

type EdgeBootstrapQueue interface {
	Get(string) (workqueue.Job, bool, error)
	DevelopmentProvisionOwner(string) (development.Objective, string, bool, error)
}

type EdgeBootstrapProvider struct {
	operations EdgeBootstrapOperations
	queue      EdgeBootstrapQueue
}

func NewEdgeBootstrapProvider(operations EdgeBootstrapOperations, queue EdgeBootstrapQueue) *EdgeBootstrapProvider {
	if operations == nil || queue == nil {
		return nil
	}
	return &EdgeBootstrapProvider{operations: operations, queue: queue}
}

var _ Provisioner = (*EdgeBootstrapProvider)(nil)
var _ ProvisionExecutor = (*EdgeBootstrapProvider)(nil)

type bootstrapSelector struct {
	toolchain  string
	capability development.CapabilityID
	version    []string
}

func (provider *EdgeBootstrapProvider) Plans(ctx context.Context, objective development.Objective, step development.ObjectiveStep, catalog development.EnvironmentCatalog) ([]development.ProvisionPlan, error) {
	if provider == nil || ctx == nil || ctx.Err() != nil || !catalog.Valid() || !objective.Scope.Pinned() ||
		!objective.Policy.AllowsClass(development.ClassWorkcell) || len(step.Requirements) == 0 {
		return nil, ErrProvisionUnavailable
	}
	var workcell development.EnvironmentAttestation
	for _, environment := range catalog.Environments() {
		if environment.Class == development.ClassWorkcell && environment.EnvironmentID == "workcell:"+objective.Scope.Anchor.WorkspaceID &&
			environment.Generation == objective.Scope.Anchor.Generation && objective.Policy.Allows(environment) {
			if workcell.EnvironmentID != "" {
				return nil, ErrProvisionUnavailable
			}
			workcell = environment
		}
	}
	if workcell.EnvironmentID == "" {
		return nil, ErrProvisionUnavailable
	}
	requirements := requirementIDs(step.Requirements)
	missing := workcell.Capabilities.Missing(step.Requirements)
	if len(missing) == 0 {
		return nil, nil
	}
	// Unsupported missing properties are rejected before the plan can enqueue
	// any work. Also reject conflicting requested pins so the durable plan can
	// reconstruct the exact selector set without another catalog read.
	for _, id := range missing {
		if _, ok, err := parseBootstrapSelector(id); err != nil || !ok {
			return nil, ErrProvisionUnsupported
		}
	}
	selectors, err := bootstrapSelectors(requirements)
	if err != nil {
		return nil, ErrProvisionUnsupported
	}
	if len(selectors) == 0 || len(selectors) > 2 {
		return nil, ErrProvisionUnavailable
	}
	plan, err := development.NewProvisionPlan(edgeBootstrapProvider, edgeBootstrapPool, edgeBootstrapProfile,
		development.ClassWorkcell, workcell.Digest, requirements)
	if err != nil {
		return nil, ErrProvisionUnavailable
	}
	return []development.ProvisionPlan{plan}, nil
}

func (provider *EdgeBootstrapProvider) Reconcile(ctx context.Context, request ProvisionRequest) (ProvisionEffect, error) {
	job, err := provider.validateLease(ctx, request, false)
	if err != nil {
		return ProvisionEffect{}, err
	}
	selectors, err := selectorsForPlan(request.Provision.Plan)
	if err != nil {
		return ProvisionEffect{Failure: development.FailureReconciliationNeeded}, nil
	}
	var receipts []string
	for _, selector := range selectors {
		effect, receipt, err := provider.reconcileSelector(ctx, request, selector)
		if err != nil || effect.Pending || effect.Failure != "" {
			return effect, err
		}
		receipts = append(receipts, receipt)
	}
	if len(receipts) == 0 {
		return ProvisionEffect{Failure: development.FailureReconciliationNeeded}, nil
	}
	return ProvisionEffect{ResultRef: bootstrapResultRef(request.Provision, job, receipts)}, nil
}

func (provider *EdgeBootstrapProvider) Cancel(ctx context.Context, request ProvisionRequest) (ProvisionEffect, error) {
	_, err := provider.validateLease(ctx, request, true)
	if err != nil {
		return ProvisionEffect{}, err
	}
	selectors, err := selectorsForPlan(request.Provision.Plan)
	if err != nil {
		return ProvisionEffect{Failure: development.FailureReconciliationNeeded}, nil
	}
	var receipts []string
	for _, selector := range selectors {
		effect, receipt, err := provider.cancelSelector(ctx, request, selector)
		if err != nil || effect.Pending || effect.Failure != "" {
			return effect, err
		}
		receipts = append(receipts, receipt)
	}
	return ProvisionEffect{ResultRef: bootstrapResultRef(request.Provision, request.Lease.Job, append([]string{"cancelled"}, receipts...))}, nil
}

func (provider *EdgeBootstrapProvider) validateLease(ctx context.Context, request ProvisionRequest, cancelling bool) (workqueue.Job, error) {
	if provider == nil || provider.operations == nil || provider.queue == nil || ctx == nil || ctx.Err() != nil ||
		!request.Scope.Pinned() || !request.Provision.Valid() ||
		(request.Provision.State != development.ProvisioningQueued && !(cancelling && request.Provision.State == development.ProvisioningCancelled)) ||
		request.Provision.Plan.Provider != edgeBootstrapProvider || request.Provision.Plan.Pool != edgeBootstrapPool ||
		request.Provision.Plan.Profile != edgeBootstrapProfile || request.Provision.Plan.OutputClass != development.ClassWorkcell ||
		!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(request.Provision.Plan.BaseEnvironmentDigest) {
		return workqueue.Job{}, ErrProvisionConflict
	}
	lease := request.Lease
	if lease.ID == "" || lease.Fence == 0 || lease.Job.ID == "" {
		return workqueue.Job{}, ErrProvisionConflict
	}
	job, found, err := provider.queue.Get(lease.Job.ID)
	if err != nil {
		return workqueue.Job{}, err
	}
	owner, stepID, ownerFound, err := provider.queue.DevelopmentProvisionOwner(request.Provision.ProvisionID)
	if err != nil {
		return workqueue.Job{}, err
	}
	if !found || !ownerFound || owner.Scope != request.Scope {
		return workqueue.Job{}, ErrProvisionConflict
	}
	var ownedProvision development.ProvisioningAttempt
	stepFound := false
	for _, step := range owner.Steps {
		if step.StepID != stepID {
			continue
		}
		for _, attempt := range step.Provisioning {
			if attempt.ProvisionID == request.Provision.ProvisionID {
				ownedProvision, stepFound = attempt, true
			}
		}
	}
	if !stepFound || !reflect.DeepEqual(ownedProvision, request.Provision) {
		return workqueue.Job{}, ErrProvisionConflict
	}
	spec := provisionSpec(owner, ownedProvision)
	if job.State != workqueue.StateLeased || job.ID != lease.Job.ID || job.LeaseID != lease.ID || job.Fence != lease.Fence ||
		job.LeaseHolder == "" || job.LeaseHolder != lease.Job.LeaseHolder || !job.LeaseExpiresAt.After(time.Now().UTC()) ||
		job.CancelRequested != cancelling || !matchesProvisionJob(job, spec) || !matchesProvisionJob(lease.Job, spec) {
		return workqueue.Job{}, ErrProvisionConflict
	}
	return job, nil
}

func (provider *EdgeBootstrapProvider) reconcileSelector(ctx context.Context, request ProvisionRequest, selector bootstrapSelector) (ProvisionEffect, string, error) {
	if err := ctx.Err(); err != nil {
		return ProvisionEffect{}, "", err
	}
	scope, provision := request.Scope, request.Provision
	anchor := scope.Anchor
	resolveKey := bootstrapOperationKey("resolve", provision, selector)
	resolveRequest := bootstrapOperationRequest(scope, selector, nil, resolveKey)
	resolved, found, err := provider.createOrFind(anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveKey, resolveRequest)
	if err != nil {
		return ProvisionEffect{}, "", err
	}
	if !found {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if !validBootstrapOperation(resolved, anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveRequest) {
		return reconciliationFailure(), "", nil
	}
	switch resolved.State {
	case edge.OperationQueued, edge.OperationLeased:
		return ProvisionEffect{Pending: true}, "", nil
	case edge.OperationFailed, edge.OperationCancelled:
		return reconciliationFailure(), "", nil
	case edge.OperationSucceeded:
	default:
		return reconciliationFailure(), "", nil
	}
	resolutionBinding := resolved.Result.DevelopmentBootstrap
	if !validProjectIdentity(resolved.Result, scope) || resolutionBinding == nil || !resolutionBinding.Valid(true) ||
		resolutionBinding.Anchor != anchor || resolutionBinding.CapabilityID != selector.capability {
		return reconciliationFailure(), "", nil
	}
	resolution := *resolutionBinding.Resolution
	startKey := bootstrapOperationKey("start", provision, selector)
	startRequest := bootstrapOperationRequest(scope, selector, resolutionBinding, startKey)
	if err := ctx.Err(); err != nil {
		return ProvisionEffect{}, "", err
	}
	if _, err := provider.validateLease(ctx, request, false); err != nil {
		return ProvisionEffect{}, "", err
	}
	started, found, err := provider.createOrFind(anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startKey, startRequest)
	if err != nil {
		return ProvisionEffect{}, "", err
	}
	if !found {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if !validBootstrapOperation(started, anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startRequest) {
		return reconciliationFailure(), "", nil
	}
	if started.State == edge.OperationQueued || started.State == edge.OperationLeased {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if started.State != edge.OperationSucceeded {
		// A failed or interrupted start is ambiguous across the Edge journal and
		// process journal. The private recovery-only operation can only recover a
		// process with the captured identity; it never starts another installer.
		if err := ctx.Err(); err != nil {
			return ProvisionEffect{}, "", err
		}
		if _, err := provider.validateLease(ctx, request, false); err != nil {
			return ProvisionEffect{}, "", err
		}
		recovery, found, err := provider.recoverStart(scope, selector, startKey, started, resolutionBinding)
		if err != nil {
			return ProvisionEffect{}, "", err
		}
		if !found {
			return ProvisionEffect{Pending: true}, "", nil
		}
		if recovery.State == edge.OperationQueued || recovery.State == edge.OperationLeased {
			return ProvisionEffect{Pending: true}, "", nil
		}
		if recovery.State != edge.OperationSucceeded || !validBootstrapOperation(recovery, anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, recovery.Request) ||
			!edge.BootstrapBindingsEqual(recovery.Result.DevelopmentBootstrap, resolutionBinding) || !validProjectIdentity(recovery.Result, scope) {
			return reconciliationFailure(), "", nil
		}
		started = recovery
	}
	processID := started.Result.BackgroundProcessID
	if processID == "" || !edge.BootstrapBindingsEqual(started.Result.DevelopmentBootstrap, resolutionBinding) || !validProjectIdentity(started.Result, scope) {
		return reconciliationFailure(), "", nil
	}
	status, found, err := provider.createProcessOperation(scope, edge.OperationProjectProcessStatus, processID, true)
	if err != nil {
		return ProvisionEffect{}, "", err
	}
	if !found || status.State == edge.OperationQueued || status.State == edge.OperationLeased {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if !validProcessOperation(status, anchor.DeviceID, edge.OperationProjectProcessStatus, scope, processID) || status.State != edge.OperationSucceeded {
		return reconciliationFailure(), "", nil
	}
	result := status.Result
	if result.BackgroundProcessState == "running" || result.BackgroundProcessState == "stopping" {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if result.BackgroundProcessState != "exited" || !result.BackgroundExitKnown || result.BackgroundExitCode != 0 ||
		result.BackgroundTerminalSignal != "" || result.BackgroundReason != "" || !result.BackgroundStdoutEOF || !result.BackgroundStderrEOF || result.BackgroundStdoutTruncated ||
		result.BackgroundStderrTruncated || !verifiedBootstrapOutput(resolution, result.BackgroundStdout) {
		return reconciliationFailure(), "", nil
	}
	return ProvisionEffect{}, resolutionBinding.ResolutionDigest + ":" + processID, nil
}

func (provider *EdgeBootstrapProvider) recoverStart(scope development.ObjectiveScope, selector bootstrapSelector, originalKey string, original edge.Operation, binding *edge.ProjectDevelopmentBootstrapBinding) (edge.Operation, bool, error) {
	if original.ID == "" || original.Request.IdempotencyKey != originalKey || binding == nil || !binding.Valid(true) {
		return edge.Operation{}, false, ErrProvisionConflict
	}
	key := "bootstrap-recover:" + digestStrings("aeontra-development-bootstrap-recovery-v1", original.ID, originalKey, string(selector.capability))[:32]
	request := bootstrapOperationRequest(scope, selector, binding, key)
	request.DevelopmentRecoveryOperationID = original.ID
	request.DevelopmentRecoveryIdempotencyKey = originalKey
	return provider.createOrFind(scope.Anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, key, request)
}

func (provider *EdgeBootstrapProvider) cancelSelector(ctx context.Context, request ProvisionRequest, selector bootstrapSelector) (ProvisionEffect, string, error) {
	if err := ctx.Err(); err != nil {
		return ProvisionEffect{}, "", err
	}
	scope, provision := request.Scope, request.Provision
	anchor := scope.Anchor
	resolveKey := bootstrapOperationKey("resolve", provision, selector)
	resolveRequest := bootstrapOperationRequest(scope, selector, nil, resolveKey)
	resolve, found, err := provider.operations.OperationByIdempotency(anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveKey)
	if err != nil {
		return ProvisionEffect{}, "", err
	}
	if found {
		if !validBootstrapOperation(resolve, anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveRequest) {
			return reconciliationFailure(), "", nil
		}
		if _, err := provider.validateLease(ctx, request, true); err != nil {
			return ProvisionEffect{}, "", err
		}
		pending, failed, err := provider.cancelOperation(resolve)
		if err != nil || pending || failed {
			return cancelEffect(pending, failed), "", err
		}
	}
	startKey := bootstrapOperationKey("start", provision, selector)
	start, found, err := provider.operations.OperationByIdempotency(anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startKey)
	if err != nil {
		return ProvisionEffect{}, "", err
	}
	if !found {
		return ProvisionEffect{}, "cancelled:" + string(selector.capability), nil
	}
	if start.DeviceID != anchor.DeviceID || start.Kind != edge.OperationProjectDevelopmentBootstrapStart || start.Request.IdempotencyKey != startKey ||
		!sameBootstrapSelector(start.Request.DevelopmentBootstrap, anchor, selector) {
		return reconciliationFailure(), "", nil
	}
	if start.State == edge.OperationQueued || start.State == edge.OperationLeased {
		if _, err := provider.validateLease(ctx, request, true); err != nil {
			return ProvisionEffect{}, "", err
		}
		pending, failed, err := provider.cancelOperation(start)
		if err != nil || pending || failed {
			return cancelEffect(pending, failed), "", err
		}
		start, err = provider.operations.OperationStatus(start.ID)
		if err != nil {
			return ProvisionEffect{}, "", err
		}
	}
	if start.State == edge.OperationFailed || start.State == edge.OperationCancelled {
		binding := start.Request.DevelopmentBootstrap
		if binding == nil || !binding.Valid(true) {
			return reconciliationFailure(), "", nil
		}
		recovery, found, err := provider.recoverStart(scope, selector, startKey, start, binding)
		if err != nil || !found {
			return ProvisionEffect{Pending: !found}, "", err
		}
		if recovery.State == edge.OperationQueued || recovery.State == edge.OperationLeased {
			return ProvisionEffect{Pending: true}, "", nil
		}
		if recovery.State != edge.OperationSucceeded || !edge.BootstrapBindingsEqual(recovery.Result.DevelopmentBootstrap, binding) || !validProjectIdentity(recovery.Result, scope) {
			return reconciliationFailure(), "", nil
		}
		start = recovery
	}
	if start.State != edge.OperationSucceeded || !validProjectIdentity(start.Result, scope) ||
		!edge.BootstrapBindingsEqual(start.Result.DevelopmentBootstrap, start.Request.DevelopmentBootstrap) || start.Result.BackgroundProcessID == "" {
		return reconciliationFailure(), "", nil
	}
	processID := start.Result.BackgroundProcessID
	status, found, err := provider.createProcessOperation(scope, edge.OperationProjectProcessStatus, processID, false)
	if err != nil || !found || status.State == edge.OperationQueued || status.State == edge.OperationLeased {
		return ProvisionEffect{Pending: true}, "", err
	}
	if !validProcessOperation(status, anchor.DeviceID, edge.OperationProjectProcessStatus, scope, processID) || status.State != edge.OperationSucceeded {
		return reconciliationFailure(), "", nil
	}
	processState := status.Result.BackgroundProcessState
	if processState == "exited" || processState == "stopped" || processState == "failed" {
		if processState == "failed" || !status.Result.BackgroundExitKnown && processState == "exited" {
			return reconciliationFailure(), "", nil
		}
		return ProvisionEffect{}, "cancelled:" + processID, nil
	}
	if processState == "stopping" {
		refreshed, found, err := provider.createProcessOperation(scope, edge.OperationProjectProcessStatus, processID, true)
		if err != nil || !found || refreshed.State == edge.OperationQueued || refreshed.State == edge.OperationLeased {
			return ProvisionEffect{Pending: true}, "", err
		}
		if !validProcessOperation(refreshed, anchor.DeviceID, edge.OperationProjectProcessStatus, scope, processID) || refreshed.State != edge.OperationSucceeded {
			return reconciliationFailure(), "", nil
		}
		if refreshed.Result.BackgroundProcessState != "stopped" && refreshed.Result.BackgroundProcessState != "exited" {
			return ProvisionEffect{Pending: true}, "", nil
		}
		if refreshed.Result.BackgroundProcessState == "exited" && !refreshed.Result.BackgroundExitKnown {
			return reconciliationFailure(), "", nil
		}
		return ProvisionEffect{}, "cancelled:" + processID, nil
	}
	if processState != "running" {
		return reconciliationFailure(), "", nil
	}
	if err := ctx.Err(); err != nil {
		return ProvisionEffect{}, "", err
	}
	if _, err := provider.validateLease(ctx, request, true); err != nil {
		return ProvisionEffect{}, "", err
	}
	stop, found, err := provider.createProcessOperation(scope, edge.OperationProjectProcessStop, processID)
	if err != nil || !found {
		return ProvisionEffect{Pending: true}, "", err
	}
	if stop.State == edge.OperationQueued || stop.State == edge.OperationLeased {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if !validProcessOperation(stop, anchor.DeviceID, edge.OperationProjectProcessStop, scope, processID) || stop.State != edge.OperationSucceeded {
		return reconciliationFailure(), "", nil
	}
	if stop.Result.BackgroundProcessState == "running" || stop.Result.BackgroundProcessState == "stopping" {
		return ProvisionEffect{Pending: true}, "", nil
	}
	if stop.Result.BackgroundProcessState != "stopped" && stop.Result.BackgroundProcessState != "exited" {
		return reconciliationFailure(), "", nil
	}
	return ProvisionEffect{}, "cancelled:" + processID, nil
}

func (provider *EdgeBootstrapProvider) cancelOperation(operation edge.Operation) (pending, failed bool, err error) {
	if operation.State != edge.OperationQueued && operation.State != edge.OperationLeased {
		return false, operation.State == edge.OperationFailed, nil
	}
	cancelled, err := provider.operations.RequestOperationCancel(operation.ID)
	if err != nil {
		current, statusErr := provider.operations.OperationStatus(operation.ID)
		if statusErr != nil {
			return true, false, nil
		}
		cancelled = current
	}
	if cancelled.ID != operation.ID || cancelled.DeviceID != operation.DeviceID || cancelled.Kind != operation.Kind || !reflect.DeepEqual(cancelled.Request, operation.Request) {
		return false, true, nil
	}
	if cancelled.State == edge.OperationQueued && cancelled.CancelRequested {
		return false, false, nil
	}
	if cancelled.State == edge.OperationLeased || cancelled.State == edge.OperationQueued {
		return true, false, nil
	}
	return false, cancelled.State == edge.OperationFailed, nil
}

func cancelEffect(pending, failed bool) ProvisionEffect {
	if failed {
		return reconciliationFailure()
	}
	return ProvisionEffect{Pending: pending}
}

func (provider *EdgeBootstrapProvider) createProcessOperation(scope development.ObjectiveScope, kind edge.OperationKind, processID string, refreshRunningStatus ...bool) (edge.Operation, bool, error) {
	request := edge.OperationRequest{Alias: scope.Project, TargetAlias: scope.Target, Profile: edgeBootstrapRuntime, BackgroundProcessID: processID,
		OutputLimit: edgeBootstrapStatusLimit}
	if kind == edge.OperationProjectProcessStop {
		request.OutputLimit = 0
		request.GraceSeconds = 10
	}
	latest, found, err := provider.operations.LatestDevelopmentProcessOperation(scope.Anchor.DeviceID, kind, request)
	if err != nil {
		return edge.Operation{}, false, err
	}
	if found {
		if latest.DeviceID != scope.Anchor.DeviceID || latest.Kind != kind || !reflect.DeepEqual(latest.Request, request) {
			return edge.Operation{}, false, ErrProvisionConflict
		}
		if latest.State == edge.OperationQueued || latest.State == edge.OperationLeased {
			latest, err = provider.operations.OperationStatus(latest.ID)
			if err != nil {
				return edge.Operation{}, false, err
			}
			if latest.DeviceID != scope.Anchor.DeviceID || latest.Kind != kind || !reflect.DeepEqual(latest.Request, request) {
				return edge.Operation{}, false, ErrProvisionConflict
			}
			if latest.State == edge.OperationQueued || latest.State == edge.OperationLeased {
				return latest, true, nil
			}
		}
		if latest.State != edge.OperationSucceeded {
			return latest, true, nil
		}
		state := latest.Result.BackgroundProcessState
		refresh := len(refreshRunningStatus) > 0 && refreshRunningStatus[0]
		if kind == edge.OperationProjectProcessStatus && (!refresh || state != "running" && state != "stopping") {
			return latest, true, nil
		}
		if kind == edge.OperationProjectProcessStop && state != "running" && state != "stopping" {
			return latest, true, nil
		}
	}
	operation, _, err := provider.operations.CreateOperation(scope.Anchor.DeviceID, kind, request)
	if err != nil {
		// Status/stop are read-only or idempotent for the captured PID. A lost
		// create ACK is reconciled through the exact request digest in the same
		// durable operation journal before retrying.
		operation, found, lookupErr := provider.operations.LatestDevelopmentProcessOperation(scope.Anchor.DeviceID, kind, request)
		if lookupErr != nil {
			return edge.Operation{}, false, lookupErr
		}
		if !found {
			return edge.Operation{}, false, err
		}
		return operation, true, nil
	}
	return operation, true, nil
}

func (provider *EdgeBootstrapProvider) createOrFind(device string, kind edge.OperationKind, key string, request edge.OperationRequest) (edge.Operation, bool, error) {
	operation, found, err := provider.operations.OperationByIdempotency(device, kind, key)
	if err != nil {
		return edge.Operation{}, false, err
	}
	if found {
		if !validBootstrapOperation(operation, device, kind, request) {
			return edge.Operation{}, false, ErrProvisionConflict
		}
		return operation, true, nil
	}
	operation, _, err = provider.operations.CreateOperation(device, kind, request)
	if err == nil {
		if !validBootstrapOperation(operation, device, kind, request) {
			return edge.Operation{}, false, ErrProvisionConflict
		}
		return operation, true, nil
	}
	// Create may commit and lose its acknowledgement. Recover only by the exact
	// device/kind/key and compare the entire normalized request before reuse.
	operation, found, lookupErr := provider.operations.OperationByIdempotency(device, kind, key)
	if lookupErr != nil {
		return edge.Operation{}, false, lookupErr
	}
	if !found {
		return edge.Operation{}, false, nil
	}
	if !validBootstrapOperation(operation, device, kind, request) {
		return edge.Operation{}, false, ErrProvisionConflict
	}
	return operation, true, nil
}

func bootstrapOperationRequest(scope development.ObjectiveScope, selector bootstrapSelector, binding *edge.ProjectDevelopmentBootstrapBinding, key string) edge.OperationRequest {
	if binding == nil {
		binding = &edge.ProjectDevelopmentBootstrapBinding{Version: 1, Anchor: scope.Anchor, CapabilityID: selector.capability}
	} else {
		copy := *binding
		binding = &copy
	}
	return edge.OperationRequest{Alias: scope.Project, TargetAlias: scope.Target, Profile: edgeBootstrapRuntime, IdempotencyKey: key, DevelopmentBootstrap: binding}
}

func bootstrapOperationKey(action string, provision development.ProvisioningAttempt, selector bootstrapSelector) string {
	return "bootstrap-" + action + ":" + digestStrings("aeontra-edge-bootstrap-operation-v1", provision.ProvisionID, provision.Plan.Digest, string(selector.capability))[:32]
}

func validBootstrapOperation(operation edge.Operation, device string, kind edge.OperationKind, request edge.OperationRequest) bool {
	return operation.ID != "" && operation.DeviceID == device && operation.Kind == kind && reflect.DeepEqual(operation.Request, request)
}

func validProjectIdentity(result edge.OperationResult, scope development.ObjectiveScope) bool {
	anchor := scope.Anchor
	return result.WorkspaceID == anchor.WorkspaceID && result.ProjectAlias == scope.Project && result.ProjectTarget == scope.Target &&
		result.ProjectOwner == anchor.Owner && result.ProjectRepository == anchor.Repository &&
		result.ProjectProfile == edgeBootstrapRuntime && result.ProjectMode == "dev" &&
		(result.ProjectState == "ready" || result.ProjectState == "dirty" || result.ProjectState == "registered")
}

func validProcessOperation(operation edge.Operation, device string, kind edge.OperationKind, scope development.ObjectiveScope, processID string) bool {
	expected := edge.OperationRequest{Alias: scope.Project, TargetAlias: scope.Target, Profile: edgeBootstrapRuntime, BackgroundProcessID: processID,
		OutputLimit: edgeBootstrapStatusLimit}
	if kind == edge.OperationProjectProcessStop {
		expected.OutputLimit = 0
		expected.GraceSeconds = 10
	}
	return operation.ID != "" && operation.DeviceID == device && operation.Kind == kind && reflect.DeepEqual(operation.Request, expected) &&
		validProjectIdentity(operation.Result, scope) && operation.Result.BackgroundProcessID == processID
}

func sameBootstrapSelector(binding *edge.ProjectDevelopmentBootstrapBinding, anchor development.WorkspaceAnchor, selector bootstrapSelector) bool {
	return binding != nil && binding.Valid(binding.Resolution != nil) && binding.Anchor == anchor && binding.CapabilityID == selector.capability
}

func reconciliationFailure() ProvisionEffect {
	return ProvisionEffect{Failure: development.FailureReconciliationNeeded}
}

func selectorsForPlan(plan development.ProvisionPlan) ([]bootstrapSelector, error) {
	if !plan.Valid() || plan.Provider != edgeBootstrapProvider || plan.Pool != edgeBootstrapPool || plan.Profile != edgeBootstrapProfile || plan.OutputClass != development.ClassWorkcell {
		return nil, ErrProvisionConflict
	}
	selectors, err := bootstrapSelectors(plan.Capabilities)
	if err != nil || len(selectors) == 0 || len(selectors) > 2 {
		return nil, ErrProvisionConflict
	}
	return selectors, nil
}

func bootstrapSelectors(ids []development.CapabilityID) ([]bootstrapSelector, error) {
	byToolchain := make(map[string]bootstrapSelector, 2)
	for _, id := range ids {
		selector, ok, err := parseBootstrapSelector(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		previous, exists := byToolchain[selector.toolchain]
		if !exists {
			byToolchain[selector.toolchain] = selector
			continue
		}
		if selector.toolchain == "go" && numericPrefix(previous.version, selector.version) {
			if len(selector.version) > len(previous.version) {
				byToolchain[selector.toolchain] = selector
			}
			continue
		}
		if selector.toolchain == "go" && numericPrefix(selector.version, previous.version) {
			continue
		}
		if selector.version[0] == "rust" && previous.version[0] == "rust" && reflect.DeepEqual(selector.version[1:], previous.version[1:]) {
			continue
		}
		if reflect.DeepEqual(selector.version, previous.version) {
			continue
		}
		return nil, errors.New("development bootstrap requirements conflict")
	}
	if len(byToolchain) > 2 {
		return nil, errors.New("development bootstrap selector count exceeds policy")
	}
	selectors := make([]bootstrapSelector, 0, len(byToolchain))
	for _, selector := range byToolchain {
		selectors = append(selectors, selector)
	}
	sort.Slice(selectors, func(i, j int) bool { return selectors[i].toolchain < selectors[j].toolchain })
	return selectors, nil
}

func parseBootstrapSelector(id development.CapabilityID) (bootstrapSelector, bool, error) {
	value := string(id)
	toolchain, suffix := "", ""
	switch {
	case strings.HasPrefix(value, "toolchain.go.v"):
		toolchain, suffix = "go", strings.TrimPrefix(value, "toolchain.go.v")
	case strings.HasPrefix(value, "toolchain.rust.v"):
		toolchain, suffix = "rust", strings.TrimPrefix(value, "toolchain.rust.v")
	case strings.HasPrefix(value, "toolchain.cargo.v"):
		toolchain, suffix = "cargo", strings.TrimPrefix(value, "toolchain.cargo.v")
	default:
		return bootstrapSelector{}, false, nil
	}
	parts := strings.Split(suffix, "-")
	precisionOK := toolchain == "go" && (len(parts) == 2 || len(parts) == 3) || (toolchain == "rust" || toolchain == "cargo") && len(parts) == 3
	if !precisionOK {
		return bootstrapSelector{}, false, errors.New("development bootstrap selector precision is invalid")
	}
	for _, part := range parts {
		if !bootstrapNumber.MatchString(part) {
			return bootstrapSelector{}, false, errors.New("development bootstrap selector is not numeric")
		}
	}
	if toolchain == "go" {
		if parts[0] != "1" || !development.BootstrapCapabilitySupported(id) {
			return bootstrapSelector{}, false, errors.New("go bootstrap selector is unsupported")
		}
		return bootstrapSelector{toolchain: "go", capability: id, version: parts}, true, nil
	}
	if toolchain == "rust" {
		if !development.BootstrapCapabilitySupported(id) {
			return bootstrapSelector{}, false, errors.New("rust bootstrap selector is unsupported")
		}
		return bootstrapSelector{toolchain: "rust", capability: id, version: parts}, true, nil
	}
	// rustup installs rustc and cargo together. Cargo requirements map to the
	// same exact Rust selection while the output must verify both binaries.
	rustID := development.CapabilityID("toolchain.rust.v" + strings.Join(parts, "-"))
	if !development.BootstrapCapabilitySupported(rustID) {
		return bootstrapSelector{}, false, errors.New("cargo bootstrap selector is unsupported")
	}
	return bootstrapSelector{toolchain: "rust", capability: rustID, version: parts}, true, nil
}

func numericPrefix(prefix, full []string) bool {
	if len(prefix) > len(full) {
		return false
	}
	for index := range prefix {
		if prefix[index] != full[index] {
			return false
		}
	}
	return true
}

func verifiedBootstrapOutput(resolution development.BootstrapResolution, output string) bool {
	if development.ValidateBootstrapResolution(resolution) != nil {
		return false
	}
	lines := strings.Split(output, "\n")
	versionLine, marker := "", ""
	switch resolution.Toolchain {
	case "go":
		versionLine = "go version go" + resolution.Version + " linux/" + resolution.Platform
		marker = "mcp-devbox-bootstrap-verified=go:" + resolution.Version
	case "rust":
		marker = "mcp-devbox-bootstrap-verified=rust:" + resolution.Version + ":cargo:" + resolution.Version
	default:
		return false
	}
	versionOK, rustcOK, cargoOK, markerOK := false, false, false, false
	for _, line := range lines {
		if line == versionLine {
			versionOK = true
		}
		if line == marker {
			markerOK = true
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "rustc" && fields[1] == resolution.Version {
			rustcOK = true
		}
		if len(fields) >= 2 && fields[0] == "cargo" && fields[1] == resolution.Version {
			cargoOK = true
		}
	}
	if !markerOK || (resolution.Toolchain == "go" && !versionOK) || (resolution.Toolchain == "rust" && (!rustcOK || !cargoOK)) {
		return false
	}
	return true
}

func bootstrapResultRef(provision development.ProvisioningAttempt, job workqueue.Job, receipts []string) string {
	values := []string{provision.ProvisionID, provision.Plan.Digest, job.ID, fmt.Sprint(job.Fence)}
	values = append(values, receipts...)
	return "rs_" + digestStrings("aeontra-edge-toolchain-receipt-v1", values...)[:32]
}
