package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/version"
	"reflect"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/tools"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

// Scoped source inference applies only to the already registered exact Go
// runner command. Explicit caller requirements are unioned by the caller and
// never changed. Absent legacy metadata keeps the original conservative set.
func developmentCommandSourceRequirements(command projectDevelopmentBody, inspection *edge.ProjectDevelopmentInspection) ([]development.CapabilityID, error) {
	if inspection.GoCommandRequirements == nil || command.RunnerProfile != tools.DevelopmentRunnerProfile {
		return inspection.Requirements, nil
	}
	profile, err := developmentRunnerCommandProfile(command.Argv, command.CWD, command.Stdin, command.Environment, command.TimeoutSeconds)
	if err != nil || profile != "go-test-all" {
		return inspection.Requirements, nil
	}
	evidence := inspection.GoCommandRequirements
	if !inspection.SourceEvidenceKnown || !inspection.SourceClean || !evidence.Valid(inspection.SourceDigest, inspection.Requirements) {
		return nil, errors.New("development Go source requirements are invalid")
	}
	names := make([]string, 0, len(evidence.ExactRequirements)+3)
	for _, id := range evidence.ExactRequirements {
		names = append(names, string(id))
	}
	// Bind compatibility to the actual pinned provider version. This does
	// not fabricate capabilities for older patch versions or relax Missing.
	required, err := development.VersionRequirement("toolchain.go", tools.DevelopmentRunnerGoVersion)
	if err != nil {
		return nil, err
	}
	names = append(names, string(required.ID))
	for _, minimum := range evidence.MinimumVersions {
		if version.Compare("go"+minimum.Version, "go"+tools.DevelopmentRunnerGoVersion) > 0 {
			required, err := development.VersionRequirement("toolchain.go", minimum.Version)
			if err != nil {
				return nil, err
			}
			names = append(names, string(required.ID))
		}
	}
	requirements, err := development.Requirements(names...)
	if err != nil {
		return nil, err
	}
	ids := make([]development.CapabilityID, 0, len(requirements))
	for _, required := range requirements {
		ids = append(ids, required.ID)
	}
	return ids, nil
}

// The public VM dispatch contains only a fixed administrator-reviewed profile.
// Unsupported private options are rejected, never dropped or approximated.
func developmentRunnerCommandProfile(argv []string, cwd, stdin string, environment map[string]string, timeout int) (string, error) {
	if cwd != "" || stdin != "" || len(environment) != 0 || timeout != 4200 {
		return "", errors.New("isolated runner requires root cwd, empty stdin/environment and its fixed 4200-second command timeout")
	}
	if reflect.DeepEqual(argv, []string{"make", "validate-all"}) {
		return "make-validate-all", nil
	}
	if reflect.DeepEqual(argv, []string{"go", "test", "./...", "-count=1"}) {
		return "go-test-all", nil
	}
	return "", errors.New("isolated runner supports only the exact registered command profiles")
}

func developmentRunnerEffect(request workqueue.DevelopmentRequest, objective development.Objective) string {
	digest := sha256.Sum256([]byte("aeontra-project-development-runner-effect-v1\x00" + request.KeyDigest + "\x00" + objective.ObjectiveID))
	return hex.EncodeToString(digest[:])
}

func developmentRunnerPlanDigest(objective development.Objective) string {
	step := objective.Steps[0]
	attempt := step.Attempts[len(step.Attempts)-1]
	body, _ := json.Marshal(struct{ ObjectiveID, AttemptID, SourceDigest, EnvironmentDigest, CommandDigest string }{objective.ObjectiveID, attempt.AttemptID, attempt.SourceDigest, attempt.EnvironmentDigest, step.AcceptanceContract.CommandDigest})
	digest := sha256.Sum256(append([]byte("aeontra-project-development-runner-plan-v1\x00"), body...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (s *Server) developmentRunnerLease(request workqueue.DevelopmentRequest, command projectDevelopmentBody) (workqueue.Lease, error) {
	suffix := strings.TrimPrefix(request.ID, "dr_")
	pool := "development.runner." + suffix
	job, _, err := s.workQueue.Enqueue(workqueue.Spec{IdempotencyKey: request.ID + ":runner", Workspace: "development." + suffix, Pool: pool, Profile: command.RunnerProfile, PayloadHash: request.BodyDigest})
	if err != nil {
		return workqueue.Lease{}, err
	}
	if job.State == workqueue.StateQueued {
		lease, err := s.workQueue.LeaseNext(pool, s.projectTaskHolder(), projectTaskLeaseTTL)
		if err != nil {
			return lease, err
		}
		if lease.Job.ID != job.ID {
			return lease, errors.New("development runner lease identity conflict")
		}
		return lease, nil
	}
	lease := workqueue.Lease{Job: job, ID: job.LeaseID, Fence: job.Fence, Attempt: job.Attempt, ExpiresAt: job.LeaseExpiresAt}
	if job.State == workqueue.StateLeased {
		if _, err := s.workQueue.Heartbeat(job.ID, job.LeaseID, job.Fence, projectTaskLeaseTTL); err != nil {
			if err := s.workQueue.RecoverExpired(); err != nil {
				return lease, err
			}
			return s.workQueue.LeaseNext(pool, s.projectTaskHolder(), projectTaskLeaseTTL)
		}
	}
	return lease, nil
}

func (s *Server) developmentRunnerRequest(request workqueue.DevelopmentRequest, objective development.Objective, command projectDevelopmentBody, initial edge.Operation, lease workqueue.Lease) (tools.DevelopmentRunnerRequest, error) {
	profile, err := developmentRunnerCommandProfile(command.Argv, command.CWD, command.Stdin, command.Environment, command.TimeoutSeconds)
	if err != nil {
		return tools.DevelopmentRunnerRequest{}, err
	}
	inspection := initial.Result.DevelopmentInspection
	if inspection == nil || !inspection.SourceEvidenceKnown || !inspection.SourceClean || !validProjectTaskCommit(inspection.SourceHead) {
		return tools.DevelopmentRunnerRequest{}, errors.New("isolated runner requires verified clean committed source; source remains on Edge")
	}
	return tools.DevelopmentRunnerRequest{EffectID: developmentRunnerEffect(request, objective), PlanDigest: developmentRunnerPlanDigest(objective), SourceOwner: objective.Scope.Anchor.Owner, SourceRepo: objective.Scope.Anchor.Repository, SourceSHA: inspection.SourceHead, SourceDigest: inspection.SourceDigest, CommandProfile: profile, Lease: lease}, nil
}

func (s *Server) reconcileDevelopmentRunner(ctx context.Context, request workqueue.DevelopmentRequest, objective development.Objective, command projectDevelopmentBody) error {
	if s.developmentRunner == nil || s.developmentRunner.Profile() != command.RunnerProfile {
		return errors.New("administrator runner profile unavailable")
	}
	initial, err := s.edgeOperations.OperationStatus(request.InspectionOperationID)
	if err != nil {
		return err
	}
	anchor, _, err := developmentInspection(request, initial)
	if err != nil || anchor != objective.Scope.Anchor {
		return errors.New("development runner source anchor mismatch")
	}
	inspection := initial.Result.DevelopmentInspection
	if !inspection.SourceEvidenceKnown || !inspection.SourceClean || !validProjectTaskCommit(inspection.SourceHead) {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonSourceChanged)
	}
	if len(objective.Steps[0].Attempts) == 0 {
		candidate, err := s.developmentRunner.ConfiguredTemplateAttestation()
		if err != nil {
			probe, err := s.developmentRunner.EnsureCalibration(ctx)
			if err != nil {
				return err
			}
			if !probe.Pending && probe.State != "succeeded" {
				return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestFailed, workqueue.DevelopmentRequestReasonOperationFailed)
			}
			return s.pendingDevelopmentCapabilities(request)
		}
		next, _, err := objective.PlanAttempt("command", objective.ObjectiveID+":attempt:1", inspection.SourceDigest, []development.EnvironmentAttestation{candidate})
		if err != nil {
			var resolutionErr *development.ResolutionError
			if errors.As(err, &resolutionErr) && resolutionErr.Reason == development.ResolutionFailureCapabilities && len(resolutionErr.Missing) != 0 {
				return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement)
			}
			return s.pendingDevelopmentCapabilities(request)
		}
		if objective, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
			return err
		}
	}
	candidate, err := s.developmentRunner.ConfiguredTemplateAttestation()
	if err != nil {
		return err
	}
	attemptSnapshot := objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1]
	if candidate.Digest != attemptSnapshot.EnvironmentDigest {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement)
	}
	if objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1].State == development.AttemptPlanned {
		next, err := objective.StartAttempt("command")
		if err != nil {
			return err
		}
		if objective, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
			return err
		}
	}
	lease, err := s.developmentRunnerLease(request, command)
	if err != nil {
		return err
	}
	runnerRequest, err := s.developmentRunnerRequest(request, objective, command, initial, lease)
	if err != nil {
		return err
	}
	// Start itself performs content-free durable intent recovery before any POST.
	result, err := s.developmentRunner.Start(ctx, runnerRequest)
	if err != nil {
		if errors.Is(err, tools.ErrDevelopmentRunnerSourceNotPublic) {
			if _, completeErr := s.workQueue.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateFailed, Summary: "isolated source is not public exact Git objects"}); completeErr != nil {
				return completeErr
			}
			return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonSourceChanged)
		}
		return err
	}
	if result.Pending {
		return nil
	}
	if result.State == "cancelled" {
		if _, err := s.workQueue.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateCancelled, Summary: "isolated development command cancelled"}); err != nil {
			return err
		}
		cancelled, err := objective.Cancel()
		if err != nil {
			return err
		}
		if _, _, err = s.workQueue.SaveDevelopmentObjective(cancelled); err != nil {
			return err
		}
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestCancelled, workqueue.DevelopmentRequestReasonCancellationRequested)
	}
	if result.State != "succeeded" {
		class := result.Failure
		if class == "" {
			class = development.FailureReconciliationNeeded
		}
		if objective.State != development.ObjectiveAcceptancePending {
			next, _, err := objective.FailAttempt("command", class)
			if err != nil {
				return err
			}
			if _, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
				return err
			}
		}
		if job := lease.Job; job.State == workqueue.StateLeased {
			if _, err := s.workQueue.Complete(job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateFailed, Summary: "isolated development command failed"}); err != nil {
				return err
			}
		}
		if class == development.FailureCode {
			return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonCodeFailure)
		}
		if class == development.FailureCapabilityMissing || class == development.FailureDependencyMissing {
			return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement)
		}
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestFailed, workqueue.DevelopmentRequestReasonOperationFailed)
	}
	if result.RunID <= 0 || !regexpDevelopmentDigest(result.ReceiptDigest) {
		return errors.New("isolated runner verified receipt missing")
	}
	capsRaw := make([]string, len(result.Capabilities))
	for i, id := range result.Capabilities {
		capsRaw[i] = string(id)
	}
	caps, err := development.NewCapabilitySet(capsRaw...)
	if err != nil {
		return err
	}
	attempt := objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1]
	actual, err := development.NewEnvironmentAttestation(attempt.EnvironmentID, development.ClassIsolatedRunner, attempt.EnvironmentGeneration, caps)
	if err != nil || actual.Digest != attempt.EnvironmentDigest || len(caps.Missing(objective.Steps[0].Requirements)) != 0 {
		return errors.New("isolated runner fresh capability receipt mismatch")
	}
	if objective.State != development.ObjectiveAcceptancePending {
		next, err := objective.CompleteAttempt("command")
		if err != nil {
			return err
		}
		if objective, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
			return err
		}
	}
	return s.acceptDevelopmentRunner(ctx, request, objective, runnerRequest, result, lease)
}

func regexpDevelopmentDigest(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && func() bool {
		decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
		return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
	}()
}

func (s *Server) acceptDevelopmentRunner(ctx context.Context, request workqueue.DevelopmentRequest, objective development.Objective, runnerRequest tools.DevelopmentRunnerRequest, result tools.DevelopmentRunnerResult, lease workqueue.Lease) error {
	op, err := s.developmentOperation(request, edge.OperationProjectDevelopmentInspect, developmentOperationKey(request, "accept-inspect"), edge.OperationRequest{Alias: request.Alias, TargetAlias: request.Target, Profile: "linux-workcell"})
	if err != nil {
		return err
	}
	if request.ActiveOperationID != op.ID {
		request.ActiveOperationID = op.ID
		if request, err = s.saveDevelopmentRequest(request); err != nil {
			return err
		}
	}
	if !developmentOperationTerminal(op.State) {
		return nil
	}
	anchor, _, err := developmentInspection(request, op)
	if err != nil {
		return err
	}
	inspection := op.Result.DevelopmentInspection
	if anchor != objective.Scope.Anchor || inspection.SourceDigest != runnerRequest.SourceDigest || !inspection.SourceEvidenceKnown || !inspection.SourceClean || inspection.SourceHead != runnerRequest.SourceSHA {
		if lease.Job.State == workqueue.StateLeased {
			if _, err := s.workQueue.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateFailed, Summary: "isolated command source changed before acceptance"}); err != nil {
				return err
			}
		}
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonSourceChanged)
	}
	contract := objective.Steps[0].AcceptanceContract
	attempt := objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1]
	receipt, err := development.NewCommandAcceptanceReceipt(objective, "command", development.CommandOperationEvidence{EvidenceProvider: "github-run", RunnerEffectID: runnerRequest.EffectID, RunnerRunID: result.RunID, RunnerReceiptDigest: result.ReceiptDigest, CommandDigest: contract.CommandDigest, SourceDigest: inspection.SourceDigest, EnvironmentDigest: attempt.EnvironmentDigest, PrivateBodyRef: request.BodyRef, PrivateBodyDigest: request.BodyDigest, ExitCode: 0})
	if err != nil {
		return err
	}
	accepted, err := objective.AcceptWithEvidence([]development.CommandAcceptanceReceipt{receipt})
	if err != nil {
		return err
	}
	if _, _, err = s.workQueue.SaveDevelopmentObjective(accepted); err != nil {
		return err
	}
	if lease.Job.State == workqueue.StateLeased {
		if _, err := s.workQueue.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateSucceeded, Summary: "isolated development command verified"}); err != nil {
			return err
		}
	}
	return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestCompleted, workqueue.DevelopmentRequestReasonNone)
}

func (s *Server) cancelDevelopmentRunner(ctx context.Context, request workqueue.DevelopmentRequest, command projectDevelopmentBody) error {
	if request.ObjectiveID != "" {
		objective, found, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
		if err != nil || !found {
			return errors.New("runner cancellation objective unavailable")
		}
		effect, found, err := s.workQueue.DevelopmentRunnerEffect(developmentRunnerEffect(request, objective))
		if err != nil {
			return err
		}
		if found {
			if s.developmentRunner == nil || (command.RunnerProfile != "" && s.developmentRunner.Profile() != command.RunnerProfile) {
				return errors.New("runner cancellation provider unavailable")
			}
			command.RunnerProfile = s.developmentRunner.Profile()
			initial, err := s.edgeOperations.OperationStatus(request.InspectionOperationID)
			if err != nil {
				return err
			}
			lease, err := s.developmentRunnerLease(request, command)
			if err != nil {
				return err
			}
			if lease.Job.ID != effect.JobID {
				return errors.New("runner cancellation job mismatch")
			}
			anchor, _, err := developmentInspection(request, initial)
			inspection := initial.Result.DevelopmentInspection
			if err != nil || anchor != objective.Scope.Anchor || inspection == nil || !inspection.SourceEvidenceKnown || !inspection.SourceClean || !validProjectTaskCommit(inspection.SourceHead) || inspection.SourceDigest != objective.Steps[0].AcceptanceContract.SourceDigest {
				return errors.New("runner cancellation captured source binding mismatch")
			}
			runnerRequest := tools.DevelopmentRunnerRequest{EffectID: effect.EffectID, PlanDigest: developmentRunnerPlanDigest(objective), SourceOwner: anchor.Owner, SourceRepo: anchor.Repository, SourceSHA: inspection.SourceHead, SourceDigest: inspection.SourceDigest, CommandProfile: effect.CommandProfile, Lease: lease}
			result, err := s.developmentRunner.Cancel(ctx, runnerRequest)
			if err != nil {
				return err
			}
			if result.Pending {
				return nil
			}
			if lease.Job.State == workqueue.StateLeased {
				if _, err := s.workQueue.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateCancelled, Summary: "isolated development command cancelled"}); err != nil {
					return err
				}
			}
		}
		if objective.State != development.ObjectiveAccepted && objective.State != development.ObjectiveCancelled && objective.State != development.ObjectiveFailed {
			cancelled, err := objective.Cancel()
			if err != nil {
				return err
			}
			if _, _, err = s.workQueue.SaveDevelopmentObjective(cancelled); err != nil {
				return err
			}
		}
	}
	return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestCancelled, workqueue.DevelopmentRequestReasonCancellationRequested)
}
