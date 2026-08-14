package codexadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	maxPromptItems       = 2048
	maxPromptTextBytes   = 1 << 20
	maxResponsesTools    = 128
	maxPromptCacheBytes  = 512
	maxClientMetadataRaw = 64 << 10
)

type responsesRequest struct {
	Model             string            `json:"model"`
	Instructions      string            `json:"instructions,omitempty"`
	Input             []json.RawMessage `json:"input"`
	Tools             []json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage   `json:"tool_choice,omitempty"`
	ParallelToolCalls bool              `json:"parallel_tool_calls,omitempty"`
	Reasoning         json.RawMessage   `json:"reasoning,omitempty"`
	Store             bool              `json:"store,omitempty"`
	Stream            bool              `json:"stream"`
	Include           []string          `json:"include,omitempty"`
	PromptCacheKey    string            `json:"prompt_cache_key,omitempty"`
	ClientMetadata    json.RawMessage   `json:"client_metadata,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type responsesNamespaceTool struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Tools       []json.RawMessage `json:"tools"`
}

type responsesWebSearchTool struct {
	Type              string `json:"type"`
	ExternalWebAccess bool   `json:"external_web_access"`
}

type normalizedRequest struct {
	payload      json.RawMessage
	offeredTools []modelturn.ToolDefinition
	toolsByID    map[string]normalizedTool
}

type normalizedTool struct {
	ID          string
	Name        string
	Description string
	Schema      map[string]any
}

type inputMessage struct {
	Type    string            `json:"type"`
	ID      string            `json:"id,omitempty"`
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type inputTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type inputFunctionCall struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status,omitempty"`
}

type inputFunctionOutput struct {
	Type   string          `json:"type"`
	ID     string          `json:"id,omitempty"`
	CallID string          `json:"call_id"`
	Output json.RawMessage `json:"output"`
	Status string          `json:"status,omitempty"`
}

type inputReasoning struct {
	Type             string          `json:"type"`
	ID               string          `json:"id,omitempty"`
	Summary          json.RawMessage `json:"summary,omitempty"`
	EncryptedContent string          `json:"encrypted_content,omitempty"`
}

type reasoningOptions struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

func normalizeResponsesRequest(input responsesRequest) (normalizedRequest, error) {
	if len(input.Input) > maxPromptItems || len(input.Tools) > maxResponsesTools {
		return normalizedRequest{}, errors.New("Responses request exceeds item limits")
	}
	if len([]byte(input.Instructions)) > maxPromptTextBytes || len([]byte(input.PromptCacheKey)) > maxPromptCacheBytes {
		return normalizedRequest{}, errors.New("Responses request text exceeds limits")
	}
	if err := validateIgnoredRequestMetadata(input); err != nil {
		return normalizedRequest{}, err
	}

	toolsByID := make(map[string]normalizedTool, len(input.Tools))
	toolNames := make(map[string]struct{}, len(input.Tools))
	offeredTools := make([]modelturn.ToolDefinition, 0, len(input.Tools))
	payloadTools := make([]any, 0, len(input.Tools))
	for _, rawTool := range input.Tools {
		toolType, err := responsesToolType(rawTool)
		if err != nil {
			return normalizedRequest{}, err
		}
		if toolType == "namespace" {
			if err := validateDeferredNamespaceTool(rawTool); err != nil {
				return normalizedRequest{}, err
			}
			continue
		}
		if toolType == "web_search" {
			var search responsesWebSearchTool
			if err := decodeStrict(rawTool, &search); err != nil || search.ExternalWebAccess {
				return normalizedRequest{}, errors.New("Responses web search tool is invalid")
			}
			continue
		}
		if toolType != "function" {
			return normalizedRequest{}, errors.New("Responses tool type is unsupported")
		}
		var tool responsesTool
		if err := decodeStrict(rawTool, &tool); err != nil {
			return normalizedRequest{}, errors.New("Responses function tool is invalid")
		}
		if tool.Type != "function" || !identifierPattern.MatchString(tool.Name) {
			return normalizedRequest{}, errors.New("Responses tool is invalid")
		}
		if _, duplicate := toolNames[tool.Name]; duplicate {
			return normalizedRequest{}, errors.New("Responses tool name is duplicated")
		}
		toolNames[tool.Name] = struct{}{}
		schema, err := decodeJSONObject(tool.Parameters, "Responses tool parameters")
		if err != nil {
			return normalizedRequest{}, err
		}
		id, err := toolID(tool.Name, tool.Parameters)
		if err != nil {
			return normalizedRequest{}, err
		}
		canonicalSchema, err := canonicalJSON(schema)
		if err != nil {
			return normalizedRequest{}, err
		}
		normalized := normalizedTool{ID: id, Name: tool.Name, Description: tool.Description, Schema: schema}
		toolsByID[id] = normalized
		offeredTools = append(offeredTools, modelturn.ToolDefinition{ID: id, Name: tool.Name, Schema: canonicalSchema})
		payloadTools = append(payloadTools, map[string]any{
			"description":  tool.Description,
			"id":           id,
			"input_schema": schema,
			"name":         tool.Name,
		})
	}

	prompt, err := normalizeInput(input.Instructions, input.Input)
	if err != nil {
		return normalizedRequest{}, err
	}
	choice, err := normalizeResponsesToolChoice(input.ToolChoice, toolNames)
	if err != nil {
		return normalizedRequest{}, err
	}
	payload, err := canonicalJSON(map[string]any{
		"generation":       map[string]any{},
		"model_id":         input.Model,
		"prompt":           prompt,
		"protocol_version": ProtocolVersion,
		"response_format":  map[string]any{"type": "text"},
		"tool_choice":      choice,
		"tools":            payloadTools,
	})
	if err != nil {
		return normalizedRequest{}, err
	}
	return normalizedRequest{payload: payload, offeredTools: offeredTools, toolsByID: toolsByID}, nil
}

func responsesToolType(raw json.RawMessage) (string, error) {
	value, err := decodeJSONObject(raw, "Responses tool")
	if err != nil {
		return "", err
	}
	toolType, ok := value["type"].(string)
	if !ok || toolType == "" {
		return "", errors.New("Responses tool type is missing")
	}
	return toolType, nil
}

func validateDeferredNamespaceTool(raw json.RawMessage) error {
	var namespace responsesNamespaceTool
	if err := decodeStrict(raw, &namespace); err != nil || !identifierPattern.MatchString(namespace.Name) {
		return errors.New("Responses namespace tool is invalid")
	}
	if len(namespace.Tools) > maxResponsesTools {
		return errors.New("Responses namespace has too many tools")
	}
	for _, rawTool := range namespace.Tools {
		var tool responsesTool
		if err := decodeStrict(rawTool, &tool); err != nil || tool.Type != "function" || !identifierPattern.MatchString(tool.Name) {
			return errors.New("Responses namespace member is invalid")
		}
		if _, err := decodeJSONObject(tool.Parameters, "Responses namespace parameters"); err != nil {
			return err
		}
	}
	return nil
}

func validateIgnoredRequestMetadata(input responsesRequest) error {
	if len(input.Reasoning) != 0 {
		var reasoning reasoningOptions
		if err := decodeStrict(input.Reasoning, &reasoning); err != nil {
			return errors.New("Responses reasoning options are invalid")
		}
		if reasoning.Summary != "" && reasoning.Summary != "auto" && reasoning.Summary != "concise" && reasoning.Summary != "detailed" {
			return errors.New("Responses reasoning summary is unsupported")
		}
		if reasoning.Effort != "" && reasoning.Effort != "none" && reasoning.Effort != "minimal" && reasoning.Effort != "low" && reasoning.Effort != "medium" && reasoning.Effort != "high" && reasoning.Effort != "xhigh" {
			return errors.New("Responses reasoning effort is unsupported")
		}
	}
	seenInclude := make(map[string]struct{}, len(input.Include))
	for _, include := range input.Include {
		if include != "reasoning.encrypted_content" {
			return errors.New("Responses include value is unsupported")
		}
		if _, duplicate := seenInclude[include]; duplicate {
			return errors.New("Responses include value is duplicated")
		}
		seenInclude[include] = struct{}{}
	}
	if len(input.ClientMetadata) != 0 {
		if len(input.ClientMetadata) > maxClientMetadataRaw {
			return errors.New("Responses client metadata exceeds the limit")
		}
		if _, err := decodeJSONObject(input.ClientMetadata, "Responses client metadata"); err != nil {
			return err
		}
	}
	return nil
}

func normalizeInput(instructions string, items []json.RawMessage) ([]any, error) {
	callNames := make(map[string]string)
	for _, raw := range items {
		kind, err := responsesInputType(raw)
		if err != nil {
			return nil, errors.New("Responses input item is invalid")
		}
		if kind != "function_call" {
			continue
		}
		var call inputFunctionCall
		if err := decodeStrict(raw, &call); err != nil || !identifierPattern.MatchString(call.CallID) || !identifierPattern.MatchString(call.Name) {
			return nil, errors.New("Responses function call is invalid")
		}
		if _, duplicate := callNames[call.CallID]; duplicate {
			return nil, errors.New("Responses function call id is duplicated")
		}
		callNames[call.CallID] = call.Name
	}

	prompt := make([]any, 0, len(items)+1)
	if instructions != "" {
		prompt = append(prompt, map[string]any{"content": instructions, "role": "system"})
	}
	for _, raw := range items {
		kind, err := responsesInputType(raw)
		if err != nil {
			return nil, errors.New("Responses input item is invalid")
		}
		switch kind {
		case "message":
			message, err := normalizeInputMessage(raw)
			if err != nil {
				return nil, err
			}
			prompt = append(prompt, message)
		case "function_call":
			var call inputFunctionCall
			if err := decodeStrict(raw, &call); err != nil {
				return nil, errors.New("Responses function call is invalid")
			}
			arguments, err := decodeJSONObject(json.RawMessage(call.Arguments), "Responses function arguments")
			if err != nil {
				return nil, err
			}
			prompt = append(prompt, map[string]any{
				"content": []any{map[string]any{
					"input":        arguments,
					"tool_call_id": call.CallID,
					"tool_name":    call.Name,
					"type":         "tool-call",
				}},
				"role": "assistant",
			})
		case "function_call_output":
			var output inputFunctionOutput
			if err := decodeStrict(raw, &output); err != nil || !identifierPattern.MatchString(output.CallID) {
				return nil, errors.New("Responses function output is invalid")
			}
			name, exists := callNames[output.CallID]
			if !exists {
				return nil, errors.New("Responses function output lacks its offered call")
			}
			value, err := decodeJSONValue(output.Output)
			if err != nil {
				return nil, errors.New("Responses function output is invalid")
			}
			prompt = append(prompt, map[string]any{
				"content": []any{map[string]any{
					"output":       value,
					"tool_call_id": output.CallID,
					"tool_name":    name,
					"type":         "tool-result",
				}},
				"role": "tool",
			})
		case "reasoning":
			var reasoning inputReasoning
			if err := decodeStrict(raw, &reasoning); err != nil {
				return nil, errors.New("Responses reasoning item is invalid")
			}
			if len(reasoning.Summary) != 0 {
				var summary []json.RawMessage
				if err := json.Unmarshal(reasoning.Summary, &summary); err != nil {
					return nil, errors.New("Responses reasoning summary is invalid")
				}
			}
		default:
			return nil, fmt.Errorf("Responses input type %q is unsupported", kind)
		}
	}
	return prompt, nil
}

func responsesInputType(raw json.RawMessage) (string, error) {
	value, err := decodeJSONObject(raw, "Responses input item")
	if err != nil {
		return "", err
	}
	kind, ok := value["type"].(string)
	if !ok || kind == "" {
		return "", errors.New("Responses input item type is missing")
	}
	return kind, nil
}

func normalizeInputMessage(raw json.RawMessage) (map[string]any, error) {
	var message inputMessage
	if err := decodeStrict(raw, &message); err != nil {
		return nil, errors.New("Responses message is invalid")
	}
	role := message.Role
	if role == "developer" {
		role = "system"
	}
	if role != "system" && role != "user" && role != "assistant" {
		return nil, errors.New("Responses message role is unsupported")
	}
	if len(message.Content) > maxPromptItems {
		return nil, errors.New("Responses message has too many parts")
	}
	parts := make([]any, 0, len(message.Content))
	for _, rawPart := range message.Content {
		var part inputTextPart
		if err := decodeStrict(rawPart, &part); err != nil || (part.Type != "input_text" && part.Type != "output_text") {
			return nil, errors.New("Responses message part is unsupported")
		}
		if len([]byte(part.Text)) > maxPromptTextBytes {
			return nil, errors.New("Responses message text exceeds the limit")
		}
		parts = append(parts, map[string]any{"text": part.Text, "type": "text"})
	}
	if role == "system" {
		var text strings.Builder
		for index, part := range parts {
			if index != 0 {
				text.WriteByte('\n')
			}
			text.WriteString(part.(map[string]any)["text"].(string))
		}
		return map[string]any{"content": text.String(), "role": role}, nil
	}
	return map[string]any{"content": parts, "role": role}, nil
}

func normalizeResponsesToolChoice(raw json.RawMessage, toolNames map[string]struct{}) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{"type": "auto"}, nil
	}
	var choice string
	if err := json.Unmarshal(raw, &choice); err == nil {
		if choice == "auto" || choice == "none" || choice == "required" {
			return map[string]any{"type": choice}, nil
		}
		return nil, errors.New("Responses tool choice is unsupported")
	}
	var selected struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := decodeStrict(raw, &selected); err != nil || selected.Type != "function" {
		return nil, errors.New("Responses tool choice is invalid")
	}
	if _, exists := toolNames[selected.Name]; !exists {
		return nil, errors.New("Responses tool choice names an unoffered tool")
	}
	return map[string]any{"tool_name": selected.Name, "type": "tool"}, nil
}

func toolID(name string, schema json.RawMessage) (string, error) {
	if !identifierPattern.MatchString(name) {
		return "", errors.New("tool name is invalid")
	}
	value, err := decodeJSONObject(schema, "tool schema")
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append(append([]byte(name), '\n'), canonical...))
	return "tool_" + hex.EncodeToString(digest[:])[:24], nil
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values are not allowed")
	}
	return value, nil
}
