package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
)

func catalogEnv() map[string]string {
	previous := catalogrollout.Identity{Commit: strings.Repeat("a", 40), ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("1", 64)}
	candidate := catalogrollout.Identity{Commit: strings.Repeat("b", 40), ProtocolVersion: "2024-11-05", ToolCount: 138, CatalogHash: "sha256:" + strings.Repeat("2", 64)}
	body, _ := json.Marshal(catalogrollout.Request{RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Previous: previous, Candidate: candidate})
	return map[string]string{
		coolifyURLEnv: "https://coolify.example", coolifyTokenEnv: "token",
		coordinatorAppEnv: "coord1", frontAppEnv: "front1", backendAppEnv: "backend1",
		frontCommitEnv: strings.Repeat("f", 40), backendCommitEnv: previous.Commit,
		expectedProtocolEnv: previous.ProtocolVersion, expectedCatalogEnv: previous.CatalogHash,
		catalogRequestEnv: string(body), catalogMCPTokenEnv: "mcp-token",
	}
}

func TestLoadCatalogRuntimeConfigIsStrictAndBoundToPreviousIdentity(t *testing.T) {
	env := catalogEnv()
	getenv := func(key string) string { return env[key] }
	config, err := loadCatalogRuntimeConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.Request.Candidate.ToolCount != 138 || config.MCPToken != "mcp-token" || config.StateRoot != defaultStateRoot+"/catalog-rollout" || !catalogRolloutRequested(getenv) {
		t.Fatalf("config=%+v", config)
	}
	for name, mutate := range map[string]func(map[string]string){
		"previous mismatch": func(values map[string]string) { values[backendCommitEnv] = strings.Repeat("c", 40) },
		"missing token": func(values map[string]string) { values[catalogMCPTokenEnv] = "" },
		"trailing JSON": func(values map[string]string) { values[catalogRequestEnv] += "{}" },
		"topology target": func(values map[string]string) {
			values[targetEnv] = "cutover"
			values[requestIDEnv] = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := catalogEnv()
			mutate(copy)
			if _, err := loadCatalogRuntimeConfig(func(key string) string { return copy[key] }); err == nil {
				t.Fatal("invalid catalog runtime config accepted")
			}
		})
	}
}
