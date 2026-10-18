package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func TestInitializeCarriesCatalogAndBuildIdentity(t *testing.T) {
	oldCommit, oldBuiltAt := buildinfo.Commit, buildinfo.BuiltAt
	buildinfo.Commit = "initialize-commit"
	buildinfo.BuiltAt = "2026-07-12T23:00:00Z"
	defer func() {
		buildinfo.Commit = oldCommit
		buildinfo.BuiltAt = oldBuiltAt
	}()

	s := stampServer(t)
	catalog, err := s.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	response := s.handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	var got struct {
		Result struct {
			ServerInfo struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Commit      string `json:"commit"`
				BuiltAt     string `json:"builtAt"`
				ToolCount   int    `json:"toolCount"`
				CatalogHash string `json:"catalogHash"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &got); err != nil {
		t.Fatal(err)
	}
	info := got.Result.ServerInfo
	if info.Name != "mcp-devbox" || info.Version != buildinfo.Version {
		t.Fatalf("unexpected server identity: %#v", info)
	}
	if info.Commit != buildinfo.Commit || info.BuiltAt != buildinfo.BuiltAt {
		t.Fatalf("unexpected build identity: %#v", info)
	}
	if info.ToolCount != catalog.ToolCount || info.CatalogHash != catalog.Hash {
		t.Fatalf("unexpected catalog identity: %#v, catalog=%#v", info, catalog)
	}
}
