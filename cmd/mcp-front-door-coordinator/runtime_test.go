package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

type fakeCoordinatorPlatform struct {
	topology    frontdoorcoordinator.Topology
	topologyErr error
	mutations   atomic.Int64
}

func (p *fakeCoordinatorPlatform) Topology(context.Context) (frontdoorcoordinator.Topology, error) {
	return p.topology, p.topologyErr
}
func (p *fakeCoordinatorPlatform) SetBackendDomains(context.Context, string) (string, error) {
	p.mutations.Add(1)
	return "", nil
}
func (p *fakeCoordinatorPlatform) ConfigureFront(context.Context, string, string) (string, error) {
	p.mutations.Add(1)
	return "", nil
}
func (p *fakeCoordinatorPlatform) ProbeBackend(context.Context, string) error { return nil }
func (p *fakeCoordinatorPlatform) ProbeFront(context.Context, string) error   { return nil }
func (p *fakeCoordinatorPlatform) PublishStatus(context.Context, frontdoorcoordinator.Status) error {
	p.mutations.Add(1)
	return nil
}

func validCoordinatorEnvironment(overrides map[string]string) func(string) string {
	values := map[string]string{
		coolifyURLEnv:       "https://control.example",
		coolifyTokenEnv:     "test-token-value",
		coordinatorAppEnv:   "coord1",
		frontAppEnv:         "front1",
		backendAppEnv:       "backend1",
		frontCommitEnv:      strings.Repeat("a", 40),
		backendCommitEnv:    strings.Repeat("b", 40),
		expectedProtocolEnv: "2024-11-05",
		expectedCatalogEnv:  "sha256:" + strings.Repeat("c", 64),
		targetEnv:           string(frontdoorcoordinator.TargetIdle),
		stateRootEnv:        defaultStateRoot,
	}
	for key, value := range overrides {
		values[key] = value
	}
	return envReader(values)
}

func startCoordinatorTestServer(t *testing.T, getenv func(string) string, dependencies coordinatorDependencies) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveCoordinator(ctx, listener, getenv, dependencies) }()
	return "http://" + listener.Addr().String(), cancel, done
}

func coordinatorGET(t *testing.T, baseURL, path string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, err := client.Get(baseURL + path)
		if err == nil {
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			return response.StatusCode, string(data)
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stopCoordinatorTestServer(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("coordinator did not stop after cancellation")
	}
}

func TestCoordinatorInitializationFailureKeepsSanitizedLiveness(t *testing.T) {
	secret := "token-value request-id /private/path"
	baseURL, cancel, done := startCoordinatorTestServer(t, validCoordinatorEnvironment(nil), coordinatorDependencies{
		openJournal: func(string) (*frontdoorcoordinator.Journal, error) { return nil, errors.New(secret) },
		newPlatform: func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) {
			t.Fatal("platform client must not be constructed after journal failure")
			return nil, nil
		},
	})
	defer stopCoordinatorTestServer(t, cancel, done)

	if status, body := coordinatorGET(t, baseURL, "/healthz"); status != http.StatusOK || body != "ok mcp-front-door-coordinator\n" {
		t.Fatalf("health status=%d body=%q", status, body)
	}
	status, body := coordinatorGET(t, baseURL, "/readyz")
	if status != http.StatusServiceUnavailable || !strings.Contains(body, `"code":"journal_open_failed"`) {
		t.Fatalf("ready status=%d body=%q", status, body)
	}
	status, body = coordinatorGET(t, baseURL, "/status")
	if status != http.StatusOK || !strings.Contains(body, `"code":"journal_open_failed"`) {
		t.Fatalf("status endpoint status=%d body=%q", status, body)
	}
	for _, forbidden := range []string{"token-value", "request-id", "/private/path"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("readiness exposed %q: %s", forbidden, body)
		}
	}
}

func TestCoordinatorTopologyFailureDoesNotAdvanceJournalOrMutate(t *testing.T) {
	journal, err := frontdoorcoordinator.OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakeCoordinatorPlatform{topologyErr: errors.New("raw upstream response with token")}
	baseURL, cancel, done := startCoordinatorTestServer(t, validCoordinatorEnvironment(nil), coordinatorDependencies{
		openJournal: func(string) (*frontdoorcoordinator.Journal, error) { return journal, nil },
		newPlatform: func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) { return platform, nil },
	})
	defer stopCoordinatorTestServer(t, cancel, done)

	status, body := coordinatorGET(t, baseURL, "/readyz")
	if status != http.StatusServiceUnavailable || !strings.Contains(body, `"code":"topology_validation_failed"`) {
		t.Fatalf("ready status=%d body=%q", status, body)
	}
	if strings.Contains(body, "raw upstream") || strings.Contains(body, "token") {
		t.Fatalf("readiness exposed raw failure: %s", body)
	}
	if !strings.Contains(body, `"revision":0`) || !strings.Contains(body, `"state":"idle"`) {
		t.Fatalf("readiness omitted durable idle journal: %s", body)
	}
	if platform.mutations.Load() != 0 {
		t.Fatalf("initialization executed %d mutations", platform.mutations.Load())
	}
	persisted, err := journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != 0 || persisted.State != frontdoorcoordinator.StateIdle {
		t.Fatalf("journal advanced during failed initialization: %+v", persisted)
	}
}

func TestCoordinatorReadyAndShutdownPreserveIdleJournal(t *testing.T) {
	journal, err := frontdoorcoordinator.OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakeCoordinatorPlatform{topology: frontdoorcoordinator.Topology{
		FrontDomain:     frontdoorcoordinator.FrontTemporaryOrigin,
		FrontBackendURL: frontdoorcoordinator.FrontPublicOrigin,
		BackendDomains:  frontdoorcoordinator.FrontPublicOrigin,
	}}
	baseURL, cancel, done := startCoordinatorTestServer(t, validCoordinatorEnvironment(nil), coordinatorDependencies{
		openJournal: func(string) (*frontdoorcoordinator.Journal, error) { return journal, nil },
		newPlatform: func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) { return platform, nil },
	})

	status, body := coordinatorGET(t, baseURL, "/readyz")
	if status != http.StatusOK || !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"revision":0`) {
		t.Fatalf("ready status=%d body=%q", status, body)
	}
	stopCoordinatorTestServer(t, cancel, done)
	persisted, err := journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != 0 || persisted.State != frontdoorcoordinator.StateIdle || platform.mutations.Load() != 0 {
		t.Fatalf("shutdown changed idle state: journal=%+v mutations=%d", persisted, platform.mutations.Load())
	}
}
