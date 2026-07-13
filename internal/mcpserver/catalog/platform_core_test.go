package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePlatformCoreService struct{ calls []string }

func (f *fakePlatformCoreService) PlatformDeploy(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "deploy:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "deploy-result", nil
}
func (f *fakePlatformCoreService) PlatformAppsList() (string, error) {
	f.calls = append(f.calls, "list")
	return "list-result", nil
}
func (f *fakePlatformCoreService) PlatformAppStatus(app string) (string, error) {
	f.calls = append(f.calls, "status:"+app)
	return "status-result", nil
}
func (f *fakePlatformCoreService) PlatformDeploymentStatus(deployment string) (string, error) {
	f.calls = append(f.calls, "deployment:"+deployment)
	return "deployment-result", nil
}
func (f *fakePlatformCoreService) PlatformAppLogs(app string, lines int) (string, error) {
	f.calls = append(f.calls, "logs:"+app)
	if lines == 25 {
		f.calls = append(f.calls, "lines-25")
	}
	return "logs-result", nil
}
func (f *fakePlatformCoreService) PlatformAppCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "create:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "create-result", nil
}

func TestRegisterPlatformCoreDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakePlatformCoreService{}
	var registered []Tool
	RegisterPlatformCore(func(tool Tool) { registered = append(registered, tool) }, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"coolify_deploy", "coolify_list_apps", "coolify_app_status", "coolify_deployment_status", "coolify_app_logs", "coolify_create_app"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"coolify_deploy":            "Execute one previously reviewed platform_deploy_preview plan after revalidating the application repository, branch and expected commit. The plan is expiring and single-use; requires approval in ask mode; token is never exposed.",
		"coolify_list_apps":         "List applications on the configured Coolify instance. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set. Token is never exposed.",
		"coolify_app_status":        "Read one Coolify application by uuid. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced when configured.",
		"coolify_deployment_status": "Read one Coolify deployment by UUID and return a safe summary containing status, commit, timestamps, and application name. Token is never exposed.",
		"coolify_app_logs":          "Read the latest bounded application logs from Coolify. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced and secrets are redacted.",
		"coolify_create_app":        "Execute one previously reviewed platform_app_create_preview plan using the configured server/project/environment. Repository owner, domain, build, port and healthcheck were validated; plan is expiring and single-use; requires approval in ask mode.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}
	wantSchemas := map[string]map[string]any{
		"coolify_deploy": object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_preview"),
			"approve": boolProp("execute the deployment plan when approval is required"),
		}, "plan_id"),
		"coolify_list_apps":         object(map[string]any{}),
		"coolify_app_status":        object(map[string]any{"app": strProp("Coolify application uuid")}, "app"),
		"coolify_deployment_status": object(map[string]any{"deployment": strProp("Coolify deployment UUID")}, "deployment"),
		"coolify_app_logs": object(map[string]any{
			"app":   strProp("Coolify application uuid"),
			"lines": map[string]any{"type": "integer", "description": "number of log lines from the end, from 1 to 1000; defaults to 100"},
		}, "app"),
		"coolify_create_app": object(map[string]any{
			"plan_id": strProp("plan id returned by platform_app_create_preview"),
			"approve": boolProp("execute the application creation plan when approval is required"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	calls := []struct{ name, args, want string }{
		{"coolify_deploy", `{"plan_id":"deploy-1","approve":true}`, "deploy-result"},
		{"coolify_list_apps", `{}`, "list-result"},
		{"coolify_app_status", `{"app":"app1"}`, "status-result"},
		{"coolify_deployment_status", `{"deployment":"dep1"}`, "deployment-result"},
		{"coolify_app_logs", `{"app":"app1","lines":25}`, "logs-result"},
		{"coolify_create_app", `{"plan_id":"create-1","approve":true}`, "create-result"},
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
	wantCalls := []string{"deploy:deploy-1", "approved", "list", "status:app1", "deployment:dep1", "logs:app1", "lines-25", "create:create-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
