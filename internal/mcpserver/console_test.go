package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPConsoleLoginAndStatusUsePublicRuntimeIdentity(t *testing.T) {
	server, _, _ := newObservedServer(t)
	handler := server.HTTPHandlerWithOptions(testToken, nil, HTTPOptions{})

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/console", nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), "MCP Devbox Console") {
		t.Fatalf("login page status=%d body=%s", loginPage.Code, loginPage.Body.String())
	}

	form := url.Values{"token": {testToken}}
	loginRequest := httptest.NewRequest(http.MethodPost, "/console/login", strings.NewReader(form.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/console/status", nil)
	statusRequest.AddCookie(cookies[0])
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	var status struct {
		Status          string `json:"status"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Commit          string `json:"commit"`
		ToolCount       int    `json:"tool_count"`
		CatalogHash     string `json:"catalog_hash"`
		Authenticated   bool   `json:"authenticated"`
		Surface         string `json:"surface"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	runtimeInfo, err := server.RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != runtimeInfo.Status || status.Version != runtimeInfo.Version || status.ProtocolVersion != runtimeInfo.ProtocolVersion || status.Commit != runtimeInfo.Commit || status.ToolCount != runtimeInfo.ToolCount || status.CatalogHash != runtimeInfo.CatalogHash {
		t.Fatalf("console status=%+v runtime=%+v", status, runtimeInfo)
	}
	if !status.Authenticated || status.Surface != "presentation-only" {
		t.Fatalf("console flags=%+v", status)
	}
}

func TestHTTPConsoleDoesNotChangeMCPAuthOrCatalog(t *testing.T) {
	server, _, _ := newObservedServer(t)
	handler := server.HTTPHandlerWithOptions(testToken, nil, HTTPOptions{})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized MCP status=%d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	authorizedRequest.Header.Set("Authorization", "Bearer "+testToken)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized MCP status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	info, err := server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.ToolCount != 100 || info.Hash != "sha256:370a309ba1b63a500dd4d2abae77a11e60e49b35cdbfdc3adf4e692e78772ea2" {
		t.Fatalf("catalog changed: %+v", info)
	}
}

func TestHTTPConsoleRoutesAreClassifiedWithoutSensitiveInput(t *testing.T) {
	server, _, events := newObservedServer(t)
	handler := server.HTTPHandlerWithOptions(testToken, nil, HTTPOptions{})
	secret := "customer-secret-query-token"
	request := httptest.NewRequest(http.MethodGet, "/console?key="+secret, nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status=%d", response.Code)
	}
	if strings.Contains(events.String(), secret) {
		t.Fatalf("console query leaked into observability: %s", events.String())
	}
	decoded := decodeEvents(t, events.String())
	if len(decoded) != 1 || string(decoded[0].Route) != "console" {
		t.Fatalf("events=%+v", decoded)
	}
}
