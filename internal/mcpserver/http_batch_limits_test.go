package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestHTTPEmptyBatchReturnsInvalidRequest(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, http.MethodPost, "/mcp", "Bearer "+testToken, `[]`)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty batch status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"code":-32600`) {
		t.Fatalf("empty batch response = %s, want invalid request", rr.Body.String())
	}
}

func TestHTTPBatchRejectsTooManyItemsWithBoundedError(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	var batch strings.Builder
	batch.WriteByte('[')
	for i := 0; i < maxHTTPBatchItems+1; i++ {
		if i > 0 {
			batch.WriteByte(',')
		}
		batch.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	}
	batch.WriteByte(']')

	rr := do(t, h, http.MethodPost, "/mcp", "Bearer "+testToken, batch.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("oversized batch status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"code":-32600`) || !strings.Contains(rr.Body.String(), "batch too large") {
		t.Fatalf("oversized batch response = %s", rr.Body.String())
	}
	if rr.Body.Len() > 1024 {
		t.Fatalf("oversized batch error is unexpectedly large: %d bytes", rr.Body.Len())
	}
}

func TestHTTPBatchAllowsConfiguredMaximum(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	var batch strings.Builder
	batch.WriteByte('[')
	for i := 0; i < maxHTTPBatchItems; i++ {
		if i > 0 {
			batch.WriteByte(',')
		}
		batch.WriteString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	}
	batch.WriteByte(']')

	rr := do(t, h, http.MethodPost, "/mcp", "Bearer "+testToken, batch.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("max batch status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var responses []rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &responses); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(responses) != maxHTTPBatchItems {
		t.Fatalf("responses = %d, want %d", len(responses), maxHTTPBatchItems)
	}
}
