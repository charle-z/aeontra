package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeSourceWorkflowService struct {
	calls []string
}

func (f *fakeSourceWorkflowService) SourceWorkflowDispatchPreview(repo, workflow, ref string, inputs map[string]string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo+":"+workflow+":"+ref+":"+inputs["release"])
	return "preview-result", nil
}

func (f *fakeSourceWorkflowService) SourceWorkflowDispatch(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "dispatch:"+planID)
	if approve {
		f.calls = append(f.calls, "dispatch-approved")
	}
	return "dispatch-result", nil
}

func TestRegisterSourceWorkflowsRoutesClosedBoundedHandlers(t *testing.T) {
	service := &fakeSourceWorkflowService{}
	byName := map[string]Tool{}
	RegisterSourceWorkflows(func(tool Tool) {
		byName[tool.Name] = tool
	}, service)

	wantNames := []string{"source_workflow_dispatch_preview", "source_workflow_dispatch"}
	for _, name := range wantNames {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Version != "1" || tool.Description == "" || tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("invalid contract for %s: %#v", name, tool.InputSchema)
		}
	}
	previewProps := byName["source_workflow_dispatch_preview"].InputSchema["properties"].(map[string]any)
	inputs := previewProps["inputs"].(map[string]any)
	if inputs["maxProperties"] != 25 || inputs["additionalProperties"].(map[string]any)["maxLength"] != 256 {
		t.Fatalf("inputs schema is not bounded: %#v", inputs)
	}

	preview, err := byName["source_workflow_dispatch_preview"].Handler(json.RawMessage(`{"repo":"mcp-devbox","workflow":"edge-release.yml","ref":"main","inputs":{"release":"p15.0.24"}}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
	dispatch, err := byName["source_workflow_dispatch"].Handler(json.RawMessage(`{"plan_id":"workflow-plan","approve":true}`))
	if err != nil || dispatch != "dispatch-result" {
		t.Fatalf("dispatch=%q err=%v", dispatch, err)
	}
	for _, name := range wantNames {
		if _, err := byName[name].Handler(json.RawMessage(`{"broken"`)); err == nil {
			t.Fatalf("%s accepted malformed JSON", name)
		}
	}

	wantCalls := []string{
		"preview:mcp-devbox:edge-release.yml:main:p15.0.24",
		"dispatch:workflow-plan",
		"dispatch-approved",
	}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", service.calls, wantCalls)
	}
}
