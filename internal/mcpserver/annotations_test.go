package mcpserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var compatibilityAliases = map[string]string{
	"repo_list":           "list_dir",
	"repo_status":         "git_status",
	"repo_diff":           "git_diff",
	"source_repo_info":    "github_repo_info",
	"source_repo_create":  "github_create_repo",
	"repo_publish":        "git_push",
	"platform_apps_list":  "coolify_list_apps",
	"platform_app_status": "coolify_app_status",
	"platform_app_create": "coolify_create_app",
	"platform_deploy":     "coolify_deploy",
}

func TestToolAnnotations_AreCompleteAndSerialize(t *testing.T) {
	s := stampServer(t)
	for _, d := range s.listTools() {
		for _, key := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
			if _, ok := d.Annotations[key].(bool); !ok {
				t.Errorf("%s annotation %s must be an explicit bool, got %#v", d.Name, key, d.Annotations[key])
			}
		}
	}

	wire, err := json.Marshal(s.listTools())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
		if !strings.Contains(string(wire), `"`+key+`"`) {
			t.Errorf("tools/list JSON does not serialize %s", key)
		}
	}
}

func TestCompatibilityAliasesShareSchemaAndHandler(t *testing.T) {
	s := stampServer(t)
	for alias, legacy := range compatibilityAliases {
		a, aliasOK := s.table[alias]
		l, legacyOK := s.table[legacy]
		if !aliasOK || !legacyOK {
			t.Errorf("expected legacy %q and alias %q to exist", legacy, alias)
			continue
		}
		if !reflect.DeepEqual(a.def.InputSchema, l.def.InputSchema) {
			t.Errorf("%s schema differs from %s", alias, legacy)
		}
		if reflect.ValueOf(a.handler).Pointer() != reflect.ValueOf(l.handler).Pointer() {
			t.Errorf("%s must use the exact %s handler", alias, legacy)
		}
	}
}

func TestCompatibilityAliasesPreserveApprovalPosture(t *testing.T) {
	s, _ := newTestServer(t, "ask")

	if _, err := s.table["repo_list"].handler(json.RawMessage(`{}`)); err != nil {
		t.Fatalf("read-only alias unexpectedly required approval: %v", err)
	}

	result, err := s.table["repo_publish"].handler(json.RawMessage(`{"repo":".","branch":"main"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "APPROVAL REQUIRED") {
		t.Fatalf("write alias bypassed ask approval: %q", result)
	}
}

func TestToolAnnotationClassifications(t *testing.T) {
	s := stampServer(t)
	byName := map[string]toolDef{}
	for _, d := range s.listTools() {
		byName[d.Name] = d
	}

	assertHints := func(name string, readOnly, destructive, idempotent, openWorld bool) {
		t.Helper()
		want := map[string]any{
			"readOnlyHint": readOnly, "destructiveHint": destructive,
			"idempotentHint": idempotent, "openWorldHint": openWorld,
		}
		if got := byName[name].Annotations; !reflect.DeepEqual(got, want) {
			t.Errorf("%s annotations = %#v, want %#v", name, got, want)
		}
	}

	for _, name := range []string{"build_context_pack", "list_dir", "repo_list", "read_file", "read_many_files", "search_code", "git_status", "repo_status", "git_diff", "repo_diff", "memory_read", "sandbox_status"} {
		assertHints(name, true, false, true, false)
	}
	for _, name := range []string{"github_repo_info", "source_repo_info", "coolify_list_apps", "platform_apps_list", "coolify_app_status", "platform_app_status"} {
		assertHints(name, true, false, true, true)
	}
	for _, name := range []string{"git_push", "repo_publish", "github_create_repo", "source_repo_create", "coolify_deploy", "platform_deploy", "coolify_create_app", "platform_app_create", "coolify_set_env"} {
		assertHints(name, false, false, false, true)
	}
}
