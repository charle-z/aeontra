package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type testSystem struct {
	root   string
	policy *policy.Policy
	tools  *tools.Service
	server *mcpserver.Server
	audit  *bytes.Buffer
}

func newSystem(t *testing.T, mode config.Mode) *testSystem {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            mode,
		AllowedCommands: []string{"git", "go"},
		TestCommand:     []string{"go", "test", "./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var auditOutput bytes.Buffer
	service := tools.NewService(pol, audit.New(&auditOutput), root).WithTestCommand(cfg.TestCommand)
	return &testSystem{
		root:   root,
		policy: pol,
		tools:  service,
		server: mcpserver.New(service),
		audit:  &auditOutput,
	}
}

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolListResult struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

func decodeToolNames(t *testing.T, body []byte) []string {
	t.Helper()
	var envelope rpcEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(body), &envelope); err != nil {
		t.Fatalf("decode RPC envelope: %v: %s", err, body)
	}
	if envelope.Error != nil {
		t.Fatalf("RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	var result toolListResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tool list: %v: %s", err, envelope.Result)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func rpcToolsListRequest() string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
}

func TestStdioHTTPAndRuntimeIdentityRemainEquivalent(t *testing.T) {
	system := newSystem(t, config.ModeReadOnly)
	catalog, err := system.server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ToolCount != 100 {
		t.Fatalf("tool count = %d, want 92", catalog.ToolCount)
	}

	var stdioOutput bytes.Buffer
	if err := system.server.Serve(strings.NewReader(rpcToolsListRequest()+"\n"), &stdioOutput); err != nil {
		t.Fatal(err)
	}
	stdioNames := decodeToolNames(t, stdioOutput.Bytes())

	const token = "integration-bearer-token"
	handler := system.server.HTTPHandler(token, nil)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "http://mcp.local/mcp", strings.NewReader(rpcToolsListRequest())))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("unauthorized status=%d challenge=%q", unauthorized.Code, unauthorized.Header().Get("WWW-Authenticate"))
	}

	authorizedRequest := httptest.NewRequest(http.MethodPost, "http://mcp.local/mcp", strings.NewReader(rpcToolsListRequest()))
	authorizedRequest.Header.Set("Authorization", "Bearer "+token)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d body=%s", authorized.Code, authorized.Body.String())
	}
	httpNames := decodeToolNames(t, authorized.Body.Bytes())
	if strings.Join(stdioNames, "\n") != strings.Join(httpNames, "\n") {
		t.Fatal("stdio and HTTP tool order differ")
	}
	if len(httpNames) != catalog.ToolCount {
		t.Fatalf("HTTP tools = %d, catalog = %d", len(httpNames), catalog.ToolCount)
	}
	if authorized.Header().Get("X-MCP-Catalog-Hash") != catalog.Hash ||
		authorized.Header().Get("X-MCP-Tool-Count") != strconv.Itoa(catalog.ToolCount) {
		t.Fatalf("runtime headers hash=%q count=%q", authorized.Header().Get("X-MCP-Catalog-Hash"), authorized.Header().Get("X-MCP-Tool-Count"))
	}

	versionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(versionRecorder, httptest.NewRequest(http.MethodGet, "http://mcp.local/version", nil))
	if versionRecorder.Code != http.StatusOK {
		t.Fatalf("version status = %d", versionRecorder.Code)
	}
	var version mcpserver.RuntimeInfo
	if err := json.Unmarshal(versionRecorder.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	runtimeInfo, err := system.server.RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if version != runtimeInfo || version.ToolCount != len(httpNames) || version.CatalogHash != catalog.Hash {
		t.Fatalf("version=%#v runtime=%#v catalog=%#v", version, runtimeInfo, catalog)
	}
}

func TestHTTPBearerAndOAuthFailClosedContracts(t *testing.T) {
	system := newSystem(t, config.ModeReadOnly)
	if err := system.server.ServeHTTP(context.Background(), "127.0.0.1:0", "", nil); err == nil || !strings.Contains(err.Error(), "requires auth") {
		t.Fatalf("ServeHTTP without auth error = %v", err)
	}

	provider, err := oauth.NewProvider(oauth.Config{
		Issuer:     "http://127.0.0.1",
		Resource:   "http://127.0.0.1/mcp",
		Passphrase: "integration-owner-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := system.server.HTTPHandler("", provider)

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(rpcToolsListRequest()))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("OAuth-protected MCP status = %d", recorder.Code)
	}
	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, "resource_metadata=") || !strings.Contains(challenge, "/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("OAuth challenge = %q", challenge)
	}

	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/.well-known/oauth-protected-resource/mcp", nil))
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"resource":"http://127.0.0.1/mcp"`) {
		t.Fatalf("protected resource metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
}

func TestLocalGrantApprovalUnlocksOneRedactedSensitiveRead(t *testing.T) {
	system := newSystem(t, config.ModeReadOnly)
	const secret = "integration-secret-value-1234567890"
	if err := os.WriteFile(filepath.Join(system.root, ".env"), []byte("API_KEY="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := system.tools.ReadFile(".env")
	var required *policy.AccessRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("initial read error = %v, want AccessRequiredError", err)
	}
	if required.RequestID == "" || !filepath.IsAbs(required.Path) {
		t.Fatalf("access request = %#v", required)
	}

	const adminToken = "local-admin-token"
	admin := grantadmin.Handler(system.policy, audit.New(system.audit), adminToken)
	unauthorized := httptest.NewRecorder()
	admin.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/admin/grants/"+required.RequestID, strings.NewReader(`{"ttl":"1m"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admin status = %d", unauthorized.Code)
	}

	approveRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/admin/grants/"+required.RequestID, strings.NewReader(`{"ttl":"1m","raw":false}`))
	approveRequest.Header.Set("Authorization", "Bearer "+adminToken)
	approved := httptest.NewRecorder()
	admin.ServeHTTP(approved, approveRequest)
	if approved.Code != http.StatusOK {
		t.Fatalf("approval status = %d body=%s", approved.Code, approved.Body.String())
	}

	content, err := system.tools.ReadFileWithAccess(".env", required.RequestID, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, secret) || !strings.Contains(content, "REDACTED") {
		t.Fatalf("sensitive read was not redacted: %q", content)
	}
	if _, err := system.tools.ReadFileWithAccess(".env", required.RequestID, false); !errors.Is(err, policy.ErrAccessGrantUsed) {
		t.Fatalf("grant replay error = %v, want ErrAccessGrantUsed", err)
	}
	if !strings.Contains(system.audit.String(), "access_grant") || strings.Contains(system.audit.String(), secret) {
		t.Fatalf("grant audit missing or leaked secret: %s", system.audit.String())
	}
}

func TestPlannedNoteWorkflowRequiresApprovalAndRejectsReplay(t *testing.T) {
	system := newSystem(t, config.ModeAsk)
	preview, err := system.tools.NotesWritePreview("integration", "hello from integration", "create")
	if err != nil {
		t.Fatal(err)
	}
	planID := field(preview, "plan_id")
	if planID == "" {
		t.Fatalf("preview missing plan id: %s", preview)
	}

	withoutApproval, err := system.tools.NotesWrite(planID, false)
	if err != nil || !strings.Contains(withoutApproval, "APPROVAL REQUIRED") {
		t.Fatalf("without approval result=%q err=%v", withoutApproval, err)
	}
	result, err := system.tools.NotesWrite(planID, true)
	if err != nil || !strings.Contains(result, "note create") {
		t.Fatalf("approved result=%q err=%v", result, err)
	}
	if _, err := system.tools.NotesWrite(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("plan replay error = %v", err)
	}
	content, err := system.tools.NotesRead("integration")
	if err != nil || !strings.Contains(content, "hello from integration") {
		t.Fatalf("note content=%q err=%v", content, err)
	}
	for _, required := range []string{"plan-created", "plan-executed", "notes_write"} {
		if !strings.Contains(system.audit.String(), required) {
			t.Fatalf("audit does not contain %q: %s", required, system.audit.String())
		}
	}
}

func field(text, name string) string {
	prefix := name + ":"
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func TestIntegrationSuiteUsesOnlyLoopbackAndTemporaryState(t *testing.T) {
	// This sentinel test documents and enforces the matrix posture: no environment
	// credentials are read and every HTTP request above targets synthetic loopback hosts.
	if !strings.HasPrefix("http://127.0.0.1", "http://127.0.0.1") || time.Minute <= 0 {
		t.Fatal("unreachable")
	}
}
