package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/devsupervisor"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type developmentCatalogSnapshot struct {
	scope        development.ObjectiveScope
	environments []development.EnvironmentAttestation
}

func (snapshot developmentCatalogSnapshot) Catalog(ctx context.Context, objective development.Objective) (development.EnvironmentCatalog, error) {
	if ctx == nil || ctx.Err() != nil || objective.Scope != snapshot.scope {
		return development.EnvironmentCatalog{}, devsupervisor.ErrCatalogUnavailable
	}
	return development.NewEnvironmentCatalog(snapshot.environments...)
}

func (s *Server) developmentBootstrapSupervisor(objective development.Objective, environments []development.EnvironmentAttestation) (*devsupervisor.Supervisor, *devsupervisor.EdgeBootstrapProvider, error) {
	operations, ok := s.edgeOperations.(devsupervisor.EdgeBootstrapOperations)
	if !ok {
		return nil, nil, devsupervisor.ErrProvisionUnavailable
	}
	provider := devsupervisor.NewEdgeBootstrapProvider(operations, s.workQueue)
	if provider == nil {
		return nil, nil, devsupervisor.ErrProvisionUnavailable
	}
	supervisor, err := devsupervisor.New(s.workQueue, developmentCatalogSnapshot{scope: objective.Scope, environments: environments})
	if err != nil {
		return nil, nil, err
	}
	supervisor, err = supervisor.WithProvisioning(s.workQueue, provider)
	return supervisor, provider, err
}

// A fixed official toolchain recipe is the only additional workcell effect.
// Its receipt does not supply capability; independent inspection is repeated
// before the command is planned on the newly measured environment.
func (s *Server) reconcileDevelopmentBootstrap(ctx context.Context, request workqueue.DevelopmentRequest, objective development.Objective) (development.Objective, []development.EnvironmentAttestation, bool, error) {
	op, err := s.developmentOperation(request, edge.OperationProjectDevelopmentInspect, developmentOperationKey(request, "provision-inspect:"+fmt.Sprint(objective.Revision)), edge.OperationRequest{Alias: request.Alias, TargetAlias: request.Target, Profile: "linux-workcell"})
	if err != nil {
		return objective, nil, false, err
	}
	if !developmentOperationTerminal(op.State) {
		return objective, nil, false, nil
	}
	anchor, environments, err := developmentInspection(request, op)
	if err != nil {
		return objective, nil, false, err
	}
	if anchor != objective.Scope.Anchor || op.Result.DevelopmentInspection.SourceDigest != objective.Steps[0].AcceptanceContract.SourceDigest {
		err := s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonSourceChanged)
		return objective, nil, false, err
	}
	supervisor, provider, err := s.developmentBootstrapSupervisor(objective, environments)
	if err != nil {
		return objective, environments, false, nil
	}
	result, err := supervisor.ProvisionStep(ctx, objective.ObjectiveID, "command", op.Result.DevelopmentInspection.SourceDigest)
	if errors.Is(err, devsupervisor.ErrProvisionUnsupported) {
		err := s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement)
		return objective, environments, false, err
	}
	if errors.Is(err, devsupervisor.ErrProvisionUnavailable) || errors.Is(err, devsupervisor.ErrProvisionUnverified) {
		return objective, environments, false, nil
	}
	if err != nil {
		return objective, nil, false, err
	}
	objective = result.Objective
	if result.Ready {
		return objective, environments, true, nil
	}
	job, found, err := s.workQueue.Get(result.Provision.JobID)
	if err != nil || !found {
		return objective, nil, false, errors.New("development bootstrap job unavailable")
	}
	if job.State == workqueue.StateQueued {
		lease, err := s.workQueue.LeaseNext(result.Provision.Plan.Pool, s.projectTaskHolder(), projectTaskLeaseTTL)
		if err != nil {
			return objective, nil, false, err
		}
		if lease.Job.ID != job.ID {
			return objective, environments, false, nil
		}
		job = lease.Job
	}
	if job.State != workqueue.StateLeased {
		return objective, environments, false, nil
	}
	lease := workqueue.Lease{Job: job, ID: job.LeaseID, Fence: job.Fence, Attempt: job.Attempt, ExpiresAt: job.LeaseExpiresAt}
	result, err = supervisor.ReconcileProvisionLease(ctx, objective.ObjectiveID, "command", lease, provider)
	if errors.Is(err, devsupervisor.ErrProvisionUnverified) {
		return result.Objective, environments, false, nil
	}
	if err != nil {
		return objective, nil, false, err
	}
	if result.Provision.State == development.ProvisioningFailed {
		if err := s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement); err != nil {
			return objective, nil, false, err
		}
		return result.Objective, environments, false, nil
	}
	return result.Objective, environments, result.Ready, nil
}

func (s *Server) cancelDevelopmentBootstrap(ctx context.Context, request workqueue.DevelopmentRequest, objective development.Objective) (bool, error) {
	if len(objective.Steps[0].Provisioning) == 0 {
		return true, nil
	}
	last := objective.Steps[0].Provisioning[len(objective.Steps[0].Provisioning)-1]
	if last.State == development.ProvisioningSucceeded {
		return true, nil
	}
	initial, err := s.edgeOperations.OperationStatus(request.InspectionOperationID)
	if err != nil {
		return false, err
	}
	_, environments, err := developmentInspection(request, initial)
	if err != nil {
		return false, err
	}
	supervisor, provider, err := s.developmentBootstrapSupervisor(objective, environments)
	if err != nil {
		return false, err
	}
	cancelled, err := supervisor.Cancel(ctx, objective.ObjectiveID)
	if err != nil {
		return false, err
	}
	objective = cancelled.Objective
	provision := objective.Steps[0].Provisioning[len(objective.Steps[0].Provisioning)-1]
	if provision.JobID == "" {
		return true, nil
	}
	job, found, err := s.workQueue.Get(provision.JobID)
	if err != nil || !found {
		return false, errors.New("development bootstrap cancellation job unavailable")
	}
	if job.State == workqueue.StateCancelled {
		// Lease expiry records queue cancellation without proving host stop.
		return job.Fence == 0 || job.ResultRef != "", nil
	}
	if job.State == workqueue.StateFailed {
		// Retain failure evidence; the provider can only prove an exact failed
		// resolution had no corresponding installer start, without dispatching.
		effect, err := provider.Cancel(ctx, devsupervisor.ProvisionRequest{Scope: objective.Scope, Provision: provision,
			Lease: workqueue.Lease{Job: job, Fence: job.Fence}})
		return err == nil && !effect.Pending && effect.Failure == "" && effect.ResultRef != "", err
	}
	if job.State != workqueue.StateLeased {
		return false, nil
	}
	lease := workqueue.Lease{Job: job, ID: job.LeaseID, Fence: job.Fence, Attempt: job.Attempt, ExpiresAt: job.LeaseExpiresAt}
	_, err = supervisor.ReconcileProvisionLease(ctx, objective.ObjectiveID, "command", lease, provider)
	if err != nil {
		return false, err
	}
	job, _, err = s.workQueue.Get(job.ID)
	return err == nil && job.State == workqueue.StateCancelled && job.ResultRef != "", err
}
