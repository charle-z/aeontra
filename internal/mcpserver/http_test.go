package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

const testToken = "s3cr3t-bearer-token-value"

func newHTTPServer(t *testing.T, mode config.Mode) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            mode,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := pol.Roots()[0]
	svc := tools.NewService(pol, audit.New(&bytes.Buffer{}), resolved)
	return New(svc).HTTPHandler(testToken, nil), resolved
}

// newHTTPServerWithOAuth builds a handler with the OAuth provider enabled (and no static
// token) to exercise the OAuth integration boundary.
func newHTTPServerWithOAuth(t *testing.T, staticToken string) http.Handler {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0])
	prov, err := oauth.NewProvider(oauth.Config{
		Issuer:     "http://localhost:8765",
		Resource:   "http://localhost:8765/mcp",
		Passphrase: "owner-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(svc).HTTPHandler(staticToken, prov)
}

func TestHTTP_OAuthChallengePointsToPRM(t *testing.T) {
	h := newHTTPServerWithOAuth(t, "")
	rr := do(t, h, "POST", "/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rr.Code)
	}
	wa := rr.Header().Get("WWW-Authenticate")
	if !strings.Contains(wa, `resource_metadata="http://localhost:8765/.well-known/oauth-protected-resource/mcp"`) {
		t.Errorf("WWW-Authenticate = %q, must point at the exact PRM", wa)
	}
}

func TestHTTP_OAuthDiscoveryMountedUnauthenticated(t *testing.T) {
	h := newHTTPServerWithOAuth(t, "")
	rr := do(t, h, "GET", "/.well-known/oauth-protected-resource/mcp", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("PRM discovery: got %d, want 200 (unauthenticated)", rr.Code)
	}
}

func TestHTTP_LegacyTokenStillWorksWithOAuth(t *testing.T) {
	// With OAuth enabled AND a static token set, the legacy bearer must still authorize.
	h := newHTTPServerWithOAuth(t, testToken)
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("legacy token should authorize even with OAuth on; got 401")
	}
}

func do(t *testing.T, h http.Handler, method, path, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func doWithHeaders(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHTTP_NoAuth401(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "POST", "/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 should carry a WWW-Authenticate header")
	}
}

func TestHTTP_WrongToken401(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "POST", "/mcp", "Bearer wrong-token", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", rr.Code)
	}
}

func TestHTTP_InitializeWithToken(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rr.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	if rr.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("initialize should return Mcp-Session-Id")
	}
}

func TestHTTP_AcceptsSessionIDOnLaterPost(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	init := do(t, h, "POST", "/mcp", "Bearer "+testToken, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	sessionID := init.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize should return Mcp-Session-Id")
	}
	rr := doWithHeaders(t, h, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{
		"Authorization":  "Bearer " + testToken,
		"Mcp-Session-Id": sessionID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST with Mcp-Session-Id: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHTTP_ToolsCallReadFileRedacted(t *testing.T) {
	h, root := newHTTPServer(t, config.ModeReadOnly)
	// A secret inside a legitimately-named file must be redacted in the HTTP result.
	secretFile := filepath.Join(root, "cfg.go")
	if err := os.WriteFile(secretFile, []byte("const T = \"gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"cfg.go"}}}`
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, msg)
	if rr.Code != http.StatusOK {
		t.Fatalf("tools/call: got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret leaked over HTTP transport: %s", rr.Body.String())
	}
}

func TestHTTP_NotificationReturns202(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("notification: got %d, want 202", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("202 should have empty body, got %q", rr.Body.String())
	}
}

func TestHTTP_GetMCPReturnsSSE(t *testing.T) {
	// The SSE stream is persistent: it stays open until the client disconnects. Drive
	// it with a cancelable context in a goroutine so the synchronous handler does not
	// hang the test; cancel to close, then assert the opening comment was flushed.
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/mcp", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { h.ServeHTTP(rr, req); close(done) }()
	cancel() // client disconnects; a persistent stream must then return promptly
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("persistent SSE handler did not return after client disconnect (hung)")
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /mcp: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("GET /mcp content-type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(rr.Body.String(), ":") {
		t.Fatalf("SSE stream should contain at least a keep-alive comment, got %q", rr.Body.String())
	}
}

func TestHTTP_GetMCPRequiresAuth(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "GET", "/mcp", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /mcp without auth: got %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 should carry a WWW-Authenticate header")
	}
}

func TestHTTP_HealthzNoAuth(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, "GET", "/healthz", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("/healthz: got %d, want 200", rr.Code)
	}
}

func TestHTTP_OversizedBodyRejected(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	big := strings.Repeat("a", (4<<20)+10)
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, big)
	if rr.Code != http.StatusRequestEntityTooLarge && rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: got %d, want 413 or 400", rr.Code)
	}
}

func TestHTTP_QueryParamTokenAuth(t *testing.T) {
	// ChatGPT's connector can't send a custom Authorization header, so the token
	// may travel in the URL as ?key=. A correct key authorizes; a wrong/missing one
	// is still 401.
	h, _ := newHTTPServer(t, config.ModeReadOnly)

	rr := do(t, h, "POST", "/mcp?key="+testToken, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("query-param auth: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	rr = do(t, h, "POST", "/mcp?key=wrong", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong query-param key: got %d, want 401", rr.Code)
	}
	rr = do(t, h, "POST", "/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no key at all: got %d, want 401", rr.Code)
	}
}

func TestInitialize_EchoesRequestedProtocolVersion(t *testing.T) {
	s, _ := newTestServer(t, config.ModeReadOnly)
	resp := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	b, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(b), `"protocolVersion":"2025-06-18"`) {
		t.Errorf("server should echo the client's requested protocolVersion: %s", b)
	}
}

func TestInitialize_DefaultsProtocolVersionWhenAbsent(t *testing.T) {
	s, _ := newTestServer(t, config.ModeReadOnly)
	resp := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	b, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(b), `"protocolVersion":"`+buildinfo.ProtocolVersion+`"`) {
		t.Errorf("server should default protocolVersion to %s: %s", buildinfo.ProtocolVersion, b)
	}
}

func TestHTTP_BatchRequest(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	batch := `[{"jsonrpc":"2.0","id":1,"method":"initialize"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`
	rr := do(t, h, "POST", "/mcp", "Bearer "+testToken, batch)
	if rr.Code != http.StatusOK {
		t.Fatalf("batch: got %d", rr.Code)
	}
	var arr []rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &arr); err != nil {
		t.Fatalf("batch response not a JSON array: %v\n%s", err, rr.Body.String())
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 responses in batch, got %d", len(arr))
	}
}
