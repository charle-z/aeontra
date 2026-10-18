package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func TestSystemRuntimeInfoToolReturnsSafeCatalogIdentity(t *testing.T) {
	oldCommit := buildinfo.Commit
	buildinfo.Commit = "runtime-tool-commit"
	defer func() { buildinfo.Commit = oldCommit }()

	s := stampServer(t)
	entry, ok := s.table["system_runtime_info"]
	if !ok {
		t.Fatal("system_runtime_info is not registered")
	}
	result, err := entry.handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var got RuntimeInfo
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("tool result is not RuntimeInfo JSON: %v\n%s", err, result)
	}
	if got.Commit != buildinfo.Commit || got.ToolCount != len(s.order) || !strings.HasPrefix(got.CatalogHash, "sha256:") {
		t.Fatalf("unexpected runtime info: %#v", got)
	}
	for _, forbidden := range []string{"token", "password", "secret", "/repos", "coolify", "github"} {
		if strings.Contains(strings.ToLower(result), forbidden) {
			t.Fatalf("runtime tool leaked forbidden detail %q: %s", forbidden, result)
		}
	}
}

func TestSystemRuntimeInfoToolIsReadOnlyLocal(t *testing.T) {
	s := stampServer(t)
	entry, ok := s.table["system_runtime_info"]
	if !ok {
		t.Fatal("system_runtime_info is not registered")
	}
	want := map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
	for key, expected := range want {
		if entry.def.Annotations[key] != expected {
			t.Fatalf("annotation %s = %#v, want %#v", key, entry.def.Annotations[key], expected)
		}
	}
}
