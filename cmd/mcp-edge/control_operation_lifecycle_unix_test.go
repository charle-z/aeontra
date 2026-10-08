//go:build !windows

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type lifecycleProgressReporter struct {
	mu       sync.Mutex
	progress []edge.OperationProgress
}

func (reporter *lifecycleProgressReporter) ReportOperationProgress(_ context.Context, _, _ string, progress edge.OperationProgress) (edge.OperationControl, error) {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.progress = append(reporter.progress, progress)
	return edge.OperationControl{CancelRequested: len(reporter.progress) >= 2}, nil
}

func (reporter *lifecycleProgressReporter) phases() []string {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	phases := make([]string, len(reporter.progress))
	for index, progress := range reporter.progress {
		phases[index] = progress.Phase
	}
	return phases
}

func TestCancelledControlOperationWaitsForExecutorBeforeReturning(t *testing.T) {
	reporter := &lifecycleProgressReporter{}
	executorCancelled := make(chan struct{})
	releaseExecutor := make(chan struct{})
	returned := make(chan struct{})
	lease := edge.OperationLease{Operation: edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef"}, LeaseID: "el_0123456789abcdef0123456789abcdef"}

	go func() {
		defer close(returned)
		_, _, cancelled, err := executeControlOperationLifecycle(context.Background(), reporter, lease, time.Millisecond, func(ctx context.Context) (edge.OperationResult, string) {
			<-ctx.Done()
			close(executorCancelled)
			<-releaseExecutor
			return edge.OperationResult{}, "operation_cancelled"
		})
		if err != nil || !cancelled {
			t.Errorf("cancelled=%t err=%v", cancelled, err)
		}
	}()

	select {
	case <-executorCancelled:
	case <-time.After(time.Second):
		t.Fatal("executor did not receive cancellation")
	}
	select {
	case <-returned:
		t.Fatal("control operation returned while its executor was still alive")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseExecutor)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("control operation did not return after executor stopped")
	}

	phases := reporter.phases()
	if len(phases) < 3 || phases[0] != "running" || phases[1] != "running" || phases[len(phases)-1] != "finalizing" {
		t.Fatalf("progress phases=%v", phases)
	}
}

func TestControlOperationPhaseClassifiesAdministrativeOperations(t *testing.T) {
	for kind, want := range map[edge.OperationKind]string{
		edge.OperationBundleUpdate:   "updating",
		edge.OperationBundleRollback: "rolling_back",
		edge.OperationEdgeRepair:     "repairing",
		edge.OperationProjectStatus:  "running",
	} {
		if got := controlOperationPhase(kind); got != want {
			t.Fatalf("kind=%s phase=%q want=%q", kind, got, want)
		}
	}
}

func TestControlOperationGateSharesNormalWorkersAndFencesGlobalMutation(t *testing.T) {
	gate := &controlOperationGate{}
	ctx := context.Background()
	readersRelease := make(chan struct{})
	readersEntered := make(chan struct{}, 4)
	var readers sync.WaitGroup
	for index := 0; index < 4; index++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			if !gate.acquire(ctx, false) {
				t.Error("normal operation failed to acquire read side")
				return
			}
			readersEntered <- struct{}{}
			<-readersRelease
			gate.release(false)
		}()
	}
	for index := 0; index < 4; index++ {
		select {
		case <-readersEntered:
		case <-time.After(time.Second):
			t.Fatal("normal workers did not overlap on the read side")
		}
	}

	globalAcquired := make(chan struct{})
	globalRelease := make(chan struct{})
	globalDone := make(chan struct{})
	go func() {
		if !gate.acquire(ctx, true) {
			t.Error("global mutation failed to acquire exclusive side")
			close(globalDone)
			return
		}
		close(globalAcquired)
		<-globalRelease
		gate.release(true)
		close(globalDone)
	}()
	select {
	case <-globalAcquired:
		t.Fatal("global mutation overlapped normal operations")
	case <-time.After(30 * time.Millisecond):
	}
	close(readersRelease)
	readers.Wait()
	select {
	case <-globalAcquired:
	case <-time.After(time.Second):
		t.Fatal("global mutation was not admitted after readers completed")
	}

	secondGlobalAcquired := make(chan struct{})
	secondGlobalDone := make(chan struct{})
	go func() {
		if !gate.acquire(ctx, true) {
			t.Error("second global mutation failed to acquire exclusive side")
			close(secondGlobalDone)
			return
		}
		close(secondGlobalAcquired)
		gate.release(true)
		close(secondGlobalDone)
	}()
	select {
	case <-secondGlobalAcquired:
		t.Fatal("two global mutations overlapped")
	case <-time.After(30 * time.Millisecond):
	}
	close(globalRelease)
	select {
	case <-globalDone:
	case <-time.After(time.Second):
		t.Fatal("first global mutation did not release")
	}
	select {
	case <-secondGlobalAcquired:
	case <-time.After(time.Second):
		t.Fatal("second global mutation was not admitted")
	}
	<-secondGlobalDone
}

func TestControlOperationGateAcquisitionHonorsContext(t *testing.T) {
	gate := &controlOperationGate{}
	if !gate.acquire(context.Background(), true) {
		t.Fatal("failed to acquire gate")
	}
	defer gate.release(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if gate.acquire(ctx, false) {
		t.Fatal("read acquisition ignored context cancellation")
	}
}
