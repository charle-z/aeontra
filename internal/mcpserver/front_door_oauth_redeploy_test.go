package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/frontdoor"
	"github.com/charle-z/mcp-devbox/internal/oauth"
)

func issueFrontDoorOAuthAccessToken(t *testing.T, client *http.Client, baseURL, passphrase, resource string) string {
	t.Helper()
	const redirectURI = "http://127.0.0.1/front-door-oauth-callback"
	registration, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirectURI}})
	registered, err := client.Post(baseURL+"/oauth/register", "application/json", bytes.NewReader(registration))
	if err != nil {
		t.Fatal(err)
	}
	var clientRecord struct {
		ClientID string `json:"client_id"`
	}
	decodeJSON(t, registered, &clientRecord)
	if clientRecord.ClientID == "" {
		t.Fatal("OAuth registration returned no client id")
	}

	verifier := "front-door-oauth-verifier-012345678901234567890123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authorized, err := client.PostForm(baseURL+"/oauth/authorize", url.Values{
		"response_type":         {"code"},
		"client_id":             {clientRecord.ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"front-door-state"},
		"scope":                 {"mcp"},
		"resource":              {resource},
		"passphrase":            {passphrase},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer authorized.Body.Close()
	if authorized.StatusCode != http.StatusFound {
		t.Fatalf("authorize status=%d", authorized.StatusCode)
	}
	location, err := url.Parse(authorized.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("authorize returned no code")
	}

	tokenResponse, err := client.PostForm(baseURL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientRecord.ClientID},
		"code_verifier": {verifier},
		"resource":      {resource},
	})
	if err != nil {
		t.Fatal(err)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, tokenResponse, &token)
	if token.AccessToken == "" {
		t.Fatal("token endpoint returned no access token")
	}
	return token.AccessToken
}

func oauthContinuityToolCall(client *http.Client, baseURL, accessToken, sessionID string, id int) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": "system_runtime_info", "arguments": map[string]any{}},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+DefaultMCPPath, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Session-Id", sessionID)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tool call %d returned HTTP %d: %s", id, response.StatusCode, body)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || (len(envelope.Error) > 0 && string(envelope.Error) != "null") || envelope.Result.IsError || len(envelope.Result.Content) != 1 {
		return fmt.Errorf("tool call %d returned an invalid MCP result", id)
	}
	return nil
}

func openOAuthContinuitySSE(t *testing.T, client *http.Client, baseURL, accessToken, sessionID string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+DefaultMCPPath, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Mcp-Session-Id", sessionID)
	response, err := client.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		response.Body.Close()
		cancel()
		t.Fatalf("SSE status=%d content-type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": mcp-devbox stream open\n" {
		response.Body.Close()
		cancel()
		t.Fatalf("SSE open line=%q err=%v", line, err)
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "\n" {
		response.Body.Close()
		cancel()
		t.Fatalf("SSE separator=%q err=%v", line, err)
	}
	closed := make(chan error, 1)
	go func() {
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				closed <- err
				return
			}
		}
	}()
	return func() {
		cancel()
		response.Body.Close()
	}, closed
}

func TestFrontDoorBackendReplacementPreservesOAuthBearerSessionSSEAndTool(t *testing.T) {
	previousCommit, previousBuiltAt := buildinfo.Commit, buildinfo.BuiltAt
	buildinfo.Commit = strings.Repeat("e", 40)
	buildinfo.BuiltAt = "2026-08-01T00:00:00Z"
	defer func() {
		buildinfo.Commit = previousCommit
		buildinfo.BuiltAt = previousBuiltAt
	}()

	const (
		passphrase = "front-door-owner-passphrase"
		issuer     = "http://localhost:8765"
		resource   = issuer + DefaultMCPPath
	)
	oauthRoot := t.TempDir()
	oauthConfig := oauth.Config{
		Issuer: issuer, Resource: resource, Passphrase: passphrase,
		ClientStorePath: filepath.Join(oauthRoot, "clients.json"), AccessStorePath: filepath.Join(oauthRoot, "access.json"),
		RefreshStorePath: filepath.Join(oauthRoot, "refresh.json"),
	}
	providerA, err := oauth.NewProvider(oauthConfig)
	if err != nil {
		t.Fatal(err)
	}
	storeA, storeB := openReplacementSessionStores(t)
	serverA, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverB, _ := newHTTPServerObject(t, config.ModeReadOnly)
	serverA.WithHTTPSessionStore(storeA)
	serverB.WithHTTPSessionStore(storeB)
	infoA := serverA.mustRuntimeInfo()
	lifecycleA, lifecycleB := newHTTPServerLifecycle(), newHTTPServerLifecycle()
	handlerA := serverA.httpHandlerWithRuntime("", providerA, HTTPOptions{}, lifecycleA, storeA, newHTTPTransportTelemetry())

	tracker := &continuityRPCTracker{}
	instanceA := &countedBackendInstance{next: handlerA, tracker: tracker}
	unavailable := &countedBackendInstance{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "replacement gap", http.StatusServiceUnavailable)
	}), tracker: tracker}
	backendSwitch := newSwitchingHTTPHandler(instanceA)
	backend := httptest.NewServer(backendSwitch)
	defer backend.Close()

	front, err := frontdoor.New(frontdoor.Config{
		BackendURL: backend.URL, ExpectedProtocol: infoA.ProtocolVersion, ExpectedCatalogHash: infoA.CatalogHash,
		FrontDoorCommit: strings.Repeat("f", 40), ProbeInterval: 250 * time.Millisecond, ProbeTimeout: time.Second,
		AdmissionTimeout: 3 * time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := front.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(front.Handler())
	defer endpoint.Close()
	client := endpoint.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	accessToken := issueFrontDoorOAuthAccessToken(t, client, endpoint.URL, passphrase, resource)
	status, sessionID := mcpInitialize(t, client, endpoint.URL, accessToken)
	if status != http.StatusOK || sessionID == "" {
		t.Fatalf("initialize status=%d session=%q", status, sessionID)
	}
	if err := oauthContinuityToolCall(client, endpoint.URL, accessToken, sessionID, 200); err != nil {
		t.Fatal(err)
	}
	closeSSE, sseClosed := openOAuthContinuitySSE(t, client, endpoint.URL, accessToken, sessionID)
	defer closeSSE()

	providerB, err := oauth.NewProvider(oauthConfig)
	if err != nil {
		t.Fatal(err)
	}
	handlerB := serverB.httpHandlerWithRuntime("", providerB, HTTPOptions{}, lifecycleB, storeB, newHTTPTransportTelemetry())
	instanceB := &countedBackendInstance{next: handlerB, tracker: tracker}

	backendSwitch.Replace(unavailable)
	lifecycleA.BeginDrain()
	if err := front.Probe(context.Background()); err == nil {
		t.Fatal("front door accepted the replacement gap")
	}
	callDone := make(chan error, 1)
	go func() {
		callDone <- oauthContinuityToolCall(client, endpoint.URL, accessToken, sessionID, 201)
	}()
	select {
	case err := <-callDone:
		t.Fatalf("OAuth tool call escaped the gap: %v", err)
	case err := <-sseClosed:
		t.Fatalf("OAuth SSE reset during the gap: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	backendSwitch.Replace(instanceB)
	if err := front.Probe(context.Background()); err != nil {
		t.Fatalf("replacement backend was rejected: %v", err)
	}
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OAuth tool call did not recover")
	}
	select {
	case err := <-sseClosed:
		t.Fatalf("OAuth SSE closed after replacement: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	reinitialized := mcpOAuthRequest(t, client, endpoint.URL, accessToken, sessionID, rpcBody(t, 202, "initialize", nil))
	defer reinitialized.Body.Close()
	if reinitialized.StatusCode != http.StatusOK || reinitialized.Header.Get("Mcp-Session-Id") != sessionID {
		t.Fatalf("durable OAuth reinitialize status=%d old=%q new=%q", reinitialized.StatusCode, sessionID, reinitialized.Header.Get("Mcp-Session-Id"))
	}
	if err := oauthContinuityToolCall(client, endpoint.URL, accessToken, sessionID, 203); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{200, 201, 203} {
		if count := tracker.count(id); count != 1 {
			t.Fatalf("OAuth JSON-RPC id %d was forwarded %d times", id, count)
		}
	}
	requireFrontDoorRecoveryMetrics(t, front)

	closeSSE()
	select {
	case err := <-sseClosed:
		if err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "closed") {
			t.Logf("OAuth SSE closed after client cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("OAuth SSE did not close after client cancellation")
	}
}
