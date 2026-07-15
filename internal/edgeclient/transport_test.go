package edgeclient

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestSignedTransportLeasesHeartbeatsAndCompletesTask(t *testing.T) {
	store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, _ := store.CreatePairing(time.Minute)
	server := httptest.NewTLSServer(edge.NewHTTPHandler(store))
	defer server.Close()
	stateRoot := filepath.Join(t.TempDir(), "client")
	identity, err := Pair(context.Background(), PairOptions{ServerURL: server.URL, Code: code, Name: "wsl-development", StateRoot: stateRoot, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := store.CreateTask(identity.DeviceID, edge.TaskSpec{
		IdempotencyKey: "transport-task-0001",
		Workcell:       "development",
		Objective:      edge.Objective{Kind: edge.ObjectiveValidate, Summary: "validate project", Acceptance: []string{"checks pass"}},
		Restrictions:   edge.Restrictions{Workspace: "project", NetworkPolicy: edge.NetworkNone, MaxDurationSeconds: 600, MaxOutputBytes: 262144},
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewTransport(stateRoot, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := transport.Lease(context.Background(), "agent-session-0001", time.Minute)
	if err != nil || lease == nil || lease.Task.ID != task.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	status, err := transport.Heartbeat(context.Background(), task.ID, lease.LeaseID, time.Minute)
	if err != nil || status.CancelRequested {
		t.Fatalf("heartbeat=%+v err=%v", status, err)
	}
	completed, err := transport.Complete(context.Background(), task.ID, lease.LeaseID, edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "passed"})
	if err != nil || completed.State != edge.TaskSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	lease, err = transport.Lease(context.Background(), "agent-session-0001", time.Minute)
	if err != nil || lease != nil {
		t.Fatalf("empty lease=%+v err=%v", lease, err)
	}
}
