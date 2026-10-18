package catalog

import (
	"encoding/json"
)

const runtimeToolVersion = "1"

// RegisterRuntime registers the safe live build/catalog diagnostic tool. The
// runtimeInfo callback is supplied by mcpserver so this package never owns process
// state or security policy.
func RegisterRuntime(register Register, runtimeInfo func() (any, error)) {
	register(Tool{
		Name:        "system_runtime_info",
		Description: "Return the live non-sensitive server version, commit, protocol version, tool count, and deterministic catalog hash.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Version:     runtimeToolVersion,
		Handler: func(json.RawMessage) (string, error) {
			info, err := runtimeInfo()
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(info)
			if err != nil {
				return "", err
			}
			return string(encoded), nil
		},
	})
}
