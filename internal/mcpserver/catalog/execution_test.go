package catalog

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type fakeExecutionService struct{ calls []string }

func (f *fakeExecutionService) RunCommandIn(prog string, args []string, approve bool, cwd string) (string, error) {
	f.calls = append(f.calls, "command:"+cwd+":"+prog+":"+strings.Join(args, ","))
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "command-result", nil
}
func (f *fakeExecutionService) SandboxStatus() string {
	f.calls = append(f.calls, "status")
	return "status-result"
}
func (f *fakeExecutionService) SandboxExec(command []string, approve bool) (string, error) {
	f.calls = append(f.calls, "sandbox:"+strings.Join(command, ","))
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "sandbox-result", nil
}

func TestRegisterExecutionDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeExecutionService{}
	var registered []Tool
	RegisterExecution(func(tool Tool) { registered = append(registered, tool) }, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"run_command", "sandbox_status", "sandbox_exec"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"run_command":    "Run a single allowlisted program with args (e.g. [\"go\",\"vet\",\"./...\"]). NOT a shell: only allowlisted programs, no metacharacters. Optional cwd is jailed under the workspace. Mode-gated (read-only denies; ask needs approve=true). Output redacted.",
		"sandbox_status": "Attest and report the private rootless L3 executor. It remains unavailable on endpoint, image, rootless or profile drift; the public MCP has no container-engine socket.",
		"sandbox_exec":   "Run explicit arbitrary argv inside the attested private L3 rootless sandbox. Network is denied, rootfs is read-only, only the registered workspace is writable, and resources/output are bounded. L1 command allowlists do not apply. Denied in read-only; approve=true is required only in ask mode.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}
	array := func(description string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
	}
	wantSchemas := map[string]map[string]any{
		"run_command": object(map[string]any{
			"command": array("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
			"cwd":     strProp("optional working directory, absolute or relative to the workspace root"),
		}, "command"),
		"sandbox_status": object(map[string]any{}),
		"sandbox_exec": object(map[string]any{
			"command": array("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
		}, "command"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	calls := []struct{ name, args, want string }{
		{"run_command", `{"command":["go","vet","./..."],"approve":true,"cwd":"project"}`, "command-result"},
		{"sandbox_status", `{}`, "status-result"},
		{"sandbox_exec", `{"command":["sh","-lc","echo ok"],"approve":true}`, "sandbox-result"},
	}
	for _, call := range calls {
		got, err := byName[call.name].Handler(json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if got != call.want {
			t.Fatalf("%s result = %q", call.name, got)
		}
	}
	wantCalls := []string{"command:project:go:vet,./...", "approved", "status", "sandbox:sh,-lc,echo ok", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}

	if _, err := byName["run_command"].Handler(json.RawMessage(`{"command":[]}`)); err == nil {
		t.Fatal("empty run_command should fail")
	}
	if _, err := byName["sandbox_exec"].Handler(json.RawMessage(`{"command":[]}`)); err == nil {
		t.Fatal("empty sandbox_exec should fail")
	}
}
