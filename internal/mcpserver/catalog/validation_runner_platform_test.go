package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeValidationRunnerPlatformService struct{ calls []string }

func (f *fakeValidationRunnerPlatformService) PlatformValidationRunnerCreatePreview(branch string) (string, error) {
	f.calls = append(f.calls, "preview:"+branch)
	return "preview-result", nil
}
func (f *fakeValidationRunnerPlatformService) PlatformValidationRunnerCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "create:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "create-result", nil
}

func TestRegisterValidationRunnerPlatformDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeValidationRunnerPlatformService{}
	var registered []Tool
	RegisterValidationRunnerPlatform(func(tool Tool) { registered = append(registered, tool) }, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		wantVersion := "1"
		if tool.Name == "platform_validation_runner_create_preview" {
			wantVersion = "2"
		}
		if tool.Version != wantVersion {
			t.Fatalf("tool %s version = %q, want %q", tool.Name, tool.Version, wantVersion)
		}
	}
	wantNames := []string{"platform_validation_runner_create_preview", "platform_validation_runner_create"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"platform_validation_runner_create_preview": "Plan exactly one private Coolify validation-runner application using the administrator-configured destination and exact mount allowlist. It never deploys or accepts secret values.",
		"platform_validation_runner_create":         "Execute one reviewed validation-runner creation plan. It creates one private, non-deployed Coolify application and configures only non-secret runtime variables; explicit approval is required.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}
	wantSchemas := map[string]map[string]any{
		"platform_validation_runner_create_preview": object(map[string]any{
			"branch": strProp("source branch; defaults to main"),
		}),
		"platform_validation_runner_create": object(map[string]any{
			"plan_id": strProp("plan id returned by platform_validation_runner_create_preview"),
			"approve": boolProp("execute the reviewed application creation plan"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	preview, err := byName["platform_validation_runner_create_preview"].Handler(json.RawMessage(`{"branch":"main"}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	create, err := byName["platform_validation_runner_create"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || create != "create-result" {
		t.Fatalf("create = %q, err=%v", create, err)
	}
	wantCalls := []string{"preview:main", "create:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
