package edgeclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestOperationLeaseIsServerSignedAndUpgradesP14Identity(t *testing.T) {
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
	legacy := identity
	legacy.SchemaVersion = 1
	legacy.ControlPublicKey = ""
	_, originalPrivateKey, err := LoadIdentity(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistIdentityOnly(stateRoot, legacy); err != nil {
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
	if err != nil || lease == nil || lease.Operation.ID != operation.ID || lease.ControlSignature == "" {
		t.Fatalf("signed lease=%+v err=%v", lease, err)
	}
	upgraded, upgradedPrivateKey, err := LoadIdentity(stateRoot)
	if err != nil || upgraded.SchemaVersion != 2 || upgraded.ControlPublicKey == "" || upgraded.DeviceID != identity.DeviceID || !bytes.Equal(upgradedPrivateKey, originalPrivateKey) {
		t.Fatalf("upgraded identity=%+v err=%v", upgraded, err)
	}

	second, _, err := store.CreateOperation(identity.DeviceID, edge.OperationOnboardingStatus, edge.OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	base := server.Client().Transport
	tamperingClient := *server.Client()
	tamperingClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(request)
		if err != nil || request.URL.Path != "/edge/v1/operations/lease" || response.StatusCode != http.StatusOK {
			return response, err
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		body = bytes.Replace(body, []byte(`"control_signature":"`), []byte(`"control_signature":"invalid`), 1)
		response.Body = io.NopCloser(bytes.NewReader(body))
		response.ContentLength = int64(len(body))
		return response, nil
	})
	tampered, err := NewTransport(stateRoot, &tamperingClient)
	if err != nil {
		t.Fatal(err)
	}
	if lease, err := tampered.LeaseOperation(context.Background(), time.Minute); err == nil || lease != nil {
		t.Fatalf("tampered signed operation accepted: %+v", lease)
	}
	if status, err := store.OperationStatus(second.ID); err != nil || status.State != edge.OperationLeased {
		t.Fatalf("tampered delivery changed durable operation unexpectedly: %+v %v", status, err)
	}
}
