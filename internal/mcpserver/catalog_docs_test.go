package mcpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToolReferenceMatchesRegisteredCatalog(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	docPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "tools.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}

	documented := map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		nameWithRest := strings.TrimPrefix(line, "| `")
		name, _, found := strings.Cut(nameWithRest, "`")
		if !found || strings.TrimSpace(name) == "" {
			t.Fatalf("malformed tool reference row: %q", line)
		}
		documented[name]++
	}

	s := stampServer(t)
	catalog, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool, catalog.ToolCount)
	for _, tool := range catalog.Tools {
		registered[tool.Name] = true
	}

	for name, count := range documented {
		if count != 1 {
			t.Errorf("tool %q is documented %d times", name, count)
		}
		if !registered[name] {
			t.Errorf("documentation contains unregistered tool %q", name)
		}
	}
	for name := range registered {
		if documented[name] != 1 {
			t.Errorf("registered tool %q is missing from docs/tools.md", name)
		}
	}
	if len(documented) != catalog.ToolCount {
		t.Fatalf("documented tools = %d, registered catalog = %d", len(documented), catalog.ToolCount)
	}
}
