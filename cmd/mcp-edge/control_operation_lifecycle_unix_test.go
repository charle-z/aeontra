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
