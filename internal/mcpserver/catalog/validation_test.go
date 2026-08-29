package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeValidationService struct {
	calls []string
}

func (f *fakeValidationService) RunTestsIn(cwd string, extra ...string) (string, error) {
	f.calls = append(f.calls, "tests:"+cwd)
	f.calls = append(f.calls, extra...)
	return "tests-result", nil
}

func (f *fakeValidationService) ValidationPreview(repo, profile string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo+":"+profile)
	return "preview-result", nil
}

func (f *fakeValidationService) ValidationExecute(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "execute:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "execute-result", nil
}

func TestRegisterValidationDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeValidationService{}
	var registered []Tool
	RegisterValidation(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		wantVersion := "1"
		if tool.Name == "run_tests" {
			wantVersion = "3"
		}
		if tool.Version != wantVersion {
			t.Fatalf("tool %s version = %q, want %s", tool.Name, tool.Version, wantVersion)
		}
	}
	wantNames := []string{"run_tests", "project_validation_preview", "project_validation_execute"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"run_tests":                  "Run the project's configured allowlisted test command inside the attested private L3 executor. Network is denied and the optional cwd is jailed under the workspace. Only administrator-selected allow mode enables execution; read-only and ask modes deny.",
		"project_validation_preview": "Preview one fixed Node/pnpm validation profile for a direct child repository. Profiles are pnpm-lockfile (generate lockfile and fetch, no lifecycle scripts) and pnpm-validate (offline frozen install, check, test, build). The public MCP never receives Docker access, shell input, or arbitrary command arguments.",
		"project_validation_execute": "Execute one unexpired project_validation_preview plan in the separately deployed private validation runner. The runner accepts only the reviewed profile and repo, starts a hardened ephemeral Node 22 container, and returns redacted bounded output. It is never a free terminal.",
	}
	for _, tool := range registered {
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed: %q", tool.Name, tool.Description)
		}
	}

	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}

	wantSchemas := map[string]map[string]any{
		"run_tests": object(map[string]any{
			"extra": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "extra arguments appended to the test command"},
			"cwd":   strProp("optional working directory, absolute or relative to the workspace root"),
		}),
		"project_validation_preview": object(map[string]any{
			"repo":    strProp("direct repository name under /repos"),
			"profile": strProp("one fixed profile: pnpm-lockfile or pnpm-validate"),
		}, "repo", "profile"),
		"project_validation_execute": object(map[string]any{
			"plan_id": strProp("plan id returned by project_validation_preview"),
			"approve": boolProp("execute the reviewed validation plan when approval is required"),
		}, "plan_id"),
	}
	for name, wantSchema := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, wantSchema) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, wantSchema)
		}
	}

	calls := []struct {
		name string
		args string
		want string
	}{
		{name: "run_tests", args: `{"extra":["-run","TestOne"],"cwd":"/repos/project"}`, want: "tests-result"},
		{name: "project_validation_preview", args: `{"repo":"project","profile":"pnpm-validate"}`, want: "preview-result"},
		{name: "project_validation_execute", args: `{"plan_id":"plan-1","approve":true}`, want: "execute-result"},
	}
	for _, call := range calls {
		result, err := byName[call.name].Handler(json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if result != call.want {
			t.Fatalf("%s result = %q, want %q", call.name, result, call.want)
		}
	}

	wantCalls := []string{
		"tests:/repos/project", "-run", "TestOne",
		"preview:project:pnpm-validate",
		"execute:plan-1", "approved",
	}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("service calls = %#v, want %#v", service.calls, wantCalls)
	}
}
