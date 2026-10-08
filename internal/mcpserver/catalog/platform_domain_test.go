package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakePlatformDomainService struct {
	previewApp    string
	previewDomain string
	planID        string
	approve       bool
}

func (service *fakePlatformDomainService) PlatformAppDomainUpdatePreview(app, domain string) (string, error) {
	service.previewApp, service.previewDomain = app, domain
	return "preview", nil
}

func (service *fakePlatformDomainService) PlatformAppDomainUpdate(planID string, approve bool) (string, error) {
	service.planID, service.approve = planID, approve
	return "updated", nil
}

func TestRegisterPlatformDomainDefinesClosedContracts(t *testing.T) {
	service := &fakePlatformDomainService{}
	var registered []Tool
	RegisterPlatformDomain(func(tool Tool) { registered = append(registered, tool) }, service)
	if got, want := []string{registered[0].Name, registered[1].Name}, []string{"platform_app_domain_update_preview", "platform_app_domain_update"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names=%v want=%v", got, want)
	}
	previewSchema := object(map[string]any{
		"app":    strProp("allowed Coolify application UUID"),
		"domain": strProp("single HTTPS origin restricted by COOLIFY_ALLOWED_DOMAINS"),
	}, "app", "domain")
	updateSchema := object(map[string]any{
		"plan_id": strProp("plan id returned by platform_app_domain_update_preview"),
		"approve": boolProp("execute the domain update plan when approval is required"),
	}, "plan_id")
	if !reflect.DeepEqual(registered[0].InputSchema, previewSchema) || !reflect.DeepEqual(registered[1].InputSchema, updateSchema) {
		t.Fatalf("unexpected schemas: %#v", registered)
	}
	out, err := registered[0].Handler(json.RawMessage(`{"app":"app1","domain":"https://demo.example.com"}`))
	if err != nil || out != "preview" || service.previewApp != "app1" || service.previewDomain != "https://demo.example.com" {
		t.Fatalf("preview routing out=%q err=%v service=%#v", out, err, service)
	}
	out, err = registered[1].Handler(json.RawMessage(`{"plan_id":"plan1","approve":true}`))
	if err != nil || out != "updated" || service.planID != "plan1" || !service.approve {
		t.Fatalf("update routing out=%q err=%v service=%#v", out, err, service)
	}
}
