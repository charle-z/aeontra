package mcpserver

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

func TestKnownToolCallIsJournaledWithoutChangingCatalog(t *testing.T) {
	server, _, _ := newObservedServer(t)
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	server.WithTaskJournal(journal)
	handler := server.HTTPHandler(testToken, nil)
	sessionID := initializeHandlerSession(t, handler, "Bearer "+testToken)
	response := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, sessionID, rpcBody(t, 2, "tools/call", map[string]any{"name": "sandbox_status", "arguments": map[string]any{}}))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	snapshot, err := journal.Snapshot(10)
	if err != nil || len(snapshot) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	entry := snapshot[0]
	if entry.Operation != "sandbox_status" || entry.Summary != "MCP tool operation: sandbox_status" || entry.State != taskjournal.StateCompleted || entry.Controller != "http" {
		t.Fatalf("entry=%+v", entry)
	}
	catalog, err := server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ToolCount != 125 || catalog.Hash != "sha256:9f1ce2ece243c1d5e821adc9b037b21b50941125292485ac43748671d13451c8" {
		t.Fatalf("catalog changed: %+v", catalog)
	}
}
