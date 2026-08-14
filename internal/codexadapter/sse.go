package codexadapter

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type boundedResponse struct {
	Text         string            `json:"text,omitempty"`
	ToolCalls    []boundedToolCall `json:"tool_calls,omitempty"`
	FinishReason string            `json:"finish_reason"`
	Usage        *boundedUsage     `json:"usage,omitempty"`
}

type boundedToolCall struct {
	CallID    string          `json:"call_id"`
	ToolID    string          `json:"tool_id"`
	Arguments json.RawMessage `json:"arguments"`
}

type boundedUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type validatedToolCall struct {
	CallID    string
	ToolID    string
	Arguments map[string]any
}

type validatedResponse struct {
	Text         string
	ToolCalls    []validatedToolCall
	FinishReason string
	Usage        boundedUsage
}

func validateBoundedResponse(raw json.RawMessage, toolsByID map[string]normalizedTool) (validatedResponse, error) {
	var response boundedResponse
	if err := decodeStrict(raw, &response); err != nil {
		return validatedResponse{}, err
	}
	if len([]byte(response.Text)) > maxModelTextBytes || len(response.ToolCalls) > maxModelToolCalls {
		return validatedResponse{}, errors.New("model response exceeds limits")
	}
	switch response.FinishReason {
	case "stop", "tool_calls", "length", "cancelled", "error":
	default:
		return validatedResponse{}, errors.New("model finish reason is invalid")
	}
	if response.FinishReason == "tool_calls" && len(response.ToolCalls) == 0 {
		return validatedResponse{}, errors.New("tool_calls requires one call")
	}
	seen := make(map[string]struct{}, len(response.ToolCalls))
	calls := make([]validatedToolCall, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		if !identifierPattern.MatchString(call.CallID) || !identifierPattern.MatchString(call.ToolID) {
			return validatedResponse{}, errors.New("model tool call identity is invalid")
		}
		if _, duplicate := seen[call.CallID]; duplicate {
			return validatedResponse{}, errors.New("model tool call id is duplicated")
		}
		seen[call.CallID] = struct{}{}
		if _, exists := toolsByID[call.ToolID]; !exists {
			return validatedResponse{}, errors.New("model used an unoffered tool")
		}
		arguments, err := decodeJSONObject(call.Arguments, "model tool call arguments")
		if err != nil {
			return validatedResponse{}, err
		}
		calls = append(calls, validatedToolCall{CallID: call.CallID, ToolID: call.ToolID, Arguments: arguments})
	}
	usage := boundedUsage{}
	if response.Usage != nil {
		usage = *response.Usage
		if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < usage.InputTokens+usage.OutputTokens {
			return validatedResponse{}, errors.New("model usage is invalid")
		}
	}
	return validatedResponse{Text: response.Text, ToolCalls: calls, FinishReason: response.FinishReason, Usage: usage}, nil
}

func writeResponsesSSE(writer http.ResponseWriter, turn modelturn.Turn, response validatedResponse, toolsByID map[string]normalizedTool) error {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	buffer := bufio.NewWriter(writer)
	responseID := "resp_" + string(turn.ID)[3:]
	if err := writeSSEEvent(buffer, "response.created", map[string]any{
		"response": map[string]any{"id": responseID},
		"type":     "response.created",
	}); err != nil {
		return err
	}
	if response.Text != "" {
		if err := writeSSEEvent(buffer, "response.output_item.done", map[string]any{
			"item": map[string]any{
				"content": []any{map[string]any{"text": response.Text, "type": "output_text"}},
				"id":      "msg_" + string(turn.ID)[3:],
				"role":    "assistant",
				"type":    "message",
			},
			"type": "response.output_item.done",
		}); err != nil {
			return err
		}
	}
	for _, call := range response.ToolCalls {
		tool := toolsByID[call.ToolID]
		arguments, err := canonicalJSON(call.Arguments)
		if err != nil {
			return err
		}
		if err := writeSSEEvent(buffer, "response.output_item.done", map[string]any{
			"item": map[string]any{
				"arguments": string(arguments),
				"call_id":   call.CallID,
				"id":        functionItemID(turn.ID, call.CallID),
				"name":      tool.Name,
				"type":      "function_call",
			},
			"type": "response.output_item.done",
		}); err != nil {
			return err
		}
	}
	if response.FinishReason == "error" || response.FinishReason == "cancelled" {
		if err := writeSSEEvent(buffer, "response.failed", map[string]any{
			"response": map[string]any{
				"error":  map[string]any{"code": "model_" + response.FinishReason, "message": "external model turn did not complete"},
				"id":     responseID,
				"status": "failed",
			},
			"type": "response.failed",
		}); err != nil {
			return err
		}
		return buffer.Flush()
	}
	if err := writeSSEEvent(buffer, "response.completed", map[string]any{
		"response": map[string]any{
			"id": responseID,
			"usage": map[string]any{
				"input_tokens":          response.Usage.InputTokens,
				"input_tokens_details":  nil,
				"output_tokens":         response.Usage.OutputTokens,
				"output_tokens_details": nil,
				"total_tokens":          response.Usage.TotalTokens,
			},
		},
		"type": "response.completed",
	}); err != nil {
		return err
	}
	return buffer.Flush()
}

func writeSSEEvent(writer io.Writer, event string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, body)
	return err
}

func functionItemID(turnID modelturn.TurnID, callID string) string {
	digest := sha256.Sum256([]byte(string(turnID) + "\n" + callID))
	return "fc_" + hex.EncodeToString(digest[:])[:32]
}
