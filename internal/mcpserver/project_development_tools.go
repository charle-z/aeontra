package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/tools"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

var developmentRequestPattern = regexp.MustCompile(`^dr_[a-f0-9]{32}$`)
var developmentCallerKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,117}$`)
var developmentProcessPattern = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)

type projectDevelopmentRunner interface {
	Profile() string
	ConfiguredTemplateAttestation() (development.EnvironmentAttestation, error)
	EnsureCalibration(context.Context) (tools.DevelopmentRunnerResult, error)
	Start(context.Context, tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error)
	Reconcile(context.Context, tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error)
	Cancel(context.Context, tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error)
}

type developmentProcessLookup interface {
	LatestDevelopmentProcessOperation(string, edge.OperationKind, edge.OperationRequest) (edge.Operation, bool, error)
}

type projectDevelopmentStartParams struct {
	Alias          string            `json:"alias"`
	Target         string            `json:"target"`
	IdempotencyKey string            `json:"idempotency_key"`
	Argv           []string          `json:"argv"`
	CWD            string            `json:"cwd,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Requirements   []string          `json:"requirements,omitempty"`
	RunnerProfile  string            `json:"runner_profile,omitempty"`
}

// This body is never part of a workqueue record or public status result.
type projectDevelopmentBody struct {
	Argv           []string                   `json:"argv"`
	CWD            string                     `json:"cwd,omitempty"`
	Stdin          string                     `json:"stdin,omitempty"`
	Environment    map[string]string          `json:"environment,omitempty"`
	TimeoutSeconds int                        `json:"timeout_seconds"`
	Requirements   []development.CapabilityID `json:"requirements,omitempty"`
	RunnerProfile  string                     `json:"runner_profile,omitempty"`
}

type projectDevelopmentIDParams struct {
	RequestID string `json:"request_id"`
}

// WithDevelopmentRunner registers a private administrator-configured broker.
// Registration never moves source or changes the requested execution location.
func (s *Server) WithDevelopmentRunner(runner *tools.DevelopmentRunner) *Server {
	s.developmentRunner = runner
	return s
}

type projectDevelopmentView struct {
	RequestID       string                             `json:"request_id"`
	Alias           string                             `json:"alias"`
	Target          string                             `json:"target"`
	State           workqueue.DevelopmentRequestState  `json:"state"`
	Reason          workqueue.DevelopmentRequestReason `json:"reason,omitempty"`
	ObjectiveID     string                             `json:"objective_id,omitempty"`
	ObjectiveState  development.ObjectiveState         `json:"objective_state,omitempty"`
	ProcessID       string                             `json:"process_id,omitempty"`
	AcceptanceState string                             `json:"acceptance_state"`
	NextTool        string                             `json:"next_tool,omitempty"`
}

func (s *Server) addProjectDevelopmentTools(projectSchema map[string]any) {
	write := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}
	read := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	id := stringSchema("opaque durable command-development request", `^dr_[a-f0-9]{32}$`, 35)
	s.addDirectTool(toolDef{Name: "project_development_start", Description: "Durably stage one exact scoped argv command and return immediately. Source and measured capabilities are inspected before execution; command acceptance requires a known zero exit and exact source/environment revalidation. This does not accept natural-language goals. Private command bytes are not returned. An external runner requires an explicit administrator-registered profile and public clean committed source.", Version: "1", Annotations: write, InputSchema: closedObject(map[string]any{
		"alias": projectSchema["alias"], "target": projectSchema["target"],
		"idempotency_key": stringSchema("caller retry key for this exact immutable request", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,117}$`, 118),
		"argv":            map[string]any{"type": "array", "minItems": 1, "maxItems": 128, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}},
		"cwd":             map[string]any{"type": "string", "maxLength": 1024}, "stdin": map[string]any{"type": "string", "maxLength": edge.MaxProjectExecStdinBytes},
		"environment":     map[string]any{"type": "object", "maxProperties": 32, "propertyNames": map[string]any{"pattern": `^[A-Za-z_][A-Za-z0-9_]{0,63}$`}, "additionalProperties": map[string]any{"type": "string", "maxLength": 4096}},
		"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 86400},
		"requirements":    map[string]any{"type": "array", "maxItems": development.MaxEnvironmentCatalogEntries, "uniqueItems": true, "items": stringSchema("additional required capability, not a grant of authority", `^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`, 128)},
		"runner_profile":  stringSchema("explicit administrator-registered isolated runner profile", `^[a-z0-9][a-z0-9-]{0,63}$`, 64),
	}, []string{"alias", "target", "idempotency_key", "argv", "timeout_seconds"})}, s.handleProjectDevelopmentStart)
	s.addDirectTool(toolDef{Name: "project_development_status", Description: "Read durable command-development and objective metadata only. This never dispatches, polls the Edge, exposes private command bodies, or infers natural-language goal success.", Version: "1", Annotations: read, InputSchema: closedObject(map[string]any{"request_id": id}, []string{"request_id"})}, s.handleProjectDevelopmentStatus)
	s.addDirectTool(toolDef{Name: "project_development_cancel", Description: "Durably request cancellation of this exact development command. The coordinator reconciles lost acknowledgments and stops only its captured process identity. Cancellation is not accepted command evidence.", Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}, InputSchema: closedObject(map[string]any{"request_id": id}, []string{"request_id"})}, s.handleProjectDevelopmentCancel)
}

func (s *Server) handleProjectDevelopmentStart(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	var params projectDevelopmentStartParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	params.Alias = strings.ToLower(strings.TrimSpace(params.Alias))
	params.Target = strings.ToLower(strings.TrimSpace(params.Target))
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if !developmentCallerKeyPattern.MatchString(params.IdempotencyKey) || len(params.Requirements) > development.MaxEnvironmentCatalogEntries {
		return "", modelturn.ErrInvalidRequest
	}
	command, err := edge.NormalizeDevelopmentCommand(edge.OperationRequest{Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: params.IdempotencyKey, Argv: params.Argv, CWD: params.CWD, Stdin: params.Stdin, Environment: params.Environment, TimeoutSeconds: params.TimeoutSeconds})
	if err != nil {
		return "", err
	}
	caps, err := development.NewCapabilitySet(params.Requirements...)
	if err != nil {
		return "", err
	}
	if params.RunnerProfile != "" && (s.developmentRunner == nil || params.RunnerProfile != s.developmentRunner.Profile()) {
		return "", errors.New("development runner profile is not administrator-registered")
	}
	if params.RunnerProfile != "" {
		if _, err := developmentRunnerCommandProfile(command.Argv, command.CWD, command.Stdin, command.Environment, command.TimeoutSeconds); err != nil {
			return "", err
		}
	}
	body, err := json.Marshal(projectDevelopmentBody{Argv: command.Argv, CWD: command.CWD, Stdin: command.Stdin, Environment: command.Environment, TimeoutSeconds: command.TimeoutSeconds, Requirements: caps.IDs(), RunnerProfile: params.RunnerProfile})
	if err != nil || len(body) > int(modelturn.MaxGoalBodyBytes) {
		return "", modelturn.ErrBodyTooLarge
	}
	bodyHash := sha256.Sum256(body)
	bodyDigest := "sha256:" + hex.EncodeToString(bodyHash[:])
	keyHash := sha256.Sum256([]byte("aeontra-project-development-key-v1\x00" + params.IdempotencyKey))
	keyDigest := "sha256:" + hex.EncodeToString(keyHash[:])
	lock := s.projectTaskStartLock(keyDigest)
	lock.Lock()
	defer lock.Unlock()
	if request, found, err := s.workQueue.DevelopmentRequestByKey(keyDigest); err != nil {
		return "", err
	} else if found {
		if request.Alias != params.Alias || request.Target != params.Target || request.BodyDigest != bodyDigest {
			return "", errors.New("development request idempotency key conflicts")
		}
		return s.projectDevelopmentResult(request)
	}
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target resolution unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil || !s.edgeDevices.DeviceActive(device.ID) {
		return "", errors.New("active edge target not found")
	}
	ref, err := s.modelTurns.StageRuntimeGoal(context.Background(), body, modelturn.MaxTurnTTL)
	if err != nil {
		return "", err
	}
	refs := []modelturn.TaskGoalReference{{BodyRef: ref.BodyRef, ContentDigest: ref.ContentDigest}}
	if err := s.modelTurns.PinTaskGoalReferences(context.Background(), keyDigest, refs); err != nil {
		_ = s.modelTurns.DiscardRuntimeGoal(context.Background(), ref.BodyRef, ref.ContentDigest)
		return "", err
	}
	identityHash := sha256.Sum256([]byte("aeontra-project-development-identity-v1\x00" + keyDigest + "\x00" + ref.BodyRef))
	request := workqueue.DevelopmentRequest{ID: "dr_" + hex.EncodeToString(identityHash[:16]), Revision: 1, KeyDigest: keyDigest, Alias: params.Alias, Target: params.Target, DeviceID: device.ID, BodyRef: ref.BodyRef, BodyDigest: ref.ContentDigest, State: workqueue.DevelopmentRequestPreparing}
	request, _, err = s.workQueue.SaveDevelopmentRequest(request)
	if err != nil { // Preserve a possibly committed binding; an orphan pin is recovered by the existing grace period.
		if saved, found, readErr := s.workQueue.DevelopmentRequestByKey(keyDigest); readErr == nil && found && saved.BodyRef == ref.BodyRef && saved.BodyDigest == ref.ContentDigest {
			return s.projectDevelopmentResult(saved)
		}
		return "", err
	}
	return s.projectDevelopmentResult(request)
}

func (s *Server) handleProjectDevelopmentStatus(arguments json.RawMessage) (string, error) {
	request, err := s.projectDevelopmentRequest(arguments)
	if err != nil {
		return "", err
	}
	return s.projectDevelopmentResult(request)
}

func (s *Server) handleProjectDevelopmentCancel(arguments json.RawMessage) (string, error) {
	request, err := s.projectDevelopmentRequest(arguments)
	if err != nil {
		return "", err
	}
	lock := s.projectTaskStartLock(request.KeyDigest)
	lock.Lock()
	defer lock.Unlock()
	request, _, err = s.workQueue.DevelopmentRequest(request.ID)
	if err != nil {
		return "", err
	}
	if !projectDevelopmentTerminal(request.State) && request.State != workqueue.DevelopmentRequestCancelling {
		request.Revision++
		request.State = workqueue.DevelopmentRequestCancelling
		request.Reason = workqueue.DevelopmentRequestReasonCancellationRequested
		request, _, err = s.workQueue.SaveDevelopmentRequest(request)
		if err != nil {
			return "", err
		}
	}
	return s.projectDevelopmentResult(request)
}

func (s *Server) projectDevelopmentRequest(arguments json.RawMessage) (workqueue.DevelopmentRequest, error) {
	if s.workQueue == nil {
		return workqueue.DevelopmentRequest{}, errWorkQueueUnavailable
	}
	var params projectDevelopmentIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return workqueue.DevelopmentRequest{}, err
	}
	if !developmentRequestPattern.MatchString(params.RequestID) {
		return workqueue.DevelopmentRequest{}, modelturn.ErrInvalidRequest
	}
	request, found, err := s.workQueue.DevelopmentRequest(params.RequestID)
	if err != nil {
		return request, err
	}
	if !found {
		return request, errors.New("development request not found")
	}
	return request, nil
}

func (s *Server) projectDevelopmentResult(request workqueue.DevelopmentRequest) (string, error) {
	view := projectDevelopmentView{RequestID: request.ID, Alias: request.Alias, Target: request.Target, State: request.State, Reason: request.Reason, ObjectiveID: request.ObjectiveID, ProcessID: request.ProcessID, AcceptanceState: "not_ready"}
	if request.ObjectiveID != "" {
		objective, found, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
		if err != nil {
			return "", err
		}
		if !found {
			return "", errors.New("development objective unavailable")
		}
		view.ObjectiveState = objective.State
		if objective.State == development.ObjectiveAccepted {
			view.AcceptanceState = "command_verified"
		} else if objective.State == development.ObjectiveAcceptancePending {
			view.AcceptanceState = "pending"
		}
	}
	if !projectDevelopmentTerminal(request.State) {
		view.NextTool = "project_development_status"
	}
	return marshalToolValue(view, nil)
}

func projectDevelopmentTerminal(state workqueue.DevelopmentRequestState) bool {
	return state == workqueue.DevelopmentRequestCompleted || state == workqueue.DevelopmentRequestFailed || state == workqueue.DevelopmentRequestCancelled
}

func (s *Server) developmentBody(ctx context.Context, request workqueue.DevelopmentRequest) (projectDevelopmentBody, error) {
	if s.modelTurns == nil {
		return projectDevelopmentBody{}, errModelTurnStoreUnavailable
	}
	body, err := s.modelTurns.PinnedDevelopmentBody(ctx, request.KeyDigest, modelturn.TaskGoalReference{BodyRef: request.BodyRef, ContentDigest: request.BodyDigest})
	if err != nil {
		return projectDevelopmentBody{}, err
	}
	var command projectDevelopmentBody
	if err := decodeClosed(body, &command); err != nil {
		return command, errors.New("development private body invalid")
	}
	return command, nil
}

// One existing coordinator tick performs at most four independent quick rounds.
// No WaitOperation, foreground workload, or additional resident scheduler is used.
func (s *Server) reconcileDevelopmentRequestsOnce(ctx context.Context) error {
	if s.workQueue == nil || s.modelTurns == nil || s.edgeOperations == nil || s.edgeDevices == nil {
		return errWorkQueueUnavailable
	}
	requests, err := s.workQueue.DevelopmentRequests(4)
	if err != nil {
		return err
	}
	type result struct{ err error }
	results := make(chan result, len(requests))
	for _, request := range requests {
		go func(request workqueue.DevelopmentRequest) {
			lock := s.projectTaskStartLock(request.KeyDigest)
			lock.Lock()
			defer lock.Unlock()
			current, err := s.workQueue.TouchDevelopmentRequest(request.ID, request.Revision)
			if err == nil {
				phaseCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				err = s.reconcileDevelopmentRequest(phaseCtx, current)
				cancel()
			}
			results <- result{err}
		}(request)
	}
	var joined error
	for range requests {
		joined = errors.Join(joined, (<-results).err)
	}
	return joined
}

func (s *Server) saveDevelopmentRequest(request workqueue.DevelopmentRequest) (workqueue.DevelopmentRequest, error) {
	request.Revision++
	stored, _, err := s.workQueue.SaveDevelopmentRequest(request)
	return stored, err
}

func developmentOperationKey(request workqueue.DevelopmentRequest, phase string) string {
	return request.ID + ":" + phase
}

func (s *Server) developmentOperation(request workqueue.DevelopmentRequest, kind edge.OperationKind, key string, body edge.OperationRequest) (edge.Operation, error) {
	lookup, ok := s.edgeOperations.(edgeOperationIdempotencyLookup)
	if !ok {
		return edge.Operation{}, errors.New("development operation recovery unavailable")
	}
	op, found, err := lookup.OperationByIdempotency(request.DeviceID, kind, key)
	if err != nil {
		return op, err
	}
	if !found {
		body.IdempotencyKey = key
		op, _, err = s.edgeOperations.CreateOperation(request.DeviceID, kind, body)
		if err != nil {
			return op, err
		}
	}
	if op.DeviceID != request.DeviceID || op.Kind != kind || op.Request.IdempotencyKey != key || op.Request.Alias != request.Alias || op.Request.TargetAlias != request.Target {
		return edge.Operation{}, errors.New("development operation binding mismatch")
	}
	return op, nil
}

func developmentInspection(request workqueue.DevelopmentRequest, op edge.Operation) (development.WorkspaceAnchor, []development.EnvironmentAttestation, error) {
	if op.DeviceID != request.DeviceID || op.Kind != edge.OperationProjectDevelopmentInspect || op.State != edge.OperationSucceeded || op.Result.ProjectAlias != request.Alias || op.Result.ProjectTarget != request.Target || op.Result.DevelopmentInspection == nil {
		return development.WorkspaceAnchor{}, nil, errors.New("development inspection binding mismatch")
	}
	inspection := op.Result.DevelopmentInspection
	anchor := development.WorkspaceAnchor{DeviceID: op.DeviceID, WorkspaceID: op.Result.WorkspaceID, Generation: inspection.ProjectGeneration, Owner: op.Result.ProjectOwner, Repository: op.Result.ProjectRepository}
	if !anchor.Valid() {
		return anchor, nil, errors.New("development workspace anchor invalid")
	}
	environments := make([]development.EnvironmentAttestation, 0, len(inspection.Environments))
	for _, record := range inspection.Environments {
		attestation, err := record.Attestation()
		if err != nil {
			return anchor, nil, err
		}
		// Only the actual workcell command path is executable here. Rootless/toolbox
		// records are not a license to execute through the unrelated host path.
		if attestation.Class == development.ClassWorkcell && attestation.EnvironmentID == "workcell:"+anchor.WorkspaceID && attestation.Generation == anchor.Generation {
			environments = append(environments, attestation)
		}
	}
	return anchor, environments, nil
}

func (s *Server) reconcileDevelopmentRequest(ctx context.Context, request workqueue.DevelopmentRequest) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if request.State == workqueue.DevelopmentRequestCancelling {
		return s.cancelDevelopmentRequest(ctx, request)
	}
	command, err := s.developmentBody(ctx, request)
	if err != nil {
		request.Reason = workqueue.DevelopmentRequestReasonReconciliationRequired
		if _, saveErr := s.saveDevelopmentRequest(request); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	if request.ObjectiveID == "" {
		op, err := s.developmentOperation(request, edge.OperationProjectDevelopmentInspect, developmentOperationKey(request, "inspect"), edge.OperationRequest{Alias: request.Alias, TargetAlias: request.Target, Profile: "linux-workcell"})
		if err != nil {
			return err
		}
		if request.InspectionOperationID == "" {
			request.InspectionOperationID = op.ID
			request.ActiveOperationID = op.ID
			request.Reason = workqueue.DevelopmentRequestReasonInspectionPending
			if request, err = s.saveDevelopmentRequest(request); err != nil {
				return err
			}
		}
		if !developmentOperationTerminal(op.State) {
			return nil
		}
		if op.State != edge.OperationSucceeded {
			return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestFailed, workqueue.DevelopmentRequestReasonOperationFailed)
		}
		anchor, environments, err := developmentInspection(request, op)
		if err != nil {
			return err
		}
		raw := make([]string, 0, len(command.Requirements)+len(op.Result.DevelopmentInspection.Requirements))
		for _, id := range op.Result.DevelopmentInspection.Requirements {
			raw = append(raw, string(id))
		}
		for _, id := range command.Requirements {
			raw = append(raw, string(id))
		}
		requirements, err := development.Requirements(raw...)
		if err != nil {
			return err
		}
		contract, err := development.NewCommandAcceptanceContract(command.Argv, op.Result.DevelopmentInspection.SourceDigest, request.BodyRef, request.BodyDigest, requirements, nil)
		if err != nil {
			return err
		}
		maxTier, class := development.TierWorkcell, development.ClassWorkcell
		if command.RunnerProfile != "" {
			maxTier, class = development.TierIsolatedRunner, development.ClassIsolatedRunner
		}
		policy, err := development.NewResolutionPolicy(maxTier, class)
		if err != nil {
			return err
		}
		objectiveID := "development-" + strings.TrimPrefix(request.ID, "dr_")
		objective, found, err := s.workQueue.DevelopmentObjective(objectiveID)
		if err != nil {
			return err
		}
		if !found {
			objective, err = development.NewScopedObjective(objectiveID, development.ObjectiveScope{Project: request.Alias, Target: request.Target, Anchor: anchor}, policy, []development.StepSpec{{StepID: "command", Requirements: requirements, AcceptanceContract: &contract}})
			if err != nil {
				return err
			}
			if objective, _, err = s.workQueue.SaveDevelopmentObjective(objective); err != nil {
				return err
			}
		}
		if objective.Scope.Anchor != anchor || objective.Steps[0].AcceptanceContract == nil || !reflect.DeepEqual(*objective.Steps[0].AcceptanceContract, contract) {
			return errors.New("development objective identity conflict")
		}
		request.ObjectiveID = objective.ObjectiveID
		request.State = workqueue.DevelopmentRequestActive
		request.Reason = workqueue.DevelopmentRequestReasonNone
		request.ActiveOperationID = ""
		if request, err = s.saveDevelopmentRequest(request); err != nil {
			return err
		}
		_ = environments // Resolution happens from the immutable initial inspection below.
		return nil
	}
	objective, found, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
	if err != nil || !found {
		return errors.New("development objective unavailable")
	}
	if objective.State == development.ObjectiveAccepted {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestCompleted, workqueue.DevelopmentRequestReasonNone)
	}
	if objective.State == development.ObjectiveAcceptancePending {
		if command.RunnerProfile != "" {
			return s.reconcileDevelopmentRunner(ctx, request, objective, command)
		}
		return s.acceptDevelopmentCommand(ctx, request, objective)
	}
	if request.ProcessID != "" {
		return s.pollDevelopmentProcess(request, objective)
	}
	initial, err := s.edgeOperations.OperationStatus(request.InspectionOperationID)
	if err != nil {
		return err
	}
	anchor, environments, err := developmentInspection(request, initial)
	if err != nil || anchor != objective.Scope.Anchor {
		return errors.New("development pinned inspection mismatch")
	}
	if command.RunnerProfile != "" {
		return s.reconcileDevelopmentRunner(ctx, request, objective, command)
	}
	if len(objective.Steps[0].Attempts) == 0 {
		next, _, planErr := objective.PlanAttempt("command", objective.ObjectiveID+":attempt:1", initial.Result.DevelopmentInspection.SourceDigest, environments)
		if planErr != nil {
			var ready bool
			objective, environments, ready, err = s.reconcileDevelopmentBootstrap(ctx, request, objective)
			if err != nil {
				return err
			}
			if !ready {
				current, _, readErr := s.workQueue.DevelopmentRequest(request.ID)
				if readErr != nil {
					return readErr
				}
				if current.State == workqueue.DevelopmentRequestAwaitingReasoning {
					return nil
				}
				return s.pendingDevelopmentCapabilities(current)
			}
			next, _, planErr = objective.PlanAttempt("command", objective.ObjectiveID+":attempt:1", initial.Result.DevelopmentInspection.SourceDigest, environments)
			if planErr != nil {
				return planErr
			}
		}
		if objective, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
			return err
		}
	}
	attempt := objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1]
	if attempt.State == development.AttemptPlanned {
		next, err := objective.StartAttempt("command")
		if err != nil {
			return err
		}
		if objective, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
			return err
		}
	}
	if objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1].State != development.AttemptRunning {
		return errors.New("development attempt requires reasoning")
	}
	binding := edge.ProjectDevelopmentCommandBinding{Version: 1, Anchor: anchor, SourceDigest: attempt.SourceDigest, EnvironmentDigest: attempt.EnvironmentDigest, CommandDigest: objective.Steps[0].AcceptanceContract.CommandDigest, PrivateBodyRef: request.BodyRef, PrivateBodyDigest: request.BodyDigest, TimeoutSeconds: command.TimeoutSeconds}
	for _, requirement := range objective.Steps[0].Requirements {
		binding.Requirements = append(binding.Requirements, requirement.ID)
	}
	op, err := s.developmentCommandOperation(request, edge.OperationRequest{Alias: request.Alias, TargetAlias: request.Target, Profile: "linux-workcell", Argv: command.Argv, CWD: command.CWD, Stdin: command.Stdin, Environment: command.Environment, DevelopmentCommand: &binding})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(op.Request.DevelopmentCommand, &binding) || !reflect.DeepEqual(op.Request.Argv, command.Argv) || op.Request.CWD != command.CWD || op.Request.Stdin != command.Stdin || !reflect.DeepEqual(op.Request.Environment, command.Environment) {
		return errors.New("development command request mismatch")
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
	if op.State != edge.OperationSucceeded {
		request.Reason = workqueue.DevelopmentRequestReasonReconciliationRequired
		_, err = s.saveDevelopmentRequest(request)
		return err
	}
	if !validDevelopmentCapturedStart(request, objective, op) {
		return errors.New("development command result mismatch")
	}
	request.ProcessID = op.Result.BackgroundProcessID
	request.ActiveOperationID = ""
	request.Reason = workqueue.DevelopmentRequestReasonNone
	_, err = s.saveDevelopmentRequest(request)
	return err
}

// Recovery requests can only look up the process captured by the authenticated
// original operation. They cannot start one when its journal marker is absent.
func (s *Server) developmentCommandOperation(request workqueue.DevelopmentRequest, body edge.OperationRequest) (edge.Operation, error) {
	key := developmentOperationKey(request, "command")
	op, err := s.developmentOperation(request, edge.OperationProjectDevelopmentCommandStart, key, body)
	if err != nil {
		return op, err
	}
	if !reflect.DeepEqual(op.Request.DevelopmentCommand, body.DevelopmentCommand) || !reflect.DeepEqual(op.Request.Argv, body.Argv) || op.Request.CWD != body.CWD || op.Request.Stdin != body.Stdin || !reflect.DeepEqual(op.Request.Environment, body.Environment) {
		return edge.Operation{}, errors.New("development original command binding mismatch")
	}
	if op.State != edge.OperationFailed {
		return op, nil
	}
	body.DevelopmentRecoveryOperationID = op.ID
	body.DevelopmentRecoveryIdempotencyKey = key
	recovered, err := s.developmentOperation(request, edge.OperationProjectDevelopmentCommandStart, developmentOperationKey(request, "command-recover"), body)
	if err != nil {
		return recovered, err
	}
	if recovered.Request.DevelopmentRecoveryOperationID != op.ID || recovered.Request.DevelopmentRecoveryIdempotencyKey != key {
		return edge.Operation{}, errors.New("development recovery identity mismatch")
	}
	return recovered, nil
}

func validDevelopmentCapturedStart(request workqueue.DevelopmentRequest, objective development.Objective, op edge.Operation) bool {
	if !objective.Valid() || len(objective.Steps) != 1 || len(objective.Steps[0].Attempts) == 0 || op.State != edge.OperationSucceeded || op.DeviceID != request.DeviceID || op.Kind != edge.OperationProjectDevelopmentCommandStart || op.Request.Alias != request.Alias || op.Request.TargetAlias != request.Target {
		return false
	}
	step := objective.Steps[0]
	attempt := step.Attempts[len(step.Attempts)-1]
	binding := op.Result.DevelopmentCommand
	return step.AcceptanceContract != nil && binding != nil && binding.Valid() && reflect.DeepEqual(binding, op.Request.DevelopmentCommand) && binding.Anchor == objective.Scope.Anchor && binding.SourceDigest == attempt.SourceDigest && binding.EnvironmentDigest == attempt.EnvironmentDigest && binding.CommandDigest == step.AcceptanceContract.CommandDigest && binding.PrivateBodyRef == request.BodyRef && binding.PrivateBodyDigest == request.BodyDigest && op.Result.WorkspaceID == binding.Anchor.WorkspaceID && op.Result.ProjectOwner == binding.Anchor.Owner && op.Result.ProjectRepository == binding.Anchor.Repository && developmentProcessPattern.MatchString(op.Result.BackgroundProcessID)
}

func (s *Server) pendingDevelopmentCapabilities(request workqueue.DevelopmentRequest) error {
	if request.Reason == workqueue.DevelopmentRequestReasonCapabilityMissing {
		return nil
	}
	request.Reason = workqueue.DevelopmentRequestReasonCapabilityMissing
	_, err := s.saveDevelopmentRequest(request)
	return err
}

func developmentOperationTerminal(state edge.OperationState) bool {
	return state == edge.OperationSucceeded || state == edge.OperationFailed || state == edge.OperationCancelled
}

func (s *Server) finishDevelopmentRequest(request workqueue.DevelopmentRequest, state workqueue.DevelopmentRequestState, reason workqueue.DevelopmentRequestReason) error {
	request.State = state
	request.Reason = reason
	_, err := s.saveDevelopmentRequest(request)
	return err
}

func (s *Server) failDevelopmentAttempt(request workqueue.DevelopmentRequest, objective development.Objective, class development.FailureClass, reason workqueue.DevelopmentRequestReason) error {
	next, _, err := objective.FailAttempt("command", class)
	if err != nil {
		return err
	}
	if _, _, err = s.workQueue.SaveDevelopmentObjective(next); err != nil {
		return err
	}
	if reason == workqueue.DevelopmentRequestReasonCodeFailure || reason == workqueue.DevelopmentRequestReasonSourceChanged {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, reason)
	}
	return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestFailed, reason)
}

func (s *Server) processOperation(request workqueue.DevelopmentRequest, kind edge.OperationKind) (edge.Operation, error) {
	if request.ActiveOperationID != "" {
		op, err := s.edgeOperations.OperationStatus(request.ActiveOperationID)
		if err != nil {
			return op, err
		}
		if op.Kind != kind || op.DeviceID != request.DeviceID || op.Request.BackgroundProcessID != request.ProcessID {
			return op, errors.New("development process operation mismatch")
		}
		return op, nil
	}
	base := edge.OperationRequest{Alias: request.Alias, TargetAlias: request.Target, Profile: "linux-workcell", BackgroundProcessID: request.ProcessID}
	if kind == edge.OperationProjectProcessStatus {
		base.OutputLimit = 1
	} else {
		base.GraceSeconds = 5
	}
	op, _, err := s.edgeOperations.CreateOperation(request.DeviceID, kind, base)
	if err != nil {
		lookup, ok := s.edgeOperations.(developmentProcessLookup)
		if !ok {
			return op, err
		}
		recovered, found, lookupErr := lookup.LatestDevelopmentProcessOperation(request.DeviceID, kind, base)
		if lookupErr != nil {
			return recovered, lookupErr
		}
		if !found {
			return op, err
		}
		op = recovered
	}
	request.ActiveOperationID = op.ID
	_, err = s.saveDevelopmentRequest(request)
	return op, err
}

func developmentProcessResult(request workqueue.DevelopmentRequest, objective development.Objective, op edge.Operation) bool {
	a := objective.Scope.Anchor
	r := op.Result
	return op.DeviceID == request.DeviceID && op.Request.Alias == request.Alias && op.Request.TargetAlias == request.Target && op.Request.BackgroundProcessID == request.ProcessID && op.State == edge.OperationSucceeded && r.BackgroundProcessID == request.ProcessID && r.WorkspaceID == a.WorkspaceID && r.ProjectOwner == a.Owner && r.ProjectRepository == a.Repository
}

func (s *Server) pollDevelopmentProcess(request workqueue.DevelopmentRequest, objective development.Objective) error {
	op, err := s.processOperation(request, edge.OperationProjectProcessStatus)
	if err != nil {
		return err
	}
	// processOperation persisted the operation binding; use its latest revision.
	request, _, err = s.workQueue.DevelopmentRequest(request.ID)
	if err != nil {
		return err
	}
	if !developmentOperationTerminal(op.State) {
		return nil
	}
	if !developmentProcessResult(request, objective, op) {
		return errors.New("development process evidence mismatch")
	}
	r := op.Result
	if r.BackgroundProcessState == "exited" && r.BackgroundExitKnown && r.BackgroundExitCode == 0 {
		next, err := objective.CompleteAttempt("command")
		if err != nil {
			return err
		}
		_, _, err = s.workQueue.SaveDevelopmentObjective(next)
		if err != nil {
			return err
		}
		request.ActiveOperationID = ""
		_, err = s.saveDevelopmentRequest(request)
		return err
	}
	if r.BackgroundProcessState == "exited" || r.BackgroundProcessState == "failed" || r.BackgroundProcessState == "stopped" {
		return s.failDevelopmentAttempt(request, objective, development.FailureCode, workqueue.DevelopmentRequestReasonCodeFailure)
	}
	request.ActiveOperationID = ""
	_, err = s.saveDevelopmentRequest(request)
	return err
}

func (s *Server) acceptDevelopmentCommand(ctx context.Context, request workqueue.DevelopmentRequest, objective development.Objective) error {
	// Recover a status observation whose journal ACK was lost before acceptance.
	if request.ActiveOperationID != "" {
		old, err := s.edgeOperations.OperationStatus(request.ActiveOperationID)
		if err != nil {
			return err
		}
		if old.Kind == edge.OperationProjectProcessStatus {
			request.ActiveOperationID = ""
			if request, err = s.saveDevelopmentRequest(request); err != nil {
				return err
			}
		}
	}
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
	anchor, environments, err := developmentInspection(request, op)
	if err != nil {
		return err
	}
	attempt := objective.Steps[0].Attempts[len(objective.Steps[0].Attempts)-1]
	contract := objective.Steps[0].AcceptanceContract
	if anchor != objective.Scope.Anchor || op.Result.DevelopmentInspection.SourceDigest != attempt.SourceDigest {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonSourceChanged)
	}
	var matching bool
	for _, environment := range environments {
		matching = matching || environment.Digest == attempt.EnvironmentDigest && len(environment.Capabilities.Missing(objective.Steps[0].Requirements)) == 0
	}
	if !matching {
		return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestAwaitingReasoning, workqueue.DevelopmentRequestReasonNewRequirement)
	}
	lookup, ok := s.edgeOperations.(edgeOperationIdempotencyLookup)
	if !ok {
		return errors.New("development evidence recovery unavailable")
	}
	start, found, err := lookup.OperationByIdempotency(request.DeviceID, edge.OperationProjectDevelopmentCommandStart, developmentOperationKey(request, "command"))
	if err != nil || !found {
		return errors.New("development execution evidence unavailable")
	}
	if start.State == edge.OperationFailed {
		command, err := s.developmentBody(ctx, request)
		if err != nil {
			return err
		}
		body := start.Request
		body.DevelopmentRecoveryOperationID = ""
		body.DevelopmentRecoveryIdempotencyKey = ""
		if !reflect.DeepEqual(body.Argv, command.Argv) || body.CWD != command.CWD || body.Stdin != command.Stdin || !reflect.DeepEqual(body.Environment, command.Environment) {
			return errors.New("development recovered execution body mismatch")
		}
		start, err = s.developmentCommandOperation(request, body)
		if err != nil {
			return err
		}
	}
	binding := start.Result.DevelopmentCommand
	if !validDevelopmentCapturedStart(request, objective, start) || start.Result.BackgroundProcessID != request.ProcessID || binding.Anchor != anchor || binding.SourceDigest != attempt.SourceDigest || binding.EnvironmentDigest != attempt.EnvironmentDigest || binding.CommandDigest != contract.CommandDigest || binding.PrivateBodyRef != request.BodyRef || binding.PrivateBodyDigest != request.BodyDigest {
		return errors.New("development execution evidence mismatch")
	}
	receipt, err := development.NewCommandAcceptanceReceipt(objective, "command", development.CommandOperationEvidence{OperationID: start.ID, CommandDigest: binding.CommandDigest, SourceDigest: op.Result.DevelopmentInspection.SourceDigest, EnvironmentDigest: attempt.EnvironmentDigest, PrivateBodyRef: binding.PrivateBodyRef, PrivateBodyDigest: binding.PrivateBodyDigest, ExitCode: 0})
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
	return s.finishDevelopmentRequest(request, workqueue.DevelopmentRequestCompleted, workqueue.DevelopmentRequestReasonNone)
}

func (s *Server) cancelDevelopmentRequest(ctx context.Context, request workqueue.DevelopmentRequest) error {
	command, err := s.developmentBody(ctx, request)
	bodyAvailable := err == nil
	if err != nil && request.ProcessID == "" {
		objective, found, readErr := s.workQueue.DevelopmentObjective(request.ObjectiveID)
		if readErr == nil && found && len(objective.Steps) == 1 && len(objective.Steps[0].Attempts) > 0 {
			attempts := objective.Steps[0].Attempts
			if attempts[len(attempts)-1].Class == development.ClassIsolatedRunner {
				if _, captured, effectErr := s.workQueue.DevelopmentRunnerEffect(developmentRunnerEffect(request, objective)); effectErr == nil && captured {
					return s.cancelDevelopmentRunner(ctx, request, projectDevelopmentBody{})
				}
			}
		}
		return err
	}
	if command.RunnerProfile != "" {
		return s.cancelDevelopmentRunner(ctx, request, command)
	}
	lookup, ok := s.edgeOperations.(edgeOperationIdempotencyLookup)
	if !ok {
		return errors.New("development cancellation recovery unavailable")
	}
	start, found, err := lookup.OperationByIdempotency(request.DeviceID, edge.OperationProjectDevelopmentCommandStart, developmentOperationKey(request, "command"))
	if err != nil {
		return err
	}
	if found {
		if start.DeviceID != request.DeviceID || start.Kind != edge.OperationProjectDevelopmentCommandStart || start.Request.IdempotencyKey != developmentOperationKey(request, "command") || start.Request.Alias != request.Alias || start.Request.TargetAlias != request.Target {
			return errors.New("development cancellation binding mismatch")
		}
		if start.State == edge.OperationFailed && (bodyAvailable || request.ProcessID == "") {
			body := start.Request
			if !reflect.DeepEqual(body.Argv, command.Argv) || body.CWD != command.CWD || body.Stdin != command.Stdin || !reflect.DeepEqual(body.Environment, command.Environment) {
				return errors.New("development cancellation body mismatch")
			}
			start, err = s.developmentCommandOperation(request, body)
			if err != nil {
				return err
			}
			if start.State == edge.OperationFailed {
				return nil
			}
		}
		if !developmentOperationTerminal(start.State) {
			_, err = s.edgeOperations.RequestOperationCancel(start.ID)
			return err
		}
		if start.State == edge.OperationSucceeded && request.ProcessID == "" {
			objective, present, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
			if err != nil || !present || !validDevelopmentCapturedStart(request, objective, start) {
				return errors.New("development cancellation captured identity mismatch")
			}
			request.ProcessID = start.Result.BackgroundProcessID
			request.ActiveOperationID = ""
			if request, err = s.saveDevelopmentRequest(request); err != nil {
				return err
			}
		}
	}
	if request.ProcessID != "" {
		objective, present, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
		if err != nil || !present {
			return errors.New("development cancellation objective unavailable")
		}
		if request.ActiveOperationID != "" {
			old, err := s.edgeOperations.OperationStatus(request.ActiveOperationID)
			if err != nil {
				return err
			}
			if old.Kind != edge.OperationProjectProcessStop {
				request.ActiveOperationID = ""
				if request, err = s.saveDevelopmentRequest(request); err != nil {
					return err
				}
			}
		}
		op, err := s.processOperation(request, edge.OperationProjectProcessStop)
		if err != nil {
			return err
		}
		request, _, err = s.workQueue.DevelopmentRequest(request.ID)
		if err != nil {
			return err
		}
		if !developmentOperationTerminal(op.State) {
			return nil
		}
		if !developmentProcessResult(request, objective, op) {
			return errors.New("development cancellation process evidence mismatch")
		}
		if op.Result.BackgroundProcessState != "stopped" && op.Result.BackgroundProcessState != "exited" && op.Result.BackgroundProcessState != "failed" {
			request.ActiveOperationID = ""
			_, err = s.saveDevelopmentRequest(request)
			return err
		}
	}
	if request.ObjectiveID != "" {
		objective, found, err := s.workQueue.DevelopmentObjective(request.ObjectiveID)
		if err != nil || !found {
			return errors.New("development cancellation objective unavailable")
		}
		if len(objective.Steps[0].Provisioning) > 0 {
			stopped, err := s.cancelDevelopmentBootstrap(ctx, request, objective)
			if err != nil {
				return err
			}
			if !stopped {
				return nil
			}
			objective, _, err = s.workQueue.DevelopmentObjective(request.ObjectiveID)
			if err != nil {
				return err
			}
		}
		if objective.State != development.ObjectiveCancelled && objective.State != development.ObjectiveAccepted && objective.State != development.ObjectiveFailed {
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
