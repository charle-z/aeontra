package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	workspaceContinuationGoalVersion = "resume-local-contract-v1"
	workspaceContinuationGoal        = "Resume the registered workspace using its local trusted contract and persistent checkpoint. Perform only operations authorized by the local contract. Keep local-only values local. Return a bounded safe status."
)

var errWorkspaceRegistryUnavailable = errors.New("edge workspace registry is not configured")

type edgeWorkspaceRegistry interface {
	ResolveWorkspace(string) (edge.WorkspaceBinding, error)
}

type workspaceRuntimeContinueParams struct {
	WorkspaceID    string `json:"workspace_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	IdempotencyKey string `json:"idempotency_key"`
}

type workspaceRuntimeContinueView struct {
	RuntimeID       string                 `json:"runtime_id"`
	WorkspaceID     string                 `json:"workspace_id"`
	DeviceID        string                 `json:"device_id"`
	State           modelturn.RuntimeState `json:"state"`
	CreatedAt       time.Time              `json:"created_at"`
	ExpiresAt       time.Time              `json:"expires_at"`
	LastSequence    uint64                 `json:"last_sequence"`
	FailureCategory string                 `json:"failure_category"`
}

func (s *Server) addRequestTool(def toolDef, handler func(json.RawMessage, string, json.RawMessage) (string, error)) {
	s.table[def.Name] = toolEntry{def: def, requestHandler: handler}
	s.order = append(s.order, def.Name)
}

func (s *Server) addWorkspaceRuntimeContinueTool() {
	hints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	s.addRequestTool(toolDef{
		Name:        "workspace_runtime_continue",
		Description: "Continue one already registered workspace through the active ChatGPT session using only its local trusted contract. Generate a fresh idempotency_key for each explicit continuation and reuse it only to retry that same call. The tool accepts no instructions, creates one runtime, and never retries automatically.",
		InputSchema: closedObject(map[string]any{
			"workspace_id":    stringSchema("opaque registered workspace id", `^ws_[a-f0-9]{32}$`, 35),
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": int(modelturn.MaxTurnTTL / time.Second)},
			"idempotency_key": stringSchema("fresh caller-generated key for this explicit continuation; reuse only for an exact retry", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`, maxOpenCodeIdempotencyBytes),
		}, []string{"workspace_id", "timeout_seconds", "idempotency_key"}),
		Version:     "1",
		Annotations: hints,
	}, s.handleWorkspaceRuntimeContinue)
}

func (s *Server) handleWorkspaceRuntimeContinue(arguments json.RawMessage, sessionKey string, requestID json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	if s.edgeWorkspaces == nil {
		return "", errWorkspaceRegistryUnavailable
	}
	var params workspaceRuntimeContinueParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if params.TimeoutSeconds < 1 || time.Duration(params.TimeoutSeconds)*time.Second > modelturn.MaxTurnTTL {
		return "", modelturn.ErrInvalidRequest
	}
	binding, err := s.edgeWorkspaces.ResolveWorkspace(params.WorkspaceID)
	if err != nil {
		return "", err
	}
	if !validWorkspaceBinding(binding, params.WorkspaceID) {
		return "", modelturn.ErrInvalidRequest
	}
	if s.edgeDevices == nil || !s.edgeDevices.DeviceActive(binding.DeviceID) {
		return "", errors.New("registered active workspace not found")
	}
	if strings.TrimSpace(params.IdempotencyKey) != params.IdempotencyKey || len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > maxOpenCodeIdempotencyBytes {
		return "", modelturn.ErrInvalidRequest
	}
	idempotencyDigest := modelturn.IdempotencyDigest(workspaceContinuationGoalVersion + "\x00" + params.WorkspaceID + "\x00" + params.IdempotencyKey)
	ttl := time.Duration(params.TimeoutSeconds) * time.Second
	goal := []byte(workspaceContinuationGoal)
	body, err := s.modelTurns.StageRuntimeGoal(context.Background(), goal, ttl)
	if err != nil {
		return "", err
	}
	runtime, created, err := s.modelTurns.StartWorkspaceContinuationRuntime(context.Background(), modelturn.BoundRuntimeRequest{
		DeviceID:             binding.DeviceID,
		WorkspaceID:          binding.WorkspaceID,
		Controller:           modelturn.ControllerRemoteEdge,
		GoalSummary:          modelturn.GoalSummary(goal),
		GoalRef:              body.BodyRef,
		GoalDigest:           body.ContentDigest,
		IdempotencyKeyDigest: idempotencyDigest,
		TTL:                  ttl,
	})
	if err != nil || !created {
		_ = s.modelTurns.DiscardRuntimeGoal(context.Background(), body.BodyRef, body.ContentDigest)
	}
	if err != nil {
		return "", err
	}
	return marshalToolValue(workspaceRuntimeContinueView{
		RuntimeID: runtime.RuntimeID, WorkspaceID: runtime.WorkspaceID, DeviceID: runtime.DeviceID,
		State: runtime.State, CreatedAt: runtime.CreatedAt, ExpiresAt: runtime.ExpiresAt,
		LastSequence: runtime.LastSequence, FailureCategory: "",
	}, nil)
}

func validWorkspaceBinding(binding edge.WorkspaceBinding, workspaceID string) bool {
	if binding.WorkspaceID != workspaceID || binding.DeviceID == "" {
		return false
	}
	switch binding.Profile {
	case "sandbox":
		return binding.Mode == "dev"
	case "linux-workcell":
		return binding.Mode == "dev" || binding.Mode == "htb-linux"
	default:
		return false
	}
}
