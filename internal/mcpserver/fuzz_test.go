package mcpserver

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func newFuzzServer(f *testing.F) *Server {
	f.Helper()
	root := f.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		f.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		f.Fatal(err)
	}
	return New(tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0]))
}

func FuzzJSONRPCHandleReturnsValidJSONOrNotification(f *testing.F) {
	server := newFuzzServer(f)
	for _, seed := range [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`),
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`),
		[]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`),
		[]byte(`{not json`), nil,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > maxHTTPBody {
			t.Skip()
		}
		response := server.handle(raw)
		if response != nil && !json.Valid(response) {
			t.Fatalf("invalid JSON response for %q: %q", raw, response)
		}
	})
}

func FuzzJSONRPCBatchResponsesStayBoundedAndValid(f *testing.F) {
	server := newFuzzServer(f)
	for _, seed := range [][]byte{
		[]byte(`[]`),
		[]byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`),
		[]byte(`[{"jsonrpc":"2.0","method":"notifications/initialized"}]`),
		[]byte(`[`), []byte(`not an array`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > maxHTTPBody {
			t.Skip()
		}
		responses := server.handleBatch(raw)
		if len(responses) > maxHTTPBatchItems {
			t.Fatalf("responses = %d, exceeds max %d", len(responses), maxHTTPBatchItems)
		}
		for _, response := range responses {
			if !json.Valid(response) {
				t.Fatalf("invalid batch response for %q: %q", raw, response)
			}
		}
	})
}
