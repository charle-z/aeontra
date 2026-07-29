package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

type switchingHTTPHandler struct {
	active atomic.Value
}

func newSwitchingHTTPHandler(initial http.Handler) *switchingHTTPHandler {
	h := &switchingHTTPHandler{}
	h.active.Store(initial)
	return h
}

func (h *switchingHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.active.Load().(http.Handler).ServeHTTP(w, r)
}

func (h *switchingHTTPHandler) Replace(next http.Handler) {
	h.active.Store(next)
}

type remoteRPCResponse struct {
	Status int
	Header http.Header
	Body   string
}

func rpcBody(t *testing.T, id int, method string, params any) string {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func callRemoteRPC(t *testing.T, client *http.Client, baseURL, sessionID, body string) remoteRPCResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+DefaultMCPPath, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return remoteRPCResponse{Status: response.StatusCode, Header: response.Header.Clone(), Body: string(data)}
}

func initializeRemote(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	response := callRemoteRPC(t, client, baseURL, "", rpcBody(t, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": true},
		},
	}))
	if response.Status != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", response.Status, response.Body)
	}
	sessionID := response.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize did not return Mcp-Session-Id")
	}
	return sessionID
}

func assertSuccessfulToolSequence(t *testing.T, client *http.Client, baseURL, sessionID, expectedTool string) {
	t.Helper()
	listed := callRemoteRPC(t, client, baseURL, sessionID, rpcBody(t, 2, "tools/list", nil))
	if listed.Status != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", listed.Status, listed.Body)
	}
	var listEnvelope struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(listed.Body), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range listEnvelope.Result.Tools {
		found = found || tool.Name == expectedTool
	}
	if !found {
		t.Fatalf("tools/list status=%d expected=%q body=%s", listed.Status, expectedTool, listed.Body)
	}
	called := callRemoteRPC(t, client, baseURL, sessionID, rpcBody(t, 3, "tools/call", map[string]any{
		"name": expectedTool, "arguments": map[string]any{},
	}))
	var callEnvelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(called.Body), &callEnvelope); err != nil {
		t.Fatal(err)
	}
	if called.Status != http.StatusOK || (len(callEnvelope.Error) > 0 && string(callEnvelope.Error) != "null") || callEnvelope.Result.IsError {
		t.Fatalf("tools/call status=%d body=%s", called.Status, called.Body)
	}
}

func TestRedeployContinuityCharacterizesCurrentSameCatalogBehavior(t *testing.T) {
	serverA, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverB, _ := newHTTPServerObject(t, config.ModeReadOnly)
	infoA := serverA.mustRuntimeInfo()
	infoB := serverB.mustRuntimeInfo()
	if infoA.CatalogHash != infoB.CatalogHash {
		t.Fatalf("same contract produced different hashes: %s != %s", infoA.CatalogHash, infoB.CatalogHash)
	}

	logical := newSwitchingHTTPHandler(serverA.HTTPHandler(testToken, nil))
	endpoint := httptest.NewServer(logical)
	defer endpoint.Close()

	oldSession := initializeRemote(t, endpoint.Client(), endpoint.URL)
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, oldSession, "system_runtime_info")

	logical.Replace(serverB.HTTPHandler(testToken, nil))
	newSession := initializeRemote(t, endpoint.Client(), endpoint.URL)
	if newSession == oldSession {
		t.Fatal("replacement instance reused the previous process session id")
	}
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, newSession, "system_runtime_info")

	// Characterization of the defect before the lifecycle fix: the replacement
	// instance ignores the caller's Mcp-Session-Id and accepts the previous
	// instance's session header for tools/list and tools/call.
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, oldSession, "system_runtime_info")
}

func TestRedeployContinuityCharacterizesCurrentChangedCatalogBehavior(t *testing.T) {
	serverA, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverB, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverB.table["redeploy_contract_probe"] = toolEntry{
		def: toolDef{
			Name:        "redeploy_contract_probe",
			Description: "Test-only contractual catalog change.",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
			Version:     "2",
		},
		handler: func(json.RawMessage) (string, error) { return "ok", nil },
	}
	serverB.order = append(serverB.order, "redeploy_contract_probe")

	infoA := serverA.mustRuntimeInfo()
	infoB := serverB.mustRuntimeInfo()
	if infoA.CatalogHash == infoB.CatalogHash {
		t.Fatalf("contractual test change did not alter catalog hash %s", infoA.CatalogHash)
	}

	logical := newSwitchingHTTPHandler(serverA.HTTPHandler(testToken, nil))
	endpoint := httptest.NewServer(logical)
	defer endpoint.Close()

	oldSession := initializeRemote(t, endpoint.Client(), endpoint.URL)
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, oldSession, "system_runtime_info")

	logical.Replace(serverB.HTTPHandler(testToken, nil))
	newSession := initializeRemote(t, endpoint.Client(), endpoint.URL)
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, newSession, "redeploy_contract_probe")

	// Current defective behavior also lets the previous session silently observe
	// and invoke the replacement instance's changed catalog.
	assertSuccessfulToolSequence(t, endpoint.Client(), endpoint.URL, oldSession, "redeploy_contract_probe")
}
