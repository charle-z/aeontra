package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePlatformEnvironmentService struct {
	app     string
	vars    map[string]string
	approve bool
}

func (f *fakePlatformEnvironmentService) CoolifySetEnv(app string, vars map[string]string, approve bool) (string, error) {
	f.app = app
	f.vars = vars
	f.approve = approve
	return "env-result", nil
}

func TestRegisterPlatformEnvironmentDefinesStableContractAndRoutesHandler(t *testing.T) {
	service := &fakePlatformEnvironmentService{}
	var registered []Tool
	RegisterPlatformEnvironment(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "coolify_set_env" || tool.Version != "1" {
		t.Fatalf("tool = %s v%s", tool.Name, tool.Version)
	}
	wantDescription := "Set environment variables on one Coolify application. Values are sent to Coolify but redacted from output/audit. Denied in read-only; in ask mode set approve=true."
	if tool.Description != wantDescription {
		t.Fatalf("description changed: %q", tool.Description)
	}
	wantSchema := object(map[string]any{
		"app": strProp("Coolify application uuid"),
		"vars": map[string]any{
			"type":                 "object",
			"additionalProperties": map[string]any{"type": "string"},
			"description":          "environment variables to set",
		},
		"approve": boolProp("set env vars even when approval is required"),
	}, "app", "vars")
	if !reflect.DeepEqual(tool.InputSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", tool.InputSchema, wantSchema)
	}

	result, err := tool.Handler(json.RawMessage(`{"app":"app-1","vars":{"TOKEN":"secret","MODE":"prod"},"approve":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "env-result" {
		t.Fatalf("result = %q", result)
	}
	if service.app != "app-1" || !service.approve || !reflect.DeepEqual(service.vars, map[string]string{"TOKEN": "secret", "MODE": "prod"}) {
		t.Fatalf("routed app=%q vars=%#v approve=%t", service.app, service.vars, service.approve)
	}
}
