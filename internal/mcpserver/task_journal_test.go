package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	request := httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sandbox_status","arguments":{}}}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
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
	if catalog.ToolCount != 67 || catalog.Hash != "sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed" {
		t.Fatalf("catalog changed: %+v", catalog)
	}
}
