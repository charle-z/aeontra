package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeFrontDoorPlatformService struct {
	previewRequest FrontDoorPlatformPreviewRequest
	planID         string
	approve        bool
	statusCalls    int
}

func (f *fakeFrontDoorPlatformService) PlatformFrontDoorCreatePreview(request FrontDoorPlatformPreviewRequest) (string, error) {
	f.previewRequest = request
	return "front-door-preview", nil
}
func (f *fakeFrontDoorPlatformService) PlatformFrontDoorCreate(planID string, approve bool) (string, error) {
	f.planID, f.approve = planID, approve
	return "front-door-created", nil
}
func (f *fakeFrontDoorPlatformService) PlatformFrontDoorStatus() (string, error) {
	f.statusCalls++
	return "front-door-status", nil
}

func TestRegisterFrontDoorPlatformDefinesThreeNarrowTools(t *testing.T) {
	service := &fakeFrontDoorPlatformService{}
	var registered []Tool
	RegisterFrontDoorPlatform(func(tool Tool) { registered = append(registered, tool) }, service)
	if len(registered) != 3 {
		t.Fatalf("registered=%d", len(registered))
	}
	gotNames := []string{registered[0].Name, registered[1].Name, registered[2].Name}
	wantNames := []string{"platform_front_door_create_preview", "platform_front_door_create", "platform_front_door_status"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names=%v want=%v", gotNames, wantNames)
	}
	for _, tool := range registered {
		if tool.Version != "1" {
			t.Fatalf("%s version=%s", tool.Name, tool.Version)
		}
	}
	wantPreviewSchema := object(map[string]any{
		"domain":                strProp("temporary HTTPS domain restricted by COOLIFY_ALLOWED_DOMAINS"),
		"backend_url":           strProp("fixed HTTPS backend origin restricted by COOLIFY_ALLOWED_DOMAINS"),
		"expected_protocol":     strProp("exact MCP protocol date exposed by the approved backend"),
		"expected_catalog_hash": strProp("exact sha256 catalog hash exposed by the approved backend"),
	}, "domain", "backend_url", "expected_protocol", "expected_catalog_hash")
	if !reflect.DeepEqual(registered[0].InputSchema, wantPreviewSchema) {
		t.Fatalf("preview schema=%#v", registered[0].InputSchema)
	}
	wantCreateSchema := object(map[string]any{
		"plan_id": strProp("plan id returned by platform_front_door_create_preview"),
		"approve": boolProp("execute the reviewed plan when approval is required"),
	}, "plan_id")
	if !reflect.DeepEqual(registered[1].InputSchema, wantCreateSchema) {
		t.Fatalf("create schema=%#v", registered[1].InputSchema)
	}
	if !reflect.DeepEqual(registered[2].InputSchema, object(map[string]any{})) {
		t.Fatalf("status schema=%#v", registered[2].InputSchema)
	}

	result, err := registered[0].Handler(json.RawMessage(`{"domain":"https://front-door.example.com","backend_url":"https://backend.example.com","expected_protocol":"2024-11-05","expected_catalog_hash":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`))
	if err != nil || result != "front-door-preview" {
		t.Fatalf("preview result=%q err=%v", result, err)
	}
	wantRequest := FrontDoorPlatformPreviewRequest{Domain: "https://front-door.example.com", BackendURL: "https://backend.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if !reflect.DeepEqual(service.previewRequest, wantRequest) {
		t.Fatalf("request=%#v want=%#v", service.previewRequest, wantRequest)
	}
	result, err = registered[1].Handler(json.RawMessage(`{"plan_id":"plan1","approve":true}`))
	if err != nil || result != "front-door-created" || service.planID != "plan1" || !service.approve {
		t.Fatalf("create result=%q err=%v plan=%q approve=%t", result, err, service.planID, service.approve)
	}
	result, err = registered[2].Handler(json.RawMessage(`{}`))
	if err != nil || result != "front-door-status" || service.statusCalls != 1 {
		t.Fatalf("status result=%q err=%v calls=%d", result, err, service.statusCalls)
	}
}
