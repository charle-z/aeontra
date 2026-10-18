package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type blockingProjectInspector struct{}

func (blockingProjectInspector) Inspect(ctx context.Context, _, _, _ string) (ProjectCheckoutState, error) {
	<-ctx.Done()
	return ProjectCheckoutUnsafe, ctx.Err()
}

func TestProjectDiscoveryHasOneTotalDeadline(t *testing.T) {
	roots := newProjectDiscoveryRoots(t)
	if err := os.Mkdir(filepath.Join(roots.Dev, "legacy"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
		Roots: roots, Inspector: blockingProjectInspector{}, Timeout: time.Second,
	}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
	if !projectErrorIs(err, ProjectErrorDiscoveryTimeout) {
		t.Fatalf("discovery timeout err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("discovery exceeded its total deadline: %v", elapsed)
	}
}

func TestProjectDiscoveryRejectsUnboundedTimeoutConfiguration(t *testing.T) {
	roots := newProjectDiscoveryRoots(t)
	for _, timeout := range []time.Duration{time.Millisecond, 3 * time.Minute} {
		_, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
			Roots: roots, Inspector: pathProjectInspector{states: map[string]ProjectCheckoutState{}}, Timeout: timeout,
		}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
		if !projectErrorIs(err, ProjectErrorInvalidInput) {
			t.Fatalf("unsafe timeout %v err=%v", timeout, err)
		}
	}
}
