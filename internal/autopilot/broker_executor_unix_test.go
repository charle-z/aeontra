//go:build !windows

package autopilot

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokerExecutorKeepsActionLocalAndPersistsCheckpoint(t *testing.T) {
	workspace := privateWorkspace(t)
	socket := filepath.Join(t.TempDir(), "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","workspace_id":"ws_0123456789abcdef0123456789abcdef"}`))
	})}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	executor := BrokerExecutor{SocketPath: socket, Workspace: workspace, WorkspaceID: "ws_0123456789abcdef0123456789abcdef"}
	observation, err := executor.Execute(context.Background(), LocalAgentResponse{Action: ActionStatus, Arguments: json.RawMessage(`{"workspace_id":"ws_0123456789abcdef0123456789abcdef"}`)})
	if err != nil || len(observation.ModelObservation) == 0 {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
	checkpoint, err := executor.Execute(context.Background(), LocalAgentResponse{Action: ActionCheckpointUpdate, Arguments: json.RawMessage(`{"content":"safe local checkpoint"}`)})
	if err != nil || !checkpoint.CheckpointChanged {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}
	body, err := os.ReadFile(filepath.Join(workspace, ".mcp-devbox", "CURRENT.md"))
	if err != nil || string(body) != "safe local checkpoint" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
