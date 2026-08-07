package catalog

import (
	"encoding/json"
	"testing"
)

type fakeEdgeReleaseService struct{}

func (fakeEdgeReleaseService) SourceEdgeReleaseStatus() (string, error) { return "status", nil }
func (fakeEdgeReleaseService) SourceEdgeReleaseMaintenancePreview() (string, error) {
	return "preview", nil
}
func (fakeEdgeReleaseService) SourceEdgeReleaseMaintenanceApply(planID string, approve bool) (string, error) {
	return planID, nil
}

func TestRegisterSourceEdgeReleaseUsesThreeClosedTools(t *testing.T) {
	got := map[string]Tool{}
	RegisterSourceEdgeRelease(func(tool Tool) { got[tool.Name] = tool }, fakeEdgeReleaseService{})
	if len(got) != 3 {
		t.Fatalf("tool count=%d want=3", len(got))
	}
	for _, name := range []string{"source_edge_release_status", "source_edge_release_maintenance_preview", "source_edge_release_maintenance_apply"} {
		tool, ok := got[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s schema is not closed", name)
		}
	}
	if out, err := got["source_edge_release_status"].Handler(json.RawMessage(`{}`)); err != nil || out != "status" {
		t.Fatalf("status=%q err=%v", out, err)
	}
	if out, err := got["source_edge_release_maintenance_preview"].Handler(json.RawMessage(`{}`)); err != nil || out != "preview" {
		t.Fatalf("preview=%q err=%v", out, err)
	}
	if out, err := got["source_edge_release_maintenance_apply"].Handler(json.RawMessage(`{"plan_id":"plan","approve":true}`)); err != nil || out != "plan" {
		t.Fatalf("apply=%q err=%v", out, err)
	}
}
