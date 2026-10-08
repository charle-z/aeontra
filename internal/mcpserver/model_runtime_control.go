package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func (s *Server) addModelRuntimeControlTool(hints map[string]any) {
	s.addDirectTool(toolDef{Name: "model_runtime_control", Version: "1", Annotations: hints,
		Description: "Opt in to fenced model-response ownership on one pending turn. Claim, prepare a successor, ACK its exact turn, transfer atomically, then release the old controller. Abort a pending handoff at a new generation. This keeps the same worker/runtime/worktree; it neither transfers host authority nor replays or stops pending effects. Reconcile those effects before continuing. Legacy uncontrolled runtimes are unchanged; controlled responses require controller_id and control_generation.",
		InputSchema: closedObject(map[string]any{
			"runtime_id":        stringSchema("opaque model runtime", `^mr_[a-f0-9]{32}$`, 35),
			"action":            map[string]any{"type": "string", "enum": []string{"claim", "prepare", "ack", "transfer", "abort", "release"}},
			"controller_id":     stringSchema("caller-generated controller identity; coordination, not an authentication credential", `^mc_[a-f0-9]{32}$`, 35),
			"generation":        map[string]any{"type": "integer", "minimum": 0, "maximum": 1000000000},
			"successor_id":      stringSchema("different successor controller identity for prepare", `^mc_[a-f0-9]{32}$`, 35),
			"turn_id":           stringSchema("exact unconsumed pending turn", `^mt_[a-f0-9]{32}$`, 35),
			"expected_sequence": map[string]any{"type": "integer", "minimum": 1},
			"request_digest":    stringSchema("exact request checkpoint digest", `^sha256:[a-f0-9]{64}$`, 71),
		}, []string{"runtime_id", "action", "controller_id", "generation", "turn_id", "expected_sequence", "request_digest"})}, s.handleModelRuntimeControl)
}

func (s *Server) handleModelRuntimeControl(arguments json.RawMessage) (string, error) {
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	var request modelturn.RuntimeControlRequest
	if err := decodeClosed(arguments, &request); err != nil {
		return "", err
	}
	control, err := s.modelTurns.ControlRuntime(context.Background(), request)
	return marshalToolValue(control, err)
}
