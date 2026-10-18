package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func stampServer(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.New(config.Config{Roots: []string{t.TempDir()}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return New(tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0]))
}

func TestInitialize_CarriesCommit(t *testing.T) {
	old := buildinfo.Commit
	buildinfo.Commit = "deadbeefcafe"
	defer func() { buildinfo.Commit = old }()

	s := stampServer(t)
	resp := s.handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	var out struct {
		Result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Commit  string `json:"commit"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Result.ServerInfo.Commit != "deadbeefcafe" {
		t.Errorf("serverInfo.commit = %q, want deadbeefcafe", out.Result.ServerInfo.Commit)
	}
}

func TestHealthz_CarriesCommit(t *testing.T) {
	old := buildinfo.Commit
	buildinfo.Commit = "abc123sha"
	defer func() { buildinfo.Commit = old }()

	h := stampServer(t).HTTPHandler("tok", nil)
	rr := do(t, h, "GET", "/healthz", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("abc123sha")) {
		t.Errorf("healthz body = %q, must contain the commit", rr.Body.String())
	}
}
