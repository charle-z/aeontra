package catalog

import (
	"encoding/json"
	"fmt"
)

// ExecutionService is the narrow domain contract required by allowlisted and
// sandboxed execution tools.
type ExecutionService interface {
	RunCommandIn(prog string, args []string, cwd string) (string, error)
	SandboxStatus() string
	SandboxExecIn(command []string, cwd string) (string, error)
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
		Description: "Run one allowlisted program with explicit argv inside the attested private L3 executor. It is not a shell. Network is denied and the optional cwd is jailed under the workspace. Only administrator-selected allow mode enables execution; read-only and ask modes deny. Output is bounded and redacted.",
		InputSchema: object(map[string]any{
			"command": stringArray("program and arguments; command[0] is the program"),
			"cwd":     strProp("optional working directory, absolute or relative to the workspace root"),
		}, "command"),
		Version: "3",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Command []string `json:"command"`
				CWD     string   `json:"cwd"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			if len(params.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return service.RunCommandIn(params.Command[0], params.Command[1:], params.CWD)
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
		Description: "Run explicit arbitrary argv inside one selected workspace in the attested private L3 rootless sandbox. Network is denied, rootfs is read-only, only that workspace is writable, and resources/output are bounded. Set cwd to a direct repository when the configured root contains multiple repositories. Only administrator-selected allow mode enables execution; read-only and ask modes deny.",
		InputSchema: object(map[string]any{
			"command": stringArray("program and arguments; command[0] is the program"),
			"cwd":     strProp("optional working directory, absolute or relative to the workspace root"),
		}, "command"),
		Version: "4",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Command []string `json:"command"`
				CWD     string   `json:"cwd"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			if len(params.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return service.SandboxExecIn(params.Command, params.CWD)
		},
	})
}
