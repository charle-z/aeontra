package catalog

import (
	"encoding/json"
	"fmt"
)

// ExecutionService is the narrow domain contract required by allowlisted and
// sandboxed execution tools.
type ExecutionService interface {
	RunCommandIn(prog string, args []string, approve bool, cwd string) (string, error)
	SandboxStatus() string
	SandboxExec(command []string, approve bool) (string, error)
}

// RegisterExecution registers the contiguous execution and sandbox tools in the
// same order used by the monolithic catalog.
func RegisterExecution(register Register, service ExecutionService) {
	stringArray := func(description string) map[string]any {
		return map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": description,
		}
	}

	register(Tool{
		Name:        "run_command",
		Description: "Run a single allowlisted program with args (e.g. [\"go\",\"vet\",\"./...\"]). NOT a shell: only allowlisted programs, no metacharacters. Optional cwd is jailed under the workspace. Mode-gated (read-only denies; ask needs approve=true). Output redacted.",
		InputSchema: object(map[string]any{
			"command": stringArray("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
			"cwd":     strProp("optional working directory, absolute or relative to the workspace root"),
		}, "command"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Command []string `json:"command"`
				Approve bool     `json:"approve"`
				CWD     string   `json:"cwd"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			if len(params.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return service.RunCommandIn(params.Command[0], params.Command[1:], params.Approve, params.CWD)
		},
	})

	register(Tool{
		Name:        "sandbox_status",
		Description: "Report L3 sandbox availability. Diagnostic only: unavailable by default, no free terminal, no Docker socket in the public MCP container.",
		InputSchema: object(map[string]any{}),
		Version:     "1",
		Handler: func(json.RawMessage) (string, error) {
			return service.SandboxStatus(), nil
		},
	})

	register(Tool{
		Name:        "sandbox_exec",
		Description: "Run an ARBITRARY command INSIDE the L3 sandbox (contained: no network, read-only rootfs, workspace-only, resource-limited). NOT allowlist-limited — the sandbox contains it. Requires a configured backend (MCP_DEVBOX_SANDBOX=docker on a host with Docker); denied in read-only; set approve=true in ask mode.",
		InputSchema: object(map[string]any{
			"command": stringArray("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
		}, "command"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Command []string `json:"command"`
				Approve bool     `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			if len(params.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return service.SandboxExec(params.Command, params.Approve)
		},
	})
}
