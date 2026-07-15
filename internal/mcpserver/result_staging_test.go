package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func newResultStagingServer(t *testing.T) (*Server, *tools.Service) {
	t.Helper()
	root := t.TempDir()
	pol, err := policy.NewPolicy(config.Config{Roots: []string{root}, Mode: config.ModeAllow})
	if err != nil {
		t.Fatal(err)
	}
	store, err := resultstore.Open(resultstore.Config{Root: filepath.Join(t.TempDir(), "results")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), root).WithResultStore(store)
	return New(service), service
}

func callToolText(t *testing.T, server *Server, name string) (string, bool) {
	t.Helper()
	request := rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call"}
	request.Params, _ = json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}})
	response, _, _, _ := server.callToolObserved(request, "internal")
	encoded, _ := json.Marshal(response.Result)
	var result toolResult
	if err := json.Unmarshal(encoded, &result); err != nil || len(result.Content) != 1 {
		t.Fatalf("result=%s err=%v", encoded, err)
	}
	return result.Content[0].Text, result.IsError
}

func TestLargeToolSuccessAndFailureArePersistedAsCompactMetadata(t *testing.T) {
	server, service := newResultStagingServer(t)
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	large := strings.Repeat("payload ", 6000) + secret
	server.table["large_success"] = toolEntry{handler: func(json.RawMessage) (string, error) { return large, nil }}
	server.table["large_failure"] = toolEntry{handler: func(json.RawMessage) (string, error) { return large, errors.New("build failed") }}

	for _, name := range []string{"large_success", "large_failure"} {
		text, isError := callToolText(t, server, name)
		if (name == "large_failure") != isError || len(text) >= largeResultThresholdBytes || strings.Contains(text, secret) {
			t.Fatalf("%s compact=%d isError=%v leaked=%v", name, len(text), isError, strings.Contains(text, secret))
		}
		var metadata map[string]any
		if err := json.Unmarshal([]byte(text), &metadata); err != nil {
			t.Fatal(err)
		}
		if len(metadata) != 7 {
			t.Fatalf("metadata keys=%v", metadata)
		}
		ref, _ := metadata["result_ref"].(string)
		read, err := service.ResultRead(ref, 0, resultstore.MaxFragmentBytes)
		if err != nil || strings.Contains(read, secret) {
			t.Fatalf("persisted=%q err=%v", read, err)
		}
		if foundSecret, err := service.ResultFind(secret, 20); err != nil || foundSecret != "[]" {
			t.Fatalf("secret remained searchable: found=%s err=%v", foundSecret, err)
		}
		if foundRedaction, err := service.ResultFind("REDACTED-SECRET", 20); err != nil || !strings.Contains(foundRedaction, ref) {
			t.Fatalf("redaction not persisted: found=%s err=%v", foundRedaction, err)
		}
	}
}

func TestSmallToolOutputRemainsCompatible(t *testing.T) {
	server, _ := newResultStagingServer(t)
	server.table["small"] = toolEntry{handler: func(json.RawMessage) (string, error) { return "small output", nil }}
	text, isError := callToolText(t, server, "small")
	if isError || text != "small output" {
		t.Fatalf("text=%q isError=%v", text, isError)
	}
}
