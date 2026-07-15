package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	maxModelTextBytes = 1 << 20
	maxModelToolCalls = 64
)

var errModelTurnStoreUnavailable = errors.New("model turn store is not configured")

func (s *Server) WithModelTurnStore(store *modelturn.Store) *Server {
	s.modelTurns = store
	return s
}

func (s *Server) addModelTurnTools() {
	readHints := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	writeHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	cancelHints := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false}

	s.addDirectTool(toolDef{
		Name:        "model_runtime_start",
		Description: "Create one bounded durable external-model runtime without starting a model provider.",
		InputSchema: closedObject(map[string]any{}, nil),
		Version:     "1",
		Annotations: writeHints,
	}, s.handleModelRuntimeStart)

	s.addDirectTool(toolDef{
		Name:        "model_runtime_status",
		Description: "Return compact durable status for one external-model runtime.",
		InputSchema: closedObject(map[string]any{
			"runtime_id": stringSchema("opaque model runtime id", `^mr_[a-f0-9]{32}$`, 35),
		}, []string{"runtime_id"}),
		Version:     "1",
		Annotations: readHints,
	}, s.handleModelRuntimeStatus)

	s.addDirectTool(toolDef{
		Name:        "model_turn_next",
		Description: "Poll one runtime for its next awaiting external-model turn.",
		InputSchema: closedObject(map[string]any{
			"runtime_id": stringSchema("opaque model runtime id", `^mr_[a-f0-9]{32}$`, 35),
		}, []string{"runtime_id"}),
		Version:     "1",
		Annotations: readHints,
	}, s.handleModelTurnNext)

	s.addDirectTool(toolDef{
		Name:        "model_turn_respond",
		Description: "Submit exactly one bounded response for an offered model turn after sequence, digest, and tool-id validation.",
		InputSchema: modelTurnRespondSchema(),
		Version:     "1",
		Annotations: writeHints,
	}, s.handleModelTurnRespond)

	s.addDirectTool(toolDef{
		Name:        "model_runtime_cancel",
		Description: "Cancel one runtime and all of its unconsumed active model turns.",
		InputSchema: closedObject(map[string]any{
			"runtime_id": stringSchema("opaque model runtime id", `^mr_[a-f0-9]{32}$`, 35),
		}, []string{"runtime_id"}),
		Version:     "1",
		Annotations: cancelHints,
	}, s.handleModelRuntimeCancel)
}

func (s *Server) addDirectTool(def toolDef, handler func(json.RawMessage) (string, error)) {
	s.table[def.Name] = toolEntry{def: def, handler: handler}
	s.order = append(s.order, def.Name)
}

func closedObject(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description, pattern string, maxLength int) map[string]any {
	return map[string]any{"type": "string", "description": description, "pattern": pattern, "minLength": 1, "maxLength": maxLength}
}

func modelTurnRespondSchema() map[string]any {
	toolCall := closedObject(map[string]any{
		"call_id":   stringSchema("response-local tool call id", `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, 128),
		"tool_id":   stringSchema("tool id offered by the corresponding request", `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, 128),
		"arguments": map[string]any{"type": "object"},
	}, []string{"call_id", "tool_id", "arguments"})
	usage := closedObject(map[string]any{
		"input_tokens":  map[string]any{"type": "integer", "minimum": 0},
		"output_tokens": map[string]any{"type": "integer", "minimum": 0},
		"total_tokens":  map[string]any{"type": "integer", "minimum": 0},
	}, []string{"input_tokens", "output_tokens", "total_tokens"})
	response := closedObject(map[string]any{
		"text":          map[string]any{"type": "string", "maxLength": maxModelTextBytes},
		"tool_calls":    map[string]any{"type": "array", "maxItems": maxModelToolCalls, "items": toolCall},
		"finish_reason": map[string]any{"type": "string", "enum": []string{"stop", "tool_calls", "length", "cancelled", "error"}},
		"usage":         usage,
	}, []string{"finish_reason"})
	return closedObject(map[string]any{
		"runtime_id":        stringSchema("opaque model runtime id", `^mr_[a-f0-9]{32}$`, 35),
		"turn_id":           stringSchema("opaque model turn id", `^mt_[a-f0-9]{32}$`, 35),
		"expected_sequence": map[string]any{"type": "integer", "minimum": 1},
		"request_digest":    stringSchema("canonical request SHA-256 digest", `^sha256:[a-f0-9]{64}$`, 71),
		"response":          response,
	}, []string{"runtime_id", "turn_id", "expected_sequence", "request_digest", "response"})
}

type runtimeIDParams struct {
	RuntimeID string `json:"runtime_id"`
}

type modelToolCall struct {
	CallID    string          `json:"call_id"`
	ToolID    string          `json:"tool_id"`
	Arguments json.RawMessage `json:"arguments"`
}

type modelUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type boundedModelResponse struct {
	Text         string          `json:"text,omitempty"`
	ToolCalls    []modelToolCall `json:"tool_calls,omitempty"`
	FinishReason string          `json:"finish_reason"`
	Usage        *modelUsage     `json:"usage,omitempty"`
}

type modelTurnRespondParams struct {
	RuntimeID        string               `json:"runtime_id"`
	TurnID           modelturn.TurnID     `json:"turn_id"`
	ExpectedSequence uint64               `json:"expected_sequence"`
	RequestDigest    string               `json:"request_digest"`
	Response         boundedModelResponse `json:"response"`
}

func (s *Server) handleModelRuntimeStart(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var params struct{}
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	runtime, err := s.modelTurns.StartRuntime(context.Background())
	return marshalToolValue(runtime, err)
}

func (s *Server) handleModelRuntimeStatus(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var params runtimeIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	runtime, err := s.modelTurns.Runtime(context.Background(), params.RuntimeID)
	return marshalToolValue(runtime, err)
}

func (s *Server) handleModelTurnNext(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var params runtimeIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	offer, pending, err := s.modelTurns.Poll(context.Background(), params.RuntimeID)
	if err != nil {
		return "", err
	}
	if !pending {
		return marshalToolValue(map[string]any{"runtime_id": params.RuntimeID, "pending": false}, nil)
	}
	return marshalToolValue(map[string]any{"pending": true, "turn": offer}, nil)
}

func (s *Server) handleModelTurnRespond(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var params modelTurnRespondParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if params.ExpectedSequence == 0 || !strings.HasPrefix(params.RequestDigest, "sha256:") {
		return "", modelturn.ErrInvalidRequest
	}
	if len([]byte(params.Response.Text)) > maxModelTextBytes || len(params.Response.ToolCalls) > maxModelToolCalls {
		return "", modelturn.ErrInvalidRequest
	}
	switch params.Response.FinishReason {
	case "stop", "tool_calls", "length", "cancelled", "error":
	default:
		return "", modelturn.ErrInvalidRequest
	}
	used := make([]string, 0, len(params.Response.ToolCalls))
	seenCalls := make(map[string]struct{}, len(params.Response.ToolCalls))
	for _, call := range params.Response.ToolCalls {
		if call.CallID == "" || call.ToolID == "" {
			return "", modelturn.ErrInvalidRequest
		}
		if _, exists := seenCalls[call.CallID]; exists {
			return "", modelturn.ErrInvalidRequest
		}
		seenCalls[call.CallID] = struct{}{}
		var object map[string]json.RawMessage
		if err := decodeClosed(call.Arguments, &object); err != nil {
			return "", fmt.Errorf("tool arguments must be one JSON object: %w", err)
		}
		used = append(used, call.ToolID)
	}
	if params.Response.FinishReason == "tool_calls" && len(params.Response.ToolCalls) == 0 {
		return "", modelturn.ErrInvalidRequest
	}
	payload, err := json.Marshal(params.Response)
	if err != nil {
		return "", err
	}
	record, err := s.modelTurns.Respond(context.Background(), modelturn.ResponseSubmission{
		RuntimeID:        params.RuntimeID,
		TurnID:           params.TurnID,
		ExpectedSequence: params.ExpectedSequence,
		RequestDigest:    params.RequestDigest,
		Payload:          payload,
		UsedToolIDs:      used,
	})
	return marshalToolValue(record, err)
}

func (s *Server) handleModelRuntimeCancel(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var params runtimeIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if err := s.modelTurns.CancelRuntime(context.Background(), params.RuntimeID); err != nil {
		return "", err
	}
	return marshalToolValue(map[string]any{"runtime_id": params.RuntimeID, "status": modelturn.RuntimeCancelled}, nil)
}

func decodeClosed(arguments json.RawMessage, target any) error {
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func marshalToolValue(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	encoded, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(encoded), nil
}
