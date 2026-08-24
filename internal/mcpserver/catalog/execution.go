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
		Description: "Attest and report the private rootless L3 executor. It remains unavailable on endpoint, image, rootless or profile drift; the public MCP has no container-engine socket.",
		InputSchema: object(map[string]any{}),
		Version:     "1",
		Handler: func(json.RawMessage) (string, error) {
			return service.SandboxStatus(), nil
		},
	})

	register(Tool{
		Name:        "sandbox_exec",
		Description: "Run explicit arbitrary argv inside the attested private L3 rootless sandbox. Network is denied, rootfs is read-only, only the registered workspace is writable, and resources/output are bounded. L1 command allowlists do not apply. Denied in read-only; approve=true is required only in ask mode.",
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
