package grantadmin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carbe/mcp-devbox/internal/audit"
	"github.com/carbe/mcp-devbox/internal/config"
	"github.com/carbe/mcp-devbox/internal/policy"
)

func TestHandler_ApproveGrantRequiresAdminToken(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, ".env")
	req, err := pol.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	var auditBuf bytes.Buffer
	h := Handler(pol, audit.New(&auditBuf), "admin-token")

	body := strings.NewReader(`{"ttl":"1m","raw":false}`)
	unauth := httptest.NewRequest(http.MethodPost, DefaultPath+"/"+req.ID, body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, unauth)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized approval code = %d, want 401", rr.Code)
	}

	body = strings.NewReader(`{"ttl":"1m","raw":false}`)
	approved := httptest.NewRequest(http.MethodPost, DefaultPath+"/"+req.ID, body)
	approved.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, approved)
	if rr.Code != http.StatusOK {
		t.Fatalf("approved code = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		RequestID string `json:"request_id"`
		Raw       bool   `json:"raw"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestID != req.ID || payload.Raw {
		t.Fatalf("bad approval payload: %#v", payload)
	}

	if _, err := pol.ConsumeReadGrant(req.ID, secret, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditBuf.String(), `"tool":"access_grant"`) ||
		!strings.Contains(auditBuf.String(), `"decision":"allow"`) {
		t.Fatalf("approval should be audited, got %s", auditBuf.String())
	}
}

func TestParseTTL(t *testing.T) {
	got, err := parseTTL("2m")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2*time.Minute {
		t.Fatalf("ttl = %v", got)
	}
	if _, err := parseTTL("500ms"); err == nil {
		t.Fatal("sub-second ttl should be rejected")
	}
}
