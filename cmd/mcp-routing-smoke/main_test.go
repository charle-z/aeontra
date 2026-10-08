package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

const smokeTestToken = "routing-smoke-test-token"

type routingFixture struct {
	identity        versionResponse
	partialCatalog  bool
	staleFirstList  bool
	unknownRuntime  bool
	missingResource bool
	mutateWorkspace bool
	initializes     int
	listCalls       int
	runtimeCalls    int
	statusToolCalls int
	sandboxCalls    int
	statusCalls     int
	session         string
}

func newRoutingFixture(t *testing.T) *routingFixture {
	t.Helper()
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	return &routingFixture{identity: versionResponse{
		Status:          local.Status,
		Version:         local.Version,
		ProtocolVersion: local.ProtocolVersion,
		Commit:          "expected-commit",
		BuiltAt:         "2026-09-16T00:00:00Z",
		ToolCount:       local.ToolCount,
		CatalogHash:     local.CatalogHash,
	}}
}

func (f *routingFixture) serveHTTP(response http.ResponseWriter, request *http.Request) {
	f.setIdentityHeaders(response.Header())
	response.Header().Set("Content-Type", "application/json")
	if request.URL.Path == "/version" {
		_ = json.NewEncoder(response).Encode(f.identity)
		return
	}
	if request.URL.Path != "/mcp" || request.Method != http.MethodPost {
		http.NotFound(response, request)
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+smokeTestToken {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}
	var requestBody struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string `json:"name"`
			Arguments struct {
				Command []string `json:"command"`
				CWD     string   `json:"cwd"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
		http.Error(response, "bad request", http.StatusBadRequest)
		return
	}
	switch requestBody.Method {
	case "initialize":
		f.initializes++
		f.session = fmt.Sprintf("session-%d", f.initializes)
		response.Header().Set("Mcp-Session-Id", f.session)
		f.writeResult(response, requestBody.ID, map[string]any{
			"protocolVersion": f.identity.ProtocolVersion,
			"serverInfo":      map[string]any{"name": "mcp-devbox", "version": f.identity.Version},
		})
	case "tools/list":
		f.listCalls++
		if f.staleFirstList && f.listCalls == 1 {
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"unknown MCP session"}}`))
			return
		}
		if request.Header.Get("Mcp-Session-Id") != f.session {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		count := f.identity.ToolCount
		if f.partialCatalog {
			count--
		}
		tools := make([]map[string]any, 0, count)
		for _, name := range []string{requiredRuntimeTool, requiredSandboxTool, optionalExecutorTool} {
			tools = append(tools, map[string]any{
				"name": name, "description": "fixture", "inputSchema": map[string]any{"type": "object"},
			})
		}
		for len(tools) < count {
			tools = append(tools, map[string]any{
				"name":        fmt.Sprintf("fixture_tool_%03d", len(tools)),
				"description": "fixture", "inputSchema": map[string]any{"type": "object"},
			})
		}
		f.writeResult(response, requestBody.ID, map[string]any{"tools": tools})
	case "tools/call":
		if request.Header.Get("Mcp-Session-Id") != f.session {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		if requestBody.Params.Name == requiredRuntimeTool {
			f.runtimeCalls++
			if f.unknownRuntime {
				_ = json.NewEncoder(response).Encode(map[string]any{
					"jsonrpc": "2.0", "id": requestBody.ID,
					"error": map[string]any{"code": -32602, "message": "unknown tool: system_runtime_info"},
				})
				return
			}
			data, _ := json.Marshal(f.identity)
			f.writeToolText(response, requestBody.ID, string(data))
			return
		}
		if requestBody.Params.Name == requiredSandboxTool {
			f.statusToolCalls++
			if f.missingResource {
				f.writeToolError(response, requestBody.ID, "resource not found")
				return
			}
			f.writeToolText(response, requestBody.ID, `{"status":"ready"}`)
			return
		}
		if requestBody.Params.Name == optionalExecutorTool {
			f.sandboxCalls++
			command := strings.Join(requestBody.Params.Arguments.Command, " ")
			switch command {
			case "git status --porcelain=v1":
				f.statusCalls++
				text := "command completed"
				if f.mutateWorkspace && f.statusCalls == 2 {
					text += "\n M changed.go"
				}
				f.writeToolText(response, requestBody.ID, text)
			case "pwd":
				f.writeToolText(response, requestBody.ID, "/workspace")
			case "git rev-parse HEAD":
				f.writeToolText(response, requestBody.ID, strings.Repeat("a", 40))
			default:
				_ = json.NewEncoder(response).Encode(map[string]any{
					"jsonrpc": "2.0", "id": requestBody.ID,
					"error": map[string]any{"code": -32602, "message": "unexpected sandbox probe"},
				})
			}
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"jsonrpc": "2.0", "id": requestBody.ID,
			"error": map[string]any{"code": -32602, "message": "unknown tool"},
		})
	default:
		_ = json.NewEncoder(response).Encode(map[string]any{
			"jsonrpc": "2.0", "id": requestBody.ID,
			"error": map[string]any{"code": -32601, "message": "unknown method"},
		})
	}
}

func (f *routingFixture) setIdentityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
	header.Set("X-MCP-Server-Commit", f.identity.Commit)
	header.Set("X-MCP-Catalog-Hash", f.identity.CatalogHash)
	header.Set("X-MCP-Tool-Count", strconv.Itoa(f.identity.ToolCount))
}

func (f *routingFixture) writeResult(response http.ResponseWriter, id int, result any) {
	_ = json.NewEncoder(response).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (f *routingFixture) writeToolText(response http.ResponseWriter, id int, text string) {
	f.writeResult(response, id, map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": false,
		"_meta":   map[string]any{"fixture": true},
	})
}

func (f *routingFixture) writeToolError(response http.ResponseWriter, id int, text string) {
	f.writeResult(response, id, map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": true,
	})
}

func runFixture(t *testing.T, fixture *routingFixture) (string, error) {
	return runFixtureArgs(t, fixture, nil)
}

func runFixtureArgs(t *testing.T, fixture *routingFixture, extraArgs []string) (string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(server.Close)
	var output bytes.Buffer
	args := []string{"--url", server.URL, "--expected-commit", fixture.identity.Commit}
	args = append(args, extraArgs...)
	err := run(
		args,
		&output,
		func(name string) string {
			if name == defaultBearerEnv {
				return smokeTestToken
			}
			return ""
		},
		server.Client(),
	)
	return output.String(), err
}

func TestRoutingSmokeSandboxProbesPreserveWorkspace(t *testing.T) {
	fixture := newRoutingFixture(t)
	output, err := runFixtureArgs(t, fixture, []string{"--sandbox-cwd", "fixture-repository"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "sandbox_exec_calls=true") || !strings.Contains(output, "workspace_unchanged=true") || fixture.sandboxCalls != 4 {
		t.Fatalf("output=%q sandbox_calls=%d", output, fixture.sandboxCalls)
	}
}

func TestRoutingSmokeDetectsWorkspaceMutation(t *testing.T) {
	fixture := newRoutingFixture(t)
	fixture.mutateWorkspace = true
	_, err := runFixtureArgs(t, fixture, []string{"--sandbox-cwd", "fixture-repository"})
	if err == nil || !strings.Contains(err.Error(), "changed the workspace state") {
		t.Fatalf("error=%v", err)
	}
}

func TestRoutingSmokeMaterializesAndCallsCatalog(t *testing.T) {
	fixture := newRoutingFixture(t)
	output, err := runFixture(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"routing smoke passed",
		"catalog_materialized=true",
		"runtime_call=true",
		"sandbox_status_call=true",
		"session_recoveries=0",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, smokeTestToken) || fixture.initializes != 1 || fixture.listCalls != 1 || fixture.runtimeCalls != 1 {
		t.Fatalf("unexpected output or call counts output=%q init=%d list=%d runtime=%d", output, fixture.initializes, fixture.listCalls, fixture.runtimeCalls)
	}
}

func TestRoutingSmokeRejectsPartialCatalog(t *testing.T) {
	fixture := newRoutingFixture(t)
	fixture.partialCatalog = true
	_, err := runFixture(t, fixture)
	if err == nil || !strings.Contains(err.Error(), "materialized tools/list contains") {
		t.Fatalf("error=%v", err)
	}
	if fixture.runtimeCalls != 0 {
		t.Fatalf("partial catalog reached tool invocation: %d", fixture.runtimeCalls)
	}
}

func TestRoutingSmokeRefreshesOnlyAnHTTP404Session(t *testing.T) {
	fixture := newRoutingFixture(t)
	fixture.staleFirstList = true
	output, err := runFixture(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "session_recoveries=1") || fixture.initializes != 2 || fixture.listCalls != 2 || fixture.runtimeCalls != 1 {
		t.Fatalf("output=%q init=%d list=%d runtime=%d", output, fixture.initializes, fixture.listCalls, fixture.runtimeCalls)
	}
}

func TestRoutingSmokeDoesNotRetryApplicationErrors(t *testing.T) {
	fixture := newRoutingFixture(t)
	fixture.unknownRuntime = true
	_, err := runFixture(t, fixture)
	if err == nil || !strings.Contains(err.Error(), "MCP error code -32602") {
		t.Fatalf("error=%v", err)
	}
	if fixture.initializes != 1 || fixture.runtimeCalls != 1 {
		t.Fatalf("application error was retried: init=%d runtime=%d", fixture.initializes, fixture.runtimeCalls)
	}
}

func TestRoutingSmokeDoesNotRetryGenuineResourceNotFound(t *testing.T) {
	fixture := newRoutingFixture(t)
	fixture.missingResource = true
	_, err := runFixture(t, fixture)
	if err == nil || !strings.Contains(err.Error(), "error or invalid content shape") {
		t.Fatalf("error=%v", err)
	}
	if fixture.initializes != 1 || fixture.statusToolCalls != 1 {
		t.Fatalf("server resource error was retried: init=%d sandbox_status=%d", fixture.initializes, fixture.statusToolCalls)
	}
}

func TestRoutingSmokeURLAndCredentialValidation(t *testing.T) {
	for _, rawURL := range []string{"", "http://example.com", "https://user:pass@example.com", "https://example.com?token=x", "https://example.com/admin"} {
		if _, _, err := smokeEndpoints(rawURL); err == nil {
			t.Fatalf("smokeEndpoints(%q) unexpectedly succeeded", rawURL)
		}
	}
	if err := run([]string{"--url", "https://example.com", "--expected-commit", "sha"}, &bytes.Buffer{}, func(string) string { return "" }, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty credential error=%v", err)
	}
}
