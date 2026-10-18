package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePlatformDeploymentService struct {
	calls []string
}

func (f *fakePlatformDeploymentService) PlatformDeployPreview(app string) (string, error) {
	f.calls = append(f.calls, "preview:"+app)
	return "preview-result", nil
}

func (f *fakePlatformDeploymentService) PlatformDeployWithoutCachePreview(app string) (string, error) {
	f.calls = append(f.calls, "force-preview:"+app)
	return "force-preview-result", nil
}

func (f *fakePlatformDeploymentService) PlatformDeployWithoutCache(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "force:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "force-result", nil
}

func TestRegisterPlatformDeploymentDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakePlatformDeploymentService{}
	var registered []Tool
	RegisterPlatformDeployment(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"platform_deploy_preview", "platform_deploy_without_cache_preview", "platform_deploy_without_cache"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"platform_deploy_preview":               "Read one allowed Coolify application and create an expiring single-use deployment plan bound to its repository, branch and expected commit. It does not deploy.",
		"platform_deploy_without_cache_preview": "Read one allowed Coolify application and create an expiring single-use force=true deployment plan bound to its repository, branch, and expected commit. It does not deploy.",
		"platform_deploy_without_cache":         "Execute one reviewed platform_deploy_without_cache_preview plan after revalidating the application repository, branch, and expected commit. It requests Coolify force=true and requires explicit approval in ask mode.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"platform_deploy_preview": object(map[string]any{
			"app": strProp("Coolify application UUID"),
		}, "app"),
		"platform_deploy_without_cache_preview": object(map[string]any{
			"app": strProp("Coolify application UUID"),
		}, "app"),
		"platform_deploy_without_cache": object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_without_cache_preview"),
			"approve": boolProp("execute the force=true deployment plan when approval is required"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	calls := []struct {
		name string
		args string
		want string
	}{
		{name: "platform_deploy_preview", args: `{"app":"app-1"}`, want: "preview-result"},
		{name: "platform_deploy_without_cache_preview", args: `{"app":"app-1"}`, want: "force-preview-result"},
		{name: "platform_deploy_without_cache", args: `{"plan_id":"plan-1","approve":true}`, want: "force-result"},
	}
	for _, call := range calls {
		got, err := byName[call.name].Handler(json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if got != call.want {
			t.Fatalf("%s result = %q, want %q", call.name, got, call.want)
		}
	}

	wantCalls := []string{"preview:app-1", "force-preview:app-1", "force:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
