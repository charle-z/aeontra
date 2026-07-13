package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePlatformAppPreviewService struct {
	requests []PlatformAppCreatePreviewRequest
}

func (f *fakePlatformAppPreviewService) PlatformAppCreatePreview(request PlatformAppCreatePreviewRequest) (string, error) {
	f.requests = append(f.requests, request)
	return "preview-result", nil
}

func TestRegisterPlatformAppPreviewDefinesStableContractAndRoutesHandler(t *testing.T) {
	service := &fakePlatformAppPreviewService{}
	var registered []Tool
	RegisterPlatformAppPreview(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "platform_app_create_preview" || tool.Version != "1" {
		t.Fatalf("tool = %s v%s", tool.Name, tool.Version)
	}
	wantDescription := "Validate a Coolify application definition against configured server/project/environment, GitHub owner and domain allowlist, then create a read-only expiring single-use plan. Required environment variable names are shown; no secret values are accepted or returned."
	if tool.Description != wantDescription {
		t.Fatalf("description changed: %q", tool.Description)
	}

	wantSchema := object(map[string]any{
		"name":                 strProp("new application name"),
		"github_repo":          strProp("owner/repo or allowed credential-free GitHub URL"),
		"branch":               strProp("branch, defaults to main"),
		"domain":               strProp("optional domain restricted by COOLIFY_ALLOWED_DOMAINS"),
		"port":                 strProp("optional exposed port from 1 to 65535"),
		"build_pack":           strProp("nixpacks, dockerfile, static, or dockercompose"),
		"healthcheck_path":     strProp("optional absolute HTTP healthcheck path"),
		"healthcheck_interval": map[string]any{"type": "integer", "description": "optional healthcheck interval in seconds"},
		"healthcheck_timeout":  map[string]any{"type": "integer", "description": "optional healthcheck timeout in seconds"},
		"required_env": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "names of required environment variables; never values",
		},
	}, "name", "github_repo")
	if !reflect.DeepEqual(tool.InputSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", tool.InputSchema, wantSchema)
	}

	result, err := tool.Handler(json.RawMessage(`{"name":"app","github_repo":"charle-z/app","branch":"main","domain":"https://app.example.com","port":"8080","build_pack":"nixpacks","healthcheck_path":"/healthz","healthcheck_interval":20,"healthcheck_timeout":4,"required_env":["TOKEN","URL"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "preview-result" {
		t.Fatalf("result = %q", result)
	}
	wantRequest := PlatformAppCreatePreviewRequest{
		Name: "app", GitHubRepo: "charle-z/app", Branch: "main", Domain: "https://app.example.com",
		Port: "8080", BuildPack: "nixpacks", HealthcheckPath: "/healthz",
		HealthcheckInterval: 20, HealthcheckTimeout: 4, RequiredEnv: []string{"TOKEN", "URL"},
	}
	if !reflect.DeepEqual(service.requests, []PlatformAppCreatePreviewRequest{wantRequest}) {
		t.Fatalf("requests = %#v, want %#v", service.requests, wantRequest)
	}
}
