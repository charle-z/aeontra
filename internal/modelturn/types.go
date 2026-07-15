// Package modelturn defines the bounded boundary between an agent runtime and an
// external model. It carries canonical JSON only; provider credentials and client
// session metadata are intentionally outside this package.
package modelturn

import (
	"encoding/json"
	"time"
)

type TurnID string

type ToolDefinition struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

type ModelRequest struct {
	RuntimeID    string           `json:"runtime_id"`
	Sequence     uint64           `json:"sequence"`
	Payload      json.RawMessage  `json:"payload"`
	OfferedTools []ToolDefinition `json:"offered_tools,omitempty"`
	TTL          time.Duration    `json:"-"`
}

type Turn struct {
	RuntimeID      string    `json:"runtime_id"`
	ID             TurnID    `json:"turn_id"`
	Sequence       uint64    `json:"sequence"`
	RequestDigest  string    `json:"request_digest"`
	OfferedToolIDs []string  `json:"offered_tool_ids,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type ModelResponse struct {
	RuntimeID     string          `json:"runtime_id"`
	TurnID        TurnID          `json:"turn_id"`
	Sequence      uint64          `json:"sequence"`
	RequestDigest string          `json:"request_digest"`
	Payload       json.RawMessage `json:"payload"`
}
