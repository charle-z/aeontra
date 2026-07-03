package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func bearerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestAuthorize_ValidToken(t *testing.T) {
	p := testProvider(t)
	tok := p.issueAccessToken("client-1", p.resource, "mcp", time.Hour)
	if !p.Authorize(bearerReq(tok)) {
		t.Fatal("valid, in-audience token should authorize")
	}
}

func TestAuthorize_UnknownToken(t *testing.T) {
	p := testProvider(t)
	if p.Authorize(bearerReq("nope-not-a-real-token")) {
		t.Fatal("unknown token must be rejected")
	}
}

func TestAuthorize_MissingHeader(t *testing.T) {
	p := testProvider(t)
	if p.Authorize(bearerReq("")) {
		t.Fatal("missing Authorization header must be rejected")
	}
}

func TestAuthorize_ExpiredToken(t *testing.T) {
	p := testProvider(t)
	tok := p.issueAccessToken("client-1", p.resource, "mcp", -1*time.Second) // already expired
	if p.Authorize(bearerReq(tok)) {
		t.Fatal("expired token must be rejected")
	}
}

func TestAuthorize_WrongAudience(t *testing.T) {
	p := testProvider(t)
	// Token minted for a different resource must never be accepted here (RFC 8707).
	tok := p.issueAccessToken("client-1", "https://evil.example.com/mcp", "mcp", time.Hour)
	if p.Authorize(bearerReq(tok)) {
		t.Fatal("token with wrong audience must be rejected")
	}
}

func TestAuthorize_TokenInQueryStringRejected(t *testing.T) {
	p := testProvider(t)
	tok := p.issueAccessToken("client-1", p.resource, "mcp", time.Hour)
	// A valid token supplied only via the query string must NOT authorize
	// (access tokens MUST NOT ride the URI per the MCP spec).
	r := httptest.NewRequest(http.MethodPost, "/mcp?access_token="+tok+"&key="+tok, nil)
	if p.Authorize(r) {
		t.Fatal("token in query string must be rejected (header-only)")
	}
}

func TestChallengeHeader_PointsToExactPRM(t *testing.T) {
	p := testProvider(t)
	got := p.ChallengeHeader()
	want := `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp"`
	if !strings.Contains(got, want) {
		t.Errorf("challenge = %q, must contain %q", got, want)
	}
	if !strings.HasPrefix(got, "Bearer ") {
		t.Errorf("challenge = %q, must start with Bearer", got)
	}
}
