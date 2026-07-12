package mcpserver

import (
	"strings"
	"testing"
)

func TestCatalogInfoIsDeterministicAndComplete(t *testing.T) {
	s := stampServer(t)
	first, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if first.ToolCount != len(s.order) || first.ToolCount == 0 {
		t.Fatalf("tool count = %d, registry = %d", first.ToolCount, len(s.order))
	}
	if !strings.HasPrefix(first.Hash, "sha256:") || len(strings.TrimPrefix(first.Hash, "sha256:")) != 64 {
		t.Fatalf("catalog hash has unexpected format: %q", first.Hash)
	}
	if len(first.Tools) != first.ToolCount {
		t.Fatalf("manifest tools = %d, count = %d", len(first.Tools), first.ToolCount)
	}
	for i, tool := range first.Tools {
		if strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Version) == "" {
			t.Fatalf("manifest tool %d lacks name/version: %#v", i, tool)
		}
		if i > 0 && first.Tools[i-1].Name >= tool.Name {
			t.Fatalf("manifest is not strictly name-sorted at %q, %q", first.Tools[i-1].Name, tool.Name)
		}
	}

	for left, right := 0, len(s.order)-1; left < right; left, right = left+1, right-1 {
		s.order[left], s.order[right] = s.order[right], s.order[left]
	}
	second, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if second.Hash != first.Hash {
		t.Fatalf("catalog hash depends on registration order: %q != %q", second.Hash, first.Hash)
	}
}

func TestCatalogHashChangesWithWireContract(t *testing.T) {
	s := stampServer(t)
	baseline, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}

	entry := s.table["repo_list"]
	entry.def.Version = "2"
	s.table["repo_list"] = entry
	changed, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if changed.Hash == baseline.Hash {
		t.Fatal("catalog hash did not change after tool contract version changed")
	}
}

func TestCatalogHashIgnoresDescriptionsAndHandlers(t *testing.T) {
	s := stampServer(t)
	baseline, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}

	entry := s.table["repo_list"]
	entry.def.Description = "documentation-only change"
	s.table["repo_list"] = entry
	changed, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if changed.Hash != baseline.Hash {
		t.Fatal("catalog hash changed for a description-only edit")
	}
}
