package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePrivilegedService struct{ calls []string }

func (f *fakePrivilegedService) PrivilegedTaskPreview(repo, profile string, params map[string]string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo+":"+profile+":"+params["service"])
	return "preview-result", nil
}
func (f *fakePrivilegedService) PrivilegedTaskExecute(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "execute:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "execute-result", nil
}

func TestRegisterPrivilegedDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakePrivilegedService{}
	var registered []Tool
	RegisterPrivileged(func(tool Tool) { registered = append(registered, tool) }, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"privileged_task_preview", "privileged_task_execute"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"privileged_task_preview": "Preview one administrator-enabled, server-defined privileged profile. The client supplies only a profile name and narrow validated parameters, never an executable, argv, or shell string. Returns the exact command, jailed working directory, network/filesystem posture, effect, risk, short-lived plan id and expiry. Disabled by default.",
		"privileged_task_execute": "Execute one unexpired unused privileged_task_preview plan after policy approval. The exact server-generated command, jailed cwd, timeout and profile remain fixed. Docker profiles fail securely when safe containment is unavailable; no free host terminal is exposed.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}
	wantSchemas := map[string]map[string]any{
		"privileged_task_preview": object(map[string]any{
			"repo":    strProp("jailed repository directory when the selected profile applies to a repository"),
			"profile": strProp("one approved server-defined profile name"),
			"params": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "narrow profile parameters such as remote, branch, or allowlisted service name",
			},
		}, "profile"),
		"privileged_task_execute": object(map[string]any{
			"plan_id": strProp("plan id returned by privileged_task_preview"),
			"approve": boolProp("execute the privileged profile when approval is required"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	preview, err := byName["privileged_task_preview"].Handler(json.RawMessage(`{"repo":"project","profile":"restart-approved-service","params":{"service":"api"}}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	execute, err := byName["privileged_task_execute"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || execute != "execute-result" {
		t.Fatalf("execute = %q, err=%v", execute, err)
	}
	wantCalls := []string{"preview:project:restart-approved-service:api", "execute:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
