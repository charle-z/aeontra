package catalog

import (
	"encoding/json"
	"testing"
)

type fakeAssetService struct{ called string }

func (s *fakeAssetService) AssetSearch(string, int) (string, error) {
	s.called = "search"
	return "[]", nil
}
func (s *fakeAssetService) AssetMaterializePreview(string, string, string) (string, error) {
	s.called = "preview"
	return "{}", nil
}
func (s *fakeAssetService) AssetMaterialize(string, bool) (string, error) {
	s.called = "materialize"
	return "{}", nil
}

func TestRegisterAssetsClosedContractsAndExactRouting(t *testing.T) {
	service := &fakeAssetService{}
	var registered []Tool
	RegisterAssets(func(tool Tool) { registered = append(registered, tool) }, service)
	if len(registered) != 3 {
		t.Fatalf("count=%d", len(registered))
	}
	for i, args := range []string{`{"query":"logo","limit":5}`, `{"asset_id":"logo","repo":"app","path":"logo.png"}`, `{"plan_id":"opaque","approve":true}`} {
		tool := registered[i]
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s open schema", tool.Name)
		}
		if _, err := tool.Handler(json.RawMessage(args)); err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Handler(json.RawMessage(`{"url":"https://evil.example.com/","headers":{}}`)); err == nil {
			t.Fatalf("%s accepted URL/header surface", tool.Name)
		}
	}
	if service.called != "materialize" {
		t.Fatalf("route=%s", service.called)
	}
}
