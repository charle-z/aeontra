package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
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
		Description: "Continue one already registered workspace using only its local trusted contract. The tool accepts no instructions, creates one runtime, and never retries automatically.",
		InputSchema: closedObject(map[string]any{
			"workspace_id":    stringSchema("opaque registered workspace id", `^ws_[a-f0-9]{32}$`, 35),
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": int(modelturn.MaxTurnTTL / time.Second)},
		}, []string{"workspace_id", "timeout_seconds"}),
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
	idempotencyDigest, err := workspaceContinuationIdempotencyDigest(sessionKey, requestID, params.WorkspaceID, params.TimeoutSeconds)
	if err != nil {
		return "", err
	}
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
		return binding.Mode == "dev"
	default:
		return false
	}
}

func workspaceContinuationIdempotencyDigest(sessionKey string, requestID json.RawMessage, workspaceID string, timeoutSeconds int) (string, error) {
	rawID := bytes.TrimSpace(requestID)
	if len(rawID) == 0 || len(rawID) > 256 || string(rawID) == "null" {
		return "", modelturn.ErrInvalidRequest
	}
	var scalar any
	decoder := json.NewDecoder(bytes.NewReader(rawID))
	decoder.UseNumber()
	if err := decoder.Decode(&scalar); err != nil {
		return "", modelturn.ErrInvalidRequest
	}
	switch scalar.(type) {
	case string, json.Number:
	default:
		return "", modelturn.ErrInvalidRequest
	}
	material := workspaceContinuationGoalVersion + "\x00" + sessionKey + "\x00" + string(rawID) + "\x00" + workspaceID + "\x00" + strconv.Itoa(timeoutSeconds)
	sum := sha256.Sum256([]byte(material))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
