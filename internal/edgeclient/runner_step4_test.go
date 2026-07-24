package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestStep4RunnerContinuesThroughTransientDisconnectAndMarksDelivery(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-offline-0001")
	transport := &step4Transport{lease: &lease, heartbeatErrors: 3}
	executor := &step4DelayedExecutor{delay: 25 * time.Millisecond, result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "finished while reconnecting"}}
	runner := Runner{
		Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001",
		LeaseTTL: time.Minute, HeartbeatInterval: time.Millisecond, OfflineGrace: 100 * time.Millisecond, ReconnectInterval: time.Millisecond,
	}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 || transport.heartbeats <= transport.heartbeatErrors || transport.lastResult.Outcome != edge.OutcomeSucceeded {
		t.Fatalf("execute=%d heartbeats=%d result=%+v", executor.calls, transport.heartbeats, transport.lastResult)
	}
	entry, err := journal.BeginAttempt(lease.Task.IdempotencyKey, lease.Task.ID, 99)
	if err != nil || entry.State != JournalCompleted || !entry.Delivered || !strings.HasPrefix(entry.ResultID, "jr_") || entry.Attempt != lease.Attempt {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}

func TestStep4RunnerOfflineGraceBlocksExecutionWithoutBlindRetry(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-offline-0002")
	transport := &step4Transport{lease: &lease, heartbeatAlwaysFails: true}
	executor := &blockingExecutor{}
	runner := Runner{
		Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001",
		LeaseTTL: time.Minute, HeartbeatInterval: time.Millisecond, OfflineGrace: 8 * time.Millisecond, ReconnectInterval: time.Millisecond,
	}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 || transport.lastResult.Outcome != edge.OutcomeFailed || transport.lastResult.Summary != "offline grace exceeded; manual reconciliation required" {
		t.Fatalf("calls=%d result=%+v", executor.calls, transport.lastResult)
	}

	secondExecutor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "must not rerun"}}
	runner.Executor = secondExecutor
	transport.heartbeatAlwaysFails = false
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("replay worked=%v err=%v", worked, err)
	}
	if secondExecutor.calls != 0 || transport.lastResult.Outcome != edge.OutcomeFailed {
		t.Fatalf("blind rerun calls=%d result=%+v", secondExecutor.calls, transport.lastResult)
	}
}

func TestStep4RunnerReplaysPendingCompletionAndMarksDelivered(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-delivery-0001")
	transport := &step4Transport{lease: &lease, completeErrors: 1}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "durable result"}}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute, HeartbeatInterval: time.Hour}

	if worked, err := runner.RunOnce(context.Background()); !worked || err == nil {
		t.Fatalf("first worked=%v err=%v", worked, err)
	}
	pending, err := journal.BeginAttempt(lease.Task.IdempotencyKey, lease.Task.ID, 2)
	if err != nil || pending.State != JournalCompleted || pending.Delivered || pending.ResultID == "" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("second worked=%v err=%v", worked, err)
	}
	delivered, err := journal.BeginAttempt(lease.Task.IdempotencyKey, lease.Task.ID, 3)
	if err != nil || !delivered.Delivered || delivered.ResultID != pending.ResultID || executor.calls != 1 {
		t.Fatalf("delivered=%+v execute=%d err=%v", delivered, executor.calls, err)
	}
}

func TestStep4RunnerReconcilesLostCompletionResponseWithoutNewLease(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-lost-response-0001")
	transport := &step4Transport{lease: &lease, loseAfterCommit: true}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "committed remotely"}}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute, HeartbeatInterval: time.Hour}

	if worked, err := runner.RunOnce(context.Background()); !worked || err == nil {
		t.Fatalf("first worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 || transport.leases != 1 || transport.completions != 1 || !transport.terminal {
		t.Fatalf("execute=%d leases=%d completions=%d terminal=%v", executor.calls, transport.leases, transport.completions, transport.terminal)
	}
	pending, err := journal.BeginAttempt(lease.Task.IdempotencyKey, lease.Task.ID, 2)
	if err != nil || pending.State != JournalCompleted || pending.Delivered || pending.LeaseID != lease.LeaseID {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	if worked, err := runner.RunOnce(context.Background()); !worked || err != nil {
		t.Fatalf("reconcile worked=%v err=%v", worked, err)
	}
	if executor.calls != 1 || transport.leases != 1 || transport.completions != 2 {
		t.Fatalf("execute=%d leases=%d completions=%d", executor.calls, transport.leases, transport.completions)
	}
	delivered, err := journal.BeginAttempt(lease.Task.IdempotencyKey, lease.Task.ID, 3)
	if err != nil || !delivered.Delivered || delivered.ResultID != pending.ResultID || delivered.LeaseID != lease.LeaseID {
		t.Fatalf("delivered=%+v err=%v", delivered, err)
	}
}

type step4Transport struct {
	lease                *edge.Lease
	heartbeatErrors      int
	heartbeatAlwaysFails bool
	completeErrors       int
	loseAfterCommit      bool
	terminal             bool
	leases               int
	mu                   sync.Mutex
	heartbeats           int
	completions          int
	lastResult           edge.TaskResult
}

func (s *step4Transport) Lease(context.Context, string, time.Duration) (*edge.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leases++
	if s.terminal {
		return nil, nil
	}
	return s.lease, nil
}

func (s *step4Transport) Heartbeat(context.Context, string, string, time.Duration) (edge.HeartbeatStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	if s.heartbeatAlwaysFails || s.heartbeats <= s.heartbeatErrors {
		return edge.HeartbeatStatus{}, errors.New("temporarily offline")
	}
	return edge.HeartbeatStatus{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *step4Transport) Complete(_ context.Context, _, _ string, result edge.TaskResult) (edge.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completions++
	s.lastResult = result
	if s.loseAfterCommit {
		s.loseAfterCommit = false
		s.terminal = true
		return edge.Task{}, errors.New("response lost after remote commit")
	}
	if s.completeErrors > 0 {
		s.completeErrors--
		return edge.Task{}, errors.New("delivery failed")
	}
	return edge.Task{State: edge.TaskSucceeded}, nil
}

type step4DelayedExecutor struct {
	delay  time.Duration
	result edge.TaskResult
	calls  int
}

func (e *step4DelayedExecutor) Execute(ctx context.Context, _ edge.Task) edge.TaskResult {
	e.calls++
	timer := time.NewTimer(e.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "cancelled"}
	case <-timer.C:
		return e.result
	}
}
