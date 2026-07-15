package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestRunnerPersistsBeforeExecuteAndReplaysAfterCompletionDeliveryFailure(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("runner-task-0001")
	transport := &fakeEdgeTransport{lease: &lease, completeErrors: 1}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "passed"}}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute, HeartbeatInterval: time.Hour}

	if worked, err := runner.RunOnce(context.Background()); !worked || err == nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 {
		t.Fatalf("execute calls=%d", executor.calls)
	}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("replay worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 || transport.completions != 2 {
		t.Fatalf("execute=%d completions=%d", executor.calls, transport.completions)
	}
}

func TestRunnerNeverRepeatsExecutionLeftStartedByCrash(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("runner-crash-0001")
	if _, err := journal.Begin(lease.Task.IdempotencyKey, lease.Task.ID); err != nil {
		t.Fatal(err)
	}
	transport := &fakeEdgeTransport{lease: &lease}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "must not run"}}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute, HeartbeatInterval: time.Hour}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if executor.calls != 0 || transport.lastResult.Outcome != edge.OutcomeFailed {
		t.Fatalf("execute=%d result=%+v", executor.calls, transport.lastResult)
	}
}

func TestRunnerHeartbeatCancellationStopsExecutor(t *testing.T) {
	journal, _ := OpenJournal(filepath.Join(t.TempDir(), "state"))
	defer journal.Close()
	lease := runnerTestLease("runner-cancel-0001")
	transport := &fakeEdgeTransport{lease: &lease, cancelOnHeartbeat: true}
	executor := &blockingExecutor{}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute, HeartbeatInterval: time.Millisecond}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if transport.lastResult.Outcome != edge.OutcomeCancelled || executor.calls != 1 {
		t.Fatalf("result=%+v calls=%d", transport.lastResult, executor.calls)
	}
}

func TestRunnerKillSwitchPreventsLease(t *testing.T) {
	root := t.TempDir()
	stopPath := filepath.Join(root, "STOP")
	if err := os.WriteFile(stopPath, []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	transport := &fakeEdgeTransport{}
	runner := Runner{Transport: transport, StopPath: stopPath}
	if _, err := runner.RunOnce(context.Background()); !errors.Is(err, ErrKillSwitch) {
		t.Fatalf("err=%v", err)
	}
	if transport.leases != 0 {
		t.Fatalf("leases=%d", transport.leases)
	}
}

func runnerTestLease(key string) edge.Lease {
	return edge.Lease{
		Task: edge.Task{
			ID:             "et_0123456789abcdef0123456789abcdef",
			IdempotencyKey: key,
			Workcell:       "development",
			Objective:      edge.Objective{Kind: edge.ObjectiveValidate, Summary: "validate"},
			Restrictions:   edge.Restrictions{Workspace: "project", NetworkPolicy: edge.NetworkNone, MaxDurationSeconds: 60, MaxOutputBytes: 1024},
		},
		LeaseID:        "el_0123456789abcdef0123456789abcdef",
		LeaseExpiresAt: time.Now().Add(time.Minute),
		Attempt:        1,
	}
}

type fakeEdgeTransport struct {
	lease             *edge.Lease
	leases            int
	heartbeats        int
	completions       int
	completeErrors    int
	cancelOnHeartbeat bool
	lastResult        edge.TaskResult
}

func (f *fakeEdgeTransport) Lease(context.Context, string, time.Duration) (*edge.Lease, error) {
	f.leases++
	return f.lease, nil
}

func (f *fakeEdgeTransport) Heartbeat(context.Context, string, string, time.Duration) (edge.HeartbeatStatus, error) {
	f.heartbeats++
	return edge.HeartbeatStatus{LeaseExpiresAt: time.Now().Add(time.Minute), CancelRequested: f.cancelOnHeartbeat}, nil
}

func (f *fakeEdgeTransport) Complete(_ context.Context, _, _ string, result edge.TaskResult) (edge.Task, error) {
	f.completions++
	f.lastResult = result
	if f.completeErrors > 0 {
		f.completeErrors--
		return edge.Task{}, errors.New("delivery failed")
	}
	return edge.Task{State: edge.TaskSucceeded}, nil
}

type fakeExecutor struct {
	calls  int
	result edge.TaskResult
}

func (f *fakeExecutor) Execute(context.Context, edge.Task) edge.TaskResult {
	f.calls++
	return f.result
}

type blockingExecutor struct{ calls int }

func (b *blockingExecutor) Execute(ctx context.Context, _ edge.Task) edge.TaskResult {
	b.calls++
	<-ctx.Done()
	return edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "cancelled"}
}
