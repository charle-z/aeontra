package edgeclient

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestStep4RunnerRejectsLeaseWithoutSafetyAuthorityBeforeExecution(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-authority-0001")
	lease.LeaseExpiresAt = time.Now().Add(5 * time.Millisecond)
	transport := &step4Transport{lease: &lease}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "must not run"}}
	runner := Runner{
		Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001",
		LeaseTTL: 100 * time.Millisecond, HeartbeatInterval: time.Millisecond,
	}

	worked, err := runner.RunOnce(context.Background())
	if !worked || err == nil || err.Error() != "edge lease authority is insufficient" {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if executor.calls != 0 || transport.leases != 1 || transport.completions != 0 {
		t.Fatalf("execute=%d leases=%d completions=%d", executor.calls, transport.leases, transport.completions)
	}
}

func TestStep4PendingDeliveryFailureBlocksNewLease(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	lease := runnerTestLease("step4-pending-block-0001")
	if _, err := journal.BeginLease(lease.Task.IdempotencyKey, lease.Task.ID, lease.Attempt, lease.LeaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishEntry(lease.Task.IdempotencyKey, lease.Task.ID, edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "pending delivery"}); err != nil {
		t.Fatal(err)
	}
	transport := &step4Transport{lease: &lease, completeErrors: 1}
	executor := &fakeExecutor{result: edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "must not run"}}
	runner := Runner{Transport: transport, Journal: journal, Executor: executor, Holder: "agent-session-0001", LeaseTTL: time.Minute}

	worked, err := runner.RunOnce(context.Background())
	if !worked || err == nil || err.Error() != "delivery failed" {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if transport.leases != 0 || executor.calls != 0 || transport.completions != 1 {
		t.Fatalf("leases=%d execute=%d completions=%d", transport.leases, executor.calls, transport.completions)
	}
}
