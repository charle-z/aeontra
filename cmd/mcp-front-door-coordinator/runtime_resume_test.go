package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

type resumeCoordinatorPlatform struct {
	mu        sync.Mutex
	topology  frontdoorcoordinator.Topology
	mutations atomic.Int64
}

func (p *resumeCoordinatorPlatform) Topology(context.Context) (frontdoorcoordinator.Topology, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.topology, nil
}

func (p *resumeCoordinatorPlatform) SetBackendDomains(_ context.Context, domains string) (string, error) {
	p.mu.Lock()
	p.topology.BackendDomains = domains
	p.mu.Unlock()
	p.mutations.Add(1)
	return "deployment-backend", nil
}

func (p *resumeCoordinatorPlatform) ConfigureFront(_ context.Context, domain, backend string) (string, error) {
	p.mu.Lock()
	p.topology.FrontDomain = domain
	p.topology.FrontBackendURL = backend
	p.mu.Unlock()
	p.mutations.Add(1)
	return "deployment-front", nil
}

func (p *resumeCoordinatorPlatform) ProbeBackend(context.Context, string) error { return nil }
func (p *resumeCoordinatorPlatform) ProbeFront(context.Context, string) error   { return nil }
func (p *resumeCoordinatorPlatform) PublishStatus(context.Context, frontdoorcoordinator.Status) error {
	p.mutations.Add(1)
	return nil
}

func TestResumeActiveCoordinatorRequestUsesDurableJournalAuthority(t *testing.T) {
	requestID := strings.Repeat("r", 43)
	base := runtimeConfig{Target: frontdoorcoordinator.TargetIdle}
	for _, state := range []frontdoorcoordinator.State{
		frontdoorcoordinator.StateQueued,
		frontdoorcoordinator.StateRunning,
		frontdoorcoordinator.StateCompensating,
	} {
		resolved := resumeActiveCoordinatorRequest(base, frontdoorcoordinator.Status{
			RequestID: requestID,
			Target:    frontdoorcoordinator.TargetCutover,
			State:     state,
		})
		if resolved.Target != frontdoorcoordinator.TargetCutover || resolved.RequestID != requestID {
			t.Fatalf("state %s did not restore durable request: %+v", state, resolved)
		}
	}

	terminal := resumeActiveCoordinatorRequest(base, frontdoorcoordinator.Status{
		RequestID: requestID,
		Target:    frontdoorcoordinator.TargetCutover,
		State:     frontdoorcoordinator.StateSucceeded,
	})
	if terminal.Target != frontdoorcoordinator.TargetIdle || terminal.RequestID != "" {
		t.Fatalf("terminal journal unexpectedly replaced idle config: %+v", terminal)
	}
}

func TestCoordinatorWaitsForRolloutThenResumesQueuedJournal(t *testing.T) {
	journal, err := frontdoorcoordinator.OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("q", 43)
	queued, err := journal.Advance(frontdoorcoordinator.Status{
		RequestID: requestID,
		Target:    frontdoorcoordinator.TargetCutover,
		State:     frontdoorcoordinator.StateQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Revision != 1 {
		t.Fatalf("queued revision=%d", queued.Revision)
	}

	platform := &resumeCoordinatorPlatform{topology: frontdoorcoordinator.Topology{
		FrontDomain:     frontdoorcoordinator.FrontTemporaryOrigin,
		FrontBackendURL: frontdoorcoordinator.BackendOrigin,
		BackendDomains:  frontdoorcoordinator.FrontPublicOrigin + "," + frontdoorcoordinator.BackendOrigin,
	}}
	start := make(chan struct{})
	baseURL, cancel, done := startCoordinatorTestServer(t, validCoordinatorEnvironment(nil), coordinatorDependencies{
		openJournal: func(string) (*frontdoorcoordinator.Journal, error) { return journal, nil },
		newPlatform: func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) { return platform, nil },
		waitForTransitionStart: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-start:
				return nil
			}
		},
	})
	defer stopCoordinatorTestServer(t, cancel, done)

	status, body := coordinatorGETUntilStatus(t, baseURL, "/readyz", 200)
	if status != 200 || !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"state":"queued"`) {
		t.Fatalf("ready status=%d body=%q", status, body)
	}
	if platform.mutations.Load() != 0 {
		t.Fatalf("transition mutated before rollout gate: %d", platform.mutations.Load())
	}

	close(start)
	deadline := time.Now().Add(3 * time.Second)
	for {
		persisted, readErr := journal.Read()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if persisted.State == frontdoorcoordinator.StateSucceeded {
			if persisted.RequestID != requestID || persisted.Target != frontdoorcoordinator.TargetCutover || persisted.Phase != frontdoorcoordinator.PhaseComplete {
				t.Fatalf("unexpected terminal journal: %+v", persisted)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("journal did not resume to success: %+v", persisted)
		}
		time.Sleep(10 * time.Millisecond)
	}

	topology, err := platform.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if topology.FrontDomain != frontdoorcoordinator.FrontPublicOrigin || topology.FrontBackendURL != frontdoorcoordinator.BackendOrigin || topology.BackendDomains != frontdoorcoordinator.BackendOrigin {
		t.Fatalf("unexpected final topology: %+v", topology)
	}
	if platform.mutations.Load() < 4 {
		t.Fatalf("expected publish and topology mutations, got %d", platform.mutations.Load())
	}
}

func TestCoordinatorCancellationDuringRolloutGateDoesNotMutate(t *testing.T) {
	journal, err := frontdoorcoordinator.OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("c", 43)
	if _, err := journal.Advance(frontdoorcoordinator.Status{
		RequestID: requestID,
		Target:    frontdoorcoordinator.TargetCutover,
		State:     frontdoorcoordinator.StateQueued,
	}); err != nil {
		t.Fatal(err)
	}
	platform := &resumeCoordinatorPlatform{topology: frontdoorcoordinator.Topology{
		FrontDomain:     frontdoorcoordinator.FrontTemporaryOrigin,
		FrontBackendURL: frontdoorcoordinator.BackendOrigin,
		BackendDomains:  frontdoorcoordinator.FrontPublicOrigin + "," + frontdoorcoordinator.BackendOrigin,
	}}
	baseURL, cancel, done := startCoordinatorTestServer(t, validCoordinatorEnvironment(nil), coordinatorDependencies{
		openJournal: func(string) (*frontdoorcoordinator.Journal, error) { return journal, nil },
		newPlatform: func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) { return platform, nil },
		waitForTransitionStart: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	status, body := coordinatorGETUntilStatus(t, baseURL, "/readyz", 200)
	if status != 200 || !strings.Contains(body, `"state":"queued"`) {
		t.Fatalf("ready status=%d body=%q", status, body)
	}
	stopCoordinatorTestServer(t, cancel, done)
	persisted, err := journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != frontdoorcoordinator.StateQueued || persisted.Revision != 1 || persisted.RequestID != requestID {
		t.Fatalf("cancellation changed durable request: %+v", persisted)
	}
	if platform.mutations.Load() != 0 {
		t.Fatalf("cancellation mutated platform: %d", platform.mutations.Load())
	}
}
