package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

var p8ToolOrder = []string{
	"system_runtime_info", "build_context_pack", "list_dir", "read_file", "read_many_files", "search_code",
	"apply_patch", "create_file", "run_command", "sandbox_status", "sandbox_exec", "privileged_task_preview",
	"privileged_task_execute", "coolify_deploy", "coolify_list_apps", "coolify_app_status",
	"coolify_deployment_status", "coolify_app_logs", "coolify_create_app",
	"platform_validation_runner_create_preview", "platform_validation_runner_create", "platform_app_create_preview",
	"platform_deploy_preview", "platform_deploy_without_cache_preview", "platform_deploy_without_cache",
	"coolify_set_env", "git_status", "git_diff", "git_clone", "repo_fetch", "repo_fast_forward_preview",
	"repo_fast_forward", "git_push", "repo_publish_preview", "github_create_repo", "source_repo_create_preview",
	"github_repo_info", "repo_remote_preview", "repo_remote_set", "run_tests", "project_validation_preview",
	"project_validation_execute", "git_commit", "memory_read", "memory_write", "notes_list", "notes_read",
	"notes_write_preview", "notes_write", "memory_update_handoff", "repo_list", "repo_status", "repo_diff",
	"source_repo_info", "source_repo_create", "repo_publish", "platform_apps_list", "platform_app_status",
	"platform_app_logs", "platform_deployment_status", "platform_app_create", "platform_deploy",
}

var brainToolOrder = []string{"brain_search", "brain_read", "brain_write", "brain_index", "brain_context"}

func isP15Control(name string) bool {
	return strings.HasPrefix(name, "workspace_lab_") || strings.HasPrefix(name, "workspace_autopilot_") || strings.HasPrefix(name, "edge_bundle_") || name == "edge_repair" || name == "edge_onboarding_status"
}

func isP16Project(name string) bool {
	return name == "project_prepare" || name == "project_status" || name == "project_snapshot" || name == "project_exec" || strings.HasPrefix(name, "project_network_") || strings.HasPrefix(name, "project_process_") || strings.HasPrefix(name, "project_git_") || strings.HasPrefix(name, "project_github_") || strings.HasPrefix(name, "project_toolbox_") || strings.HasPrefix(name, "project_browser_") || strings.HasPrefix(name, "project_task_") || strings.HasPrefix(name, "edge_operation_")
}

func isFrontDoorPlatform(name string) bool {
	return strings.HasPrefix(name, "platform_front_door_")
}

func isPlatformDomain(name string) bool {
	return strings.HasPrefix(name, "platform_app_domain_")
}

func isPublicOSS(name string) bool {
	return strings.HasPrefix(name, "source_public_") || strings.HasPrefix(name, "source_cross_repo_")
}

func isEdgeReleaseSource(name string) bool {
	return strings.HasPrefix(name, "source_edge_release_")
}

func TestWorkspaceCheckpointTracksCatalogIdentityAfterValidationRunnerV2(t *testing.T) {
	server := stampServer(t)
	if len(server.order) != 176 {
		t.Fatalf("tool order length=%d want=176", len(server.order))
	}
	if server.order[73] != "workspace_checkpoint" {
		t.Fatalf("workspace checkpoint position=%v", server.order[:73])
	}
	if !reflect.DeepEqual(server.order[78:81], []string{"result_read", "result_find", "result_stage"}) {
		t.Fatalf("result tool position=%v", server.order[78:81])
	}
	historical := make([]string, 0, len(p8ToolOrder))
	for _, name := range server.order {
		if name != "mcp_client_capabilities" && !strings.HasPrefix(name, "model_") && !strings.HasPrefix(name, "opencode_") && !strings.HasPrefix(name, "codex_") && name != "workspace_checkpoint" && name != "workspace_runtime_continue" && !strings.HasPrefix(name, "workspace_htb_") && !isP15Control(name) && !isP16Project(name) && !strings.HasPrefix(name, "result_") && !strings.HasPrefix(name, "brain_") && !strings.HasPrefix(name, "source_pull_request_") && !strings.HasPrefix(name, "source_default_branch_") && !strings.HasPrefix(name, "source_workflow_") && !isEdgeReleaseSource(name) && !isFrontDoorPlatform(name) && !isPlatformDomain(name) && !isPublicOSS(name) {
			historical = append(historical, name)
		}
	}
	if !reflect.DeepEqual(historical, p8ToolOrder) {
		t.Fatalf("P8 compatibility tool order changed\ngot=%v\nwant=%v", historical, p8ToolOrder)
	}
	if !reflect.DeepEqual(server.order[171:], brainToolOrder) {
		t.Fatalf("Brain suffix=%v want=%v", server.order[171:], brainToolOrder)
	}

	// Compatibility slices below are rebuilt from current tool contracts. Dated
	// historical identities remain immutable evidence under docs/baselines.
	snapshot, err := server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	legacy := make([]CatalogTool, 0, 62)
	for _, tool := range snapshot.Tools {
		if tool.Name != "mcp_client_capabilities" && !strings.HasPrefix(tool.Name, "model_") && !strings.HasPrefix(tool.Name, "opencode_") && !strings.HasPrefix(tool.Name, "codex_") && !strings.HasPrefix(tool.Name, "workspace_htb_") && !isP15Control(tool.Name) && !isP16Project(tool.Name) && !strings.HasPrefix(tool.Name, "brain_") && !strings.HasPrefix(tool.Name, "result_") && !strings.HasPrefix(tool.Name, "source_pull_request_") && !strings.HasPrefix(tool.Name, "source_default_branch_") && !strings.HasPrefix(tool.Name, "source_workflow_") && !isEdgeReleaseSource(tool.Name) && !isFrontDoorPlatform(tool.Name) && !isPlatformDomain(tool.Name) && !isPublicOSS(tool.Name) && tool.Name != "workspace_checkpoint" && tool.Name != "workspace_runtime_continue" {
			legacy = append(legacy, tool)
		}
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	legacyHash := "sha256:" + hex.EncodeToString(sum[:])
	const p8CurrentContractHash = "sha256:b9fe7d2b9e291e80618459baa1ac7d1099809861b96ac48706a7dfb38b6701cd"
	if len(legacy) != 62 || legacyHash != p8CurrentContractHash {
		t.Fatalf("P8 compatibility catalog changed: count=%d hash=%s", len(legacy), legacyHash)
	}
	previous := make([]CatalogTool, 0, 71)
	for _, tool := range snapshot.Tools {
		if tool.Name != "mcp_client_capabilities" && !strings.HasPrefix(tool.Name, "model_") && !strings.HasPrefix(tool.Name, "opencode_") && !strings.HasPrefix(tool.Name, "codex_") && !strings.HasPrefix(tool.Name, "workspace_htb_") && !isP15Control(tool.Name) && !isP16Project(tool.Name) && !strings.HasPrefix(tool.Name, "source_pull_request_") && !strings.HasPrefix(tool.Name, "source_default_branch_") && !strings.HasPrefix(tool.Name, "source_workflow_") && !isEdgeReleaseSource(tool.Name) && !isFrontDoorPlatform(tool.Name) && !isPlatformDomain(tool.Name) && !isPublicOSS(tool.Name) && tool.Name != "workspace_runtime_continue" {
			previous = append(previous, tool)
		}
	}
	previousEncoded, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	previousSum := sha256.Sum256(previousEncoded)
	previousHash := "sha256:" + hex.EncodeToString(previousSum[:])
	const p11CurrentContractHash = "sha256:7ec8ad55c5c109eb48e1423b3eeaa99e5f1a052974be955602afe688d50648b9"
	if len(previous) != 71 || previousHash != p11CurrentContractHash {
		t.Fatalf("P11 compatibility catalog changed: count=%d hash=%s", len(previous), previousHash)
	}
	step1 := make([]CatalogTool, 0, 72)
	for _, tool := range snapshot.Tools {
		if !strings.HasPrefix(tool.Name, "model_") && !strings.HasPrefix(tool.Name, "opencode_") && !strings.HasPrefix(tool.Name, "codex_") && !strings.HasPrefix(tool.Name, "workspace_htb_") && !isP15Control(tool.Name) && !isP16Project(tool.Name) && !strings.HasPrefix(tool.Name, "source_pull_request_") && !strings.HasPrefix(tool.Name, "source_default_branch_") && !strings.HasPrefix(tool.Name, "source_workflow_") && !isEdgeReleaseSource(tool.Name) && !isFrontDoorPlatform(tool.Name) && !isPlatformDomain(tool.Name) && !isPublicOSS(tool.Name) && tool.Name != "workspace_runtime_continue" {
			step1 = append(step1, tool)
		}
	}
	step1Encoded, err := json.Marshal(step1)
	if err != nil {
		t.Fatal(err)
	}
	step1Sum := sha256.Sum256(step1Encoded)
	step1ComputedHash := "sha256:" + hex.EncodeToString(step1Sum[:])

	const step1Hash = "sha256:832a4591fbaf9a0dfb918db99b5e011b59b154c8749e942063283d64e9333c3a"
	if len(step1) != 72 || step1ComputedHash != step1Hash {
		t.Fatalf("Step 1 catalog identity changed: count=%d hash=%s", len(step1), step1ComputedHash)
	}
	step4 := make([]CatalogTool, 0, 77)
	for _, tool := range snapshot.Tools {
		if strings.HasPrefix(tool.Name, "source_pull_request_") || strings.HasPrefix(tool.Name, "source_default_branch_") || strings.HasPrefix(tool.Name, "source_workflow_") || isEdgeReleaseSource(tool.Name) || isFrontDoorPlatform(tool.Name) || isPlatformDomain(tool.Name) || isPublicOSS(tool.Name) || strings.HasPrefix(tool.Name, "workspace_htb_") || isP15Control(tool.Name) || isP16Project(tool.Name) {
			continue
		}
		if tool.Name == "workspace_runtime_continue" {
			continue
		}
		if tool.Name == "opencode_runtime_start" {
			continue
		}
		if tool.Name == "codex_runtime_start" {
			continue
		}
		if tool.Name == "model_runtime_status" || tool.Name == "model_runtime_cancel" {
			tool.Version = "1"
		}
		if tool.Name == "model_runtime_cancel" {
			tool.Annotations = map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false}
		}
		step4 = append(step4, tool)
	}
	step4Encoded, err := json.Marshal(step4)
	if err != nil {
		t.Fatal(err)
	}
	step4Sum := sha256.Sum256(step4Encoded)
	step4ComputedHash := "sha256:" + hex.EncodeToString(step4Sum[:])
	const step4Hash = "sha256:afd9b609e6367ccc82bf96448882ebb5a81ed65ef2f38e9aaaf3bfc09b90702e"
	if len(step4) != 77 || step4ComputedHash != step4Hash {
		t.Fatalf("Step 4 compatibility catalog changed: count=%d hash=%s", len(step4), step4ComputedHash)
	}
	if snapshot.ToolCount != 176 || snapshot.Hash != "sha256:2af5006e3525331ca52ec495cdaf853ee7e67db34437c5191853af3bc25605e7" {
		t.Fatalf("Step 6 catalog identity changed: count=%d hash=%s", snapshot.ToolCount, snapshot.Hash)
	}
}

func TestBrainToolContractsAreClosedBoundedAndAnnotated(t *testing.T) {
	server := stampServer(t)
	readHints := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	writeHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	indexHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	for _, name := range brainToolOrder {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if entry.def.Version != "1" {
			t.Fatalf("%s version=%q", name, entry.def.Version)
		}
		if entry.def.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s schema is not closed: %#v", name, entry.def.InputSchema)
		}
		wantHints := readHints
		if name == "brain_write" {
			wantHints = writeHints
		}
		if name == "brain_index" {
			wantHints = indexHints
		}
		if !reflect.DeepEqual(entry.def.Annotations, wantHints) {
			t.Fatalf("%s annotations=%#v want=%#v", name, entry.def.Annotations, wantHints)
		}
	}

	searchProps := server.table["brain_search"].def.InputSchema["properties"].(map[string]any)
	if searchProps["query"].(map[string]any)["maxLength"] != float64(brainpkg.MaxQueryBytes) && searchProps["query"].(map[string]any)["maxLength"] != brainpkg.MaxQueryBytes {
		t.Fatalf("search query bound=%#v", searchProps["query"])
	}
	if searchProps["top_k"].(map[string]any)["maximum"] != brainpkg.MaxTopK {
		t.Fatalf("top_k bound=%#v", searchProps["top_k"])
	}
	writeProps := server.table["brain_write"].def.InputSchema["properties"].(map[string]any)
	for _, forbidden := range []string{"path", "collection", "trust", "created", "updated", "approve"} {
		if _, ok := writeProps[forbidden]; ok {
			t.Fatalf("brain_write exposes forbidden property %q", forbidden)
		}
	}
	if writeProps["body"].(map[string]any)["maxLength"] != brainpkg.MaxBodyBytes {
		t.Fatalf("body bound=%#v", writeProps["body"])
	}
}

func TestBrainToolsFailClosedWhenStoreIsNotConfigured(t *testing.T) {
	server, _ := newTestServer(t, config.ModeReadOnly)
	calls := map[string]string{
		"brain_search":  `{"query":"test"}`,
		"brain_read":    `{"slug":"safe-note"}`,
		"brain_write":   `{"slug":"safe-note","title":"Safe","type":"note","author":"agent:test","provenance":"test","review_by":"2026-08-13","body":"body"}`,
		"brain_index":   `{"action":"status"}`,
		"brain_context": `{}`,
	}
	for name, arguments := range calls {
		response := call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+arguments+`}}`)
		var result toolResult
		encoded, _ := json.Marshal(response.Result)
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if !result.IsError || len(result.Content) != 1 || result.Content[0].Text != "brain is not configured" {
			t.Fatalf("%s result=%s", name, encoded)
		}
	}
}

func TestBrainToolsRejectUnknownArgumentsBeforeCapability(t *testing.T) {
	server, _ := newTestServer(t, config.ModeReadOnly)
	response := call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_search","arguments":{"query":"test","path":"/private"}}}`)
	var result toolResult
	encoded, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "unknown field") || strings.Contains(result.Content[0].Text, "/private") {
		t.Fatalf("strict decode result=%s", encoded)
	}
}

func newConfiguredBrainServer(t *testing.T) (*Server, *brainpkg.Store) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{repoRoot}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git", "go"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), repoRoot)
	brainRoot := filepath.Join(t.TempDir(), "brain")
	store, err := brainpkg.OpenStore(brainRoot, time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.WithBrainStore(store)
	t.Cleanup(func() { _ = service.BrainCapability.Close() })
	return New(service), store
}

func toolText(t *testing.T, response rpcResponse) string {
	t.Helper()
	var result toolResult
	encoded, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("tool result=%s", encoded)
	}
	return result.Content[0].Text
}

func TestBrainToolsExecuteBoundedWorkflow(t *testing.T) {
	server, _ := newConfiguredBrainServer(t)
	write := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_write","arguments":{"slug":"release-observation","title":"Release observation","type":"note","author":"agent:chatgpt","provenance":"owner instruction","review_by":"2026-08-13","body":"Staticcheck and race gates are required."}}}`))
	if !strings.Contains(write, `"slug":"release-observation"`) || strings.Contains(write, "Metadata") {
		t.Fatalf("write output=%s", write)
	}
	search := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"brain_search","arguments":{"query":"staticcheck race","top_k":5}}}`))
	if !strings.Contains(search, `"slug":"release-observation"`) {
		t.Fatalf("search output=%s", search)
	}
	read := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"brain_read","arguments":{"slug":"release-observation"}}}`))
	if !strings.Contains(read, `"backlinks":[]`) || !strings.Contains(read, `"body":"Staticcheck and race gates are required."`) {
		t.Fatalf("read output=%s", read)
	}
	status := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"brain_index","arguments":{"action":"status"}}}`))
	if !strings.Contains(status, `"note_count":1`) || strings.Contains(status, filepath.Dir(os.TempDir())) {
		t.Fatalf("status output=%s", status)
	}
	contextText := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"brain_context","arguments":{"limit":5}}}`))
	if !strings.Contains(contextText, "release-observation") || strings.Contains(contextText, "Staticcheck and race gates are required") {
		t.Fatalf("context output=%s", contextText)
	}
}
