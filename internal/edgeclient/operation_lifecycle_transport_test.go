package edgeclient

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestTransportReportsOperationProgressAndAcknowledgesCancellation(t *testing.T) {
	store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, _ := store.CreatePairing(time.Minute)
	server := httptest.NewTLSServer(edge.NewHTTPHandler(store))
	defer server.Close()
	stateRoot := filepath.Join(t.TempDir(), "client")
	identity, err := Pair(context.Background(), PairOptions{ServerURL: server.URL, Code: code, Name: "parrot-edge", StateRoot: stateRoot, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := store.CreateOperation(identity.DeviceID, edge.OperationBundleStatus, edge.OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewTransport(stateRoot, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := transport.LeaseOperation(context.Background(), time.Minute)
	if err != nil || lease == nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	control, err := transport.ReportOperationProgress(context.Background(), operation.ID, lease.LeaseID, edge.OperationProgress{Revision: 1, Phase: "running"})
	if err != nil || control.CancelRequested {
		t.Fatalf("control=%+v err=%v", control, err)
	}
	if _, err := store.RequestOperationCancel(operation.ID); err != nil {
		t.Fatal(err)
	}
	control, err = transport.ReportOperationProgress(context.Background(), operation.ID, lease.LeaseID, edge.OperationProgress{Revision: 2, Phase: "stopping"})
	if err != nil || !control.CancelRequested {
		t.Fatalf("cancel control=%+v err=%v", control, err)
	}
	cancelled, err := transport.CancelOperation(context.Background(), operation.ID, lease.LeaseID)
	if err != nil || cancelled.State != edge.OperationCancelled || cancelled.Progress.Revision != 2 {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
}
