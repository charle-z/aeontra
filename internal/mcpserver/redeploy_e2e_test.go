package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

const (
	redeployBrainSlug = "redeploy-persistent-state"
	redeployBrainBody = "Persistent Brain state remains readable after an MCP instance replacement."
)

func newRedeployBrainServer(t *testing.T, repoRoot, brainRoot string, now time.Time) (*Server, *tools.Service, *brainpkg.Store) {
	t.Helper()
	cfg, err := config.New(config.Config{
		Roots:           []string{repoRoot},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store, err := brainpkg.OpenStore(brainRoot, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0]).WithBrainStore(store)
	return New(service), service, store
}

func writeRedeployBrainFixture(t *testing.T, store *brainpkg.Store) {
	t.Helper()
	_, err := store.WriteAgent(context.Background(), brainpkg.AgentDraft{
		Slug:       redeployBrainSlug,
		Title:      "Redeploy persistent state",
		Type:       brainpkg.TypeFact,
		Author:     "agent:redeploy-e2e",
		Provenance: "MCP instance replacement E2E fixture",
		ReviewBy:   "2026-10-31",
		Body:       redeployBrainBody,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readyStatus(t *testing.T, client *http.Client, baseURL string) int {
	t.Helper()
	response, err := client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func handlerReadyStatus(t *testing.T, handler http.Handler) int {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return response.Code
}

func initializeRemoteWithPrior(t *testing.T, client *http.Client, baseURL, priorSession string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+DefaultMCPPath, strings.NewReader(rpcBody(t, 20, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
	})))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if priorSession != "" {
		request.Header.Set("Mcp-Session-Id", priorSession)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replacement initialize status=%d", response.StatusCode)
	}
	sessionID := response.Header.Get("Mcp-Session-Id")
	if sessionID == "" || sessionID == priorSession {
		t.Fatalf("replacement session prior=%q new=%q", priorSession, sessionID)
	}
	return sessionID
}

func assertRemoteToolAvailable(t *testing.T, client *http.Client, baseURL, sessionID, toolName string) {
	t.Helper()
	listed := callRemoteRPC(t, client, baseURL, sessionID, rpcBody(t, 21, "tools/list", nil))
	if listed.Status != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", listed.Status, listed.Body)
	}
	var envelope struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(listed.Body), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, tool := range envelope.Result.Tools {
		if tool.Name == toolName {
			return
		}
	}
	t.Fatalf("tools/list missing %q", toolName)
}

func assertRemoteToolCall(t *testing.T, client *http.Client, baseURL, sessionID, toolName string, arguments map[string]any, expectedText string) {
	t.Helper()
	called := callRemoteRPC(t, client, baseURL, sessionID, rpcBody(t, 22, "tools/call", map[string]any{
		"name": toolName, "arguments": arguments,
	}))
	if called.Status != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", toolName, called.Status, called.Body)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(called.Body), &envelope); err != nil {
		t.Fatal(err)
	}
	if (len(envelope.Error) > 0 && string(envelope.Error) != "null") || envelope.Result.IsError || len(envelope.Result.Content) != 1 {
		t.Fatalf("%s failed: %s", toolName, called.Body)
	}
	if expectedText != "" && !strings.Contains(envelope.Result.Content[0].Text, expectedText) {
		t.Fatalf("%s result missing %q: %s", toolName, expectedText, envelope.Result.Content[0].Text)
	}
}

func TestRedeployE2EInstanceReplacementPreservesBrainAndSessionBoundary(t *testing.T) {
	for _, test := range []struct {
		name            string
		changeContract  bool
		expectedNewTool string
	}{
		{name: "same contract", expectedNewTool: "system_runtime_info"},
		{name: "changed contract", changeContract: true, expectedNewTool: "redeploy_contract_probe"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			brainRoot := filepath.Join(t.TempDir(), "brain")
			now := time.Date(2026, 7, 29, 4, 30, 0, 0, time.UTC)

			serverA, serviceA, storeA := newRedeployBrainServer(t, repoRoot, brainRoot, now)
			writeRedeployBrainFixture(t, storeA)
			infoA := serverA.mustRuntimeInfo()
			lifecycleA := newHTTPServerLifecycle()
			handlerA := serverA.httpHandlerWithRuntime(testToken, nil, HTTPOptions{}, lifecycleA, newHTTPSessionStore(defaultHTTPSessionTTL), newHTTPTransportTelemetry())

			logical := newSwitchingHTTPHandler(handlerA)
			endpoint := httptest.NewServer(logical)
			defer endpoint.Close()
			client := endpoint.Client()

			if readyStatus(t, client, endpoint.URL) != http.StatusOK {
				t.Fatal("server A was not ready before initialize")
			}
			oldSession := initializeRemote(t, client, endpoint.URL)
			assertRemoteToolAvailable(t, client, endpoint.URL, oldSession, "brain_read")
			assertRemoteToolCall(t, client, endpoint.URL, oldSession, "brain_read", map[string]any{"slug": redeployBrainSlug}, redeployBrainBody)

			lifecycleA.BeginDrain()
			if readyStatus(t, client, endpoint.URL) != http.StatusServiceUnavailable {
				t.Fatal("server A remained ready after drain began")
			}
			drainingInitialize := callRemoteRPC(t, client, endpoint.URL, "", rpcBody(t, 23, "initialize", nil))
			if drainingInitialize.Status != http.StatusServiceUnavailable {
				t.Fatalf("draining initialize status=%d body=%s", drainingInitialize.Status, drainingInitialize.Body)
			}
			if err := serviceA.BrainCapability.Close(); err != nil {
				t.Fatal(err)
			}

			serverB, serviceB, _ := newRedeployBrainServer(t, repoRoot, brainRoot, now.Add(time.Minute))
			defer func() { _ = serviceB.BrainCapability.Close() }()
			if serverA.BootID() == serverB.BootID() {
				t.Fatal("replacement instance reused the prior boot id")
			}
			if test.changeContract {
				serverB.table[test.expectedNewTool] = toolEntry{
					def: toolDef{
						Name:        test.expectedNewTool,
						Description: "Test-only contractual replacement probe.",
						InputSchema: map[string]any{"type": "object", "additionalProperties": false},
						Version:     "2",
					},
					handler: func(json.RawMessage) (string, error) { return "replacement probe ok", nil },
				}
				serverB.order = append(serverB.order, test.expectedNewTool)
			}
			infoB := serverB.mustRuntimeInfo()
			if test.changeContract && infoA.CatalogHash == infoB.CatalogHash {
				t.Fatalf("contract change retained hash %s", infoA.CatalogHash)
			}
			if !test.changeContract && infoA.CatalogHash != infoB.CatalogHash {
				t.Fatalf("same contract changed hash %s -> %s", infoA.CatalogHash, infoB.CatalogHash)
			}

			lifecycleB := newHTTPServerLifecycle()
			handlerB := serverB.httpHandlerWithRuntime(testToken, nil, HTTPOptions{}, lifecycleB, newHTTPSessionStore(defaultHTTPSessionTTL), newHTTPTransportTelemetry())
			if handlerReadyStatus(t, handlerB) != http.StatusOK {
				t.Fatal("server B exposed itself before it could serve MCP")
			}
			logical.Replace(handlerB)
			if readyStatus(t, client, endpoint.URL) != http.StatusOK {
				t.Fatal("logical endpoint did not become ready on server B")
			}

			assertRejectedSession(t, client, endpoint.URL, oldSession)
			newSession := initializeRemoteWithPrior(t, client, endpoint.URL, oldSession)
			assertRemoteToolAvailable(t, client, endpoint.URL, newSession, test.expectedNewTool)
			if test.changeContract {
				assertRemoteToolCall(t, client, endpoint.URL, newSession, test.expectedNewTool, map[string]any{}, "replacement probe ok")
			} else {
				assertRemoteToolCall(t, client, endpoint.URL, newSession, test.expectedNewTool, map[string]any{}, infoB.CatalogHash)
			}
			assertRemoteToolCall(t, client, endpoint.URL, newSession, "brain_read", map[string]any{"slug": redeployBrainSlug}, redeployBrainBody)
		})
	}
}
