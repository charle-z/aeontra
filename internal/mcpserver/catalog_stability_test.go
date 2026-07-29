package mcpserver

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCatalogInfoRepeatedCalculationIsStable(t *testing.T) {
	s := stampServer(t)
	first, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 10; iteration++ {
		next, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		if next.Hash != first.Hash || next.ToolCount != first.ToolCount {
			t.Fatalf("catalog calculation %d changed identity: %+v != %+v", iteration, next, first)
		}
		for index := range first.Tools {
			if next.Tools[index].Name != first.Tools[index].Name {
				t.Fatalf("catalog calculation %d changed order at %d: %q != %q", iteration, index, next.Tools[index].Name, first.Tools[index].Name)
			}
		}
	}
}

func TestCatalogHashIgnoresProcessOperationalState(t *testing.T) {
	s := stampServer(t)
	baseline, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}

	s.startedAt = s.startedAt.Add(48 * time.Hour)
	s.stateRoot = "/different-runtime-state"
	s.auditPath = "/different-runtime-audit.jsonl"
	s.name = "different-process-name"

	changed, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if changed.Hash != baseline.Hash || changed.ToolCount != baseline.ToolCount {
		t.Fatalf("process-local operational state changed catalog identity: %+v != %+v", changed, baseline)
	}
}

func TestCatalogHashChangesForSchemaDefaultsAndToolMembership(t *testing.T) {
	t.Run("schema default", func(t *testing.T) {
		s := stampServer(t)
		baseline, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		entry := s.table["repo_list"]
		entry.def.InputSchema["default"] = map[string]any{"branch": "main"}
		s.table["repo_list"] = entry
		changed, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		if changed.Hash == baseline.Hash {
			t.Fatal("public schema default did not change catalog identity")
		}
	})

	t.Run("tool added", func(t *testing.T) {
		s := stampServer(t)
		baseline, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		s.table["catalog_contract_probe"] = toolEntry{def: toolDef{
			Name:        "catalog_contract_probe",
			Description: "Test-only public contract.",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
			Version:     "1",
			Annotations: map[string]any{"readOnlyHint": true},
		}, handler: func(json.RawMessage) (string, error) { return "ok", nil }}
		changed, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		if changed.Hash == baseline.Hash || changed.ToolCount != baseline.ToolCount+1 {
			t.Fatalf("added tool did not change identity and count: %+v -> %+v", baseline, changed)
		}
	})

	t.Run("tool removed", func(t *testing.T) {
		s := stampServer(t)
		baseline, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		delete(s.table, "repo_list")
		changed, err := s.CatalogInfo()
		if err != nil {
			t.Fatal(err)
		}
		if changed.Hash == baseline.Hash || changed.ToolCount != baseline.ToolCount-1 {
			t.Fatalf("removed tool did not change identity and count: %+v -> %+v", baseline, changed)
		}
	})
}

func TestToolsListIsNameSortedIndependentOfRegistrationOrder(t *testing.T) {
	s := stampServer(t)
	for left, right := 0, len(s.order)-1; left < right; left, right = left+1, right-1 {
		s.order[left], s.order[right] = s.order[right], s.order[left]
	}
	listed := s.listTools()
	if len(listed) != len(s.table) {
		t.Fatalf("listed tools=%d registry=%d", len(listed), len(s.table))
	}
	for index := 1; index < len(listed); index++ {
		if listed[index-1].Name >= listed[index].Name {
			t.Fatalf("tools/list order is not stable at %q, %q", listed[index-1].Name, listed[index].Name)
		}
	}
}
