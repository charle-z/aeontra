package mcpserver

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"
)

const stdioSessionKey = "stdio"

// ClientCapabilities is the complete allowlist retained from initialize. It is
// intentionally flat so accidental expansion is visible in schema tests.
type ClientCapabilities struct {
	ClientName           string `json:"client_name"`
	ClientVersion        string `json:"client_version"`
	ProtocolVersion      string `json:"protocol_version"`
	SamplingSupported    bool   `json:"sampling_supported"`
	RootsSupported       bool   `json:"roots_supported"`
	ElicitationSupported bool   `json:"elicitation_supported"`
	ObservedAt           string `json:"observed_at"`
}

type clientCapabilityStore struct {
	mu        sync.RWMutex
	bySession map[string]ClientCapabilities
}

func newClientCapabilityStore() *clientCapabilityStore {
	return &clientCapabilityStore{bySession: make(map[string]ClientCapabilities)}
}

func (s *clientCapabilityStore) Record(sessionKey string, params json.RawMessage, observedAt time.Time) ClientCapabilities {
	if sessionKey == "" {
		sessionKey = "unknown"
	}
	capabilities := parseClientCapabilities(params, observedAt)
	s.mu.Lock()
	s.bySession[sessionKey] = capabilities
	s.mu.Unlock()
	return capabilities
}

func (s *clientCapabilityStore) Snapshot(sessionKey string) ClientCapabilities {
	s.mu.RLock()
	capabilities := s.bySession[sessionKey]
	s.mu.RUnlock()
	return capabilities
}

func (s *clientCapabilityStore) Reset() {
	s.mu.Lock()
	s.bySession = make(map[string]ClientCapabilities)
	s.mu.Unlock()
}

func parseClientCapabilities(params json.RawMessage, observedAt time.Time) ClientCapabilities {
	result := ClientCapabilities{ObservedAt: observedAt.UTC().Format(time.RFC3339Nano)}
	if len(bytes.TrimSpace(params)) == 0 {
		return result
	}
	var initialize struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(params, &initialize); err != nil {
		return result
	}
	if initialize.ClientInfo != nil {
		result.ClientName = initialize.ClientInfo.Name
		result.ClientVersion = initialize.ClientInfo.Version
	}
	result.ProtocolVersion = initialize.ProtocolVersion
	result.SamplingSupported = announcedCapability(initialize.Capabilities, "sampling")
	result.RootsSupported = announcedCapability(initialize.Capabilities, "roots")
	result.ElicitationSupported = announcedCapability(initialize.Capabilities, "elicitation")
	return result
}

func announcedCapability(capabilities map[string]json.RawMessage, name string) bool {
	raw, ok := capabilities[name]
	if !ok {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil
}
