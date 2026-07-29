package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const maxOpenCodeIdempotencyBytes = 128

var errEdgeStoreUnavailable = errors.New("edge device store is not configured")

type edgeDeviceRegistry interface {
	DeviceActive(string) bool
}

type edgeOperationRegistry interface {
	CreateOperation(string, edge.OperationKind, edge.OperationRequest) (edge.Operation, bool, error)
	OperationStatus(string) (edge.Operation, error)
	AutopilotStatus(string) (edge.OperationResult, error)
	WaitOperation(context.Context, string, time.Duration) (edge.Operation, error)
}

type openCodeRuntimeStartParams struct {
	DeviceID       string `json:"device_id"`
	WorkspaceID    string `json:"workspace_id"`
	Goal           string `json:"goal"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	IdempotencyKey string `json:"idempotency_key"`
}

type runtimePublicView struct {
	RuntimeID    string                        `json:"runtime_id"`
	State        modelturn.RuntimeState        `json:"state"`
	DeviceID     string                        `json:"device_id,omitempty"`
	WorkspaceID  string                        `json:"workspace_id,omitempty"`
	Controller   modelturn.RuntimeController   `json:"controller"`
	LastSequence uint64                        `json:"last_sequence"`
	UpdatedAt    time.Time                     `json:"updated_at"`
	ResultRef    string                        `json:"result_ref,omitempty"`
	Phases       []modelturn.RuntimePhaseEvent `json:"phases,omitempty"`
}

func (s *Server) WithEdgeStore(store edgeDeviceRegistry) *Server {
	s.edgeDevices = store
	s.edgeWorkspaces = nil
	s.edgeOperations = nil
	if workspaces, ok := store.(edgeWorkspaceRegistry); ok {
		s.edgeWorkspaces = workspaces
	}
	if operations, ok := store.(edgeOperationRegistry); ok {
		s.edgeOperations = operations
	}
	return s
}

func publicRuntime(runtime modelturn.Runtime) runtimePublicView {
	return runtimePublicView{
		RuntimeID: runtime.RuntimeID, State: runtime.State, DeviceID: runtime.DeviceID,
		WorkspaceID: runtime.WorkspaceID, Controller: runtime.Controller,
		LastSequence: runtime.LastSequence, UpdatedAt: runtime.UpdatedAt, ResultRef: runtime.ResultRef,
		Phases: runtime.Phases,
	}
}

func (s *Server) handleOpenCodeRuntimeStart(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	if s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var params openCodeRuntimeStartParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	goal := []byte(params.Goal)
	if len(goal) == 0 || int64(len(goal)) > modelturn.MaxGoalBodyBytes || !utf8.Valid(goal) || strings.TrimSpace(params.Goal) == "" {
		return "", modelturn.ErrInvalidRequest
	}
	if params.TimeoutSeconds < 1 || time.Duration(params.TimeoutSeconds)*time.Second > modelturn.MaxTurnTTL || len(params.IdempotencyKey) == 0 || len(params.IdempotencyKey) > maxOpenCodeIdempotencyBytes {
		return "", modelturn.ErrInvalidRequest
	}
	if !s.edgeDevices.DeviceActive(params.DeviceID) {
		return "", errors.New("active edge device not found")
	}
	if s.edgeWorkspaces == nil {
		return "", errWorkspaceRegistryUnavailable
	}
	binding, err := s.edgeWorkspaces.ResolveWorkspace(params.WorkspaceID)
	if err != nil || !validWorkspaceBinding(binding, params.WorkspaceID) || binding.DeviceID != params.DeviceID || binding.Mode != "dev" {
		return "", errors.New("registered development workspace not found")
	}
	ttl := time.Duration(params.TimeoutSeconds) * time.Second
	body, err := s.modelTurns.StageRuntimeGoal(context.Background(), goal, ttl)
	if err != nil {
		return "", err
	}
	runtime, created, err := s.modelTurns.StartBoundRuntime(context.Background(), modelturn.BoundRuntimeRequest{
		DeviceID: params.DeviceID, WorkspaceID: params.WorkspaceID,
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: body.BodyRef, GoalDigest: body.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest(params.IdempotencyKey), TTL: ttl,
	})
	if err != nil || !created {
		_ = s.modelTurns.DiscardRuntimeGoal(context.Background(), body.BodyRef, body.ContentDigest)
	}
	return marshalToolValue(publicRuntime(runtime), err)
}
