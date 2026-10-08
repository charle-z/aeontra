package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskCreationIsIdempotentAndBoundToActiveDevice(t *testing.T) {
	store, now, device := openTaskTestStore(t)
	spec := validTaskSpec("build-portfolio-001")
	first, created, err := store.CreateTask(device.ID, spec)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	second, created, err := store.CreateTask(device.ID, spec)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}
	changed := spec
	changed.Objective.Summary = "different objective"
	if _, _, err := store.CreateTask(device.ID, changed); err == nil {
		t.Fatal("idempotency key accepted different task body")
	}
	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	newSpec := validTaskSpec("build-portfolio-002")
	if _, _, err := store.CreateTask(device.ID, newSpec); err == nil {
		t.Fatal("task accepted for revoked device")
	}
	_ = now
}

func TestLeaseReconnectHeartbeatCompletionAndReplayAreIdempotent(t *testing.T) {
	store, now, device := openTaskTestStore(t)
	created, _, err := store.CreateTask(device.ID, validTaskSpec("validate-portfolio-001"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(device.ID, "development", "agent-session-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Task.ID != created.ID || lease.LeaseID == "" || lease.Task.IdempotencyKey == "" {
		t.Fatalf("lease=%+v", lease)
	}
	reconnect, err := store.LeaseNext(device.ID, "development", "agent-session-0001", time.Minute)
	if err != nil || reconnect.LeaseID != lease.LeaseID {
		t.Fatalf("reconnect=%+v err=%v", reconnect, err)
	}
	status, err := store.Heartbeat(device.ID, created.ID, lease.LeaseID, time.Minute)
	if err != nil || status.CancelRequested || !status.LeaseExpiresAt.After(*now) {
		t.Fatalf("heartbeat=%+v err=%v", status, err)
	}
	result := TaskResult{Outcome: OutcomeSucceeded, Summary: "all gates passed", ResultRef: "rs_0123456789abcdef0123456789abcdef"}
	completed, err := store.Complete(device.ID, created.ID, lease.LeaseID, result)
	if err != nil || completed.State != TaskSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	replayed, err := store.Complete(device.ID, created.ID, lease.LeaseID, result)
	if err != nil || replayed.State != TaskSucceeded {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	conflict := result
	conflict.Summary = "different"
	if _, err := store.Complete(device.ID, created.ID, lease.LeaseID, conflict); err == nil {
		t.Fatal("terminal result replay accepted different content")
	}
}

func TestExpiredLeaseRedeliversSameIdempotencyKeyWithoutConcurrentLease(t *testing.T) {
	store, now, device := openTaskTestStore(t)
	created, _, _ := store.CreateTask(device.ID, validTaskSpec("reconnect-task-001"))
	first, err := store.LeaseNext(device.ID, "development", "agent-session-0001", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNext(device.ID, "development", "agent-session-0002", time.Minute); err == nil {
		t.Fatal("concurrent lease accepted")
	}
	*now = (*now).Add(31 * time.Second)
	second, err := store.LeaseNext(device.ID, "development", "agent-session-0002", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Task.ID != created.ID || second.Task.IdempotencyKey != first.Task.IdempotencyKey || second.LeaseID == first.LeaseID || second.Attempt != 2 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if _, err := store.Complete(device.ID, created.ID, first.LeaseID, TaskResult{Outcome: OutcomeSucceeded, Summary: "stale"}); err == nil {
		t.Fatal("stale lease completed task")
	}
}

func TestExpiredTaskLeaseReturnsBehindQueuedTask(t *testing.T) {
	store, now, device := openTaskTestStore(t)
	front, _, err := store.CreateTask(device.ID, validTaskSpec("expired-front-001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNext(device.ID, "development", "agent-session-0001", MinLeaseTTL); err != nil {
		t.Fatal(err)
	}
	*now = (*now).Add(time.Second)
	behind, _, err := store.CreateTask(device.ID, validTaskSpec("queued-behind-001"))
	if err != nil {
		t.Fatal(err)
	}
	*now = (*now).Add(MinLeaseTTL + time.Second)
	next, err := store.LeaseNext(device.ID, "development", "agent-session-0002", MinLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if next.Task.ID != behind.ID {
		t.Fatalf("expired task %s was selected before queued task %s", front.ID, behind.ID)
	}
}

func TestExpiredTaskLeaseRecoveryBudgetFailsClosed(t *testing.T) {
	store, now, device := openTaskTestStore(t)
	task, _, err := store.CreateTask(device.ID, validTaskSpec("recovery-budget-001"))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxTaskLeaseAttempts; attempt++ {
		lease, leaseErr := store.LeaseNext(device.ID, "development", "agent-session-0001", MinLeaseTTL)
		if leaseErr != nil || lease.Task.ID != task.ID || lease.Attempt != attempt {
			t.Fatalf("attempt=%d lease=%+v err=%v", attempt, lease, leaseErr)
		}
		*now = (*now).Add(MinLeaseTTL + time.Second)
	}
	if _, err := store.LeaseNext(device.ID, "development", "agent-session-0002", MinLeaseTTL); !errors.Is(err, ErrNoTaskAvailable) {
		t.Fatalf("post-budget lease err=%v", err)
	}
	failed, err := store.TaskStatus(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != TaskFailed || failed.Outcome != OutcomeFailed || failed.ResultSummary != taskRecoveryExhaustedSummary {
		t.Fatalf("failed task=%+v", failed)
	}
}

func TestCancellationIsObservedAndQueuedCancellationNeverLeases(t *testing.T) {
	store, _, device := openTaskTestStore(t)
	leasedTask, _, _ := store.CreateTask(device.ID, validTaskSpec("cancel-leased-001"))
	lease, _ := store.LeaseNext(device.ID, "development", "agent-session-0001", time.Minute)
	if err := store.CancelTask(leasedTask.ID); err != nil {
		t.Fatal(err)
	}
	status, err := store.Heartbeat(device.ID, leasedTask.ID, lease.LeaseID, time.Minute)
	if err != nil || !status.CancelRequested {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	completed, err := store.Complete(device.ID, leasedTask.ID, lease.LeaseID, TaskResult{Outcome: OutcomeCancelled, Summary: "cancelled by operator"})
	if err != nil || completed.State != TaskCancelled {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}

	queued, _, _ := store.CreateTask(device.ID, validTaskSpec("cancel-queued-001"))
	if err := store.CancelTask(queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNext(device.ID, "development", "agent-session-0002", time.Minute); err == nil {
		t.Fatal("cancelled queued task was leased")
	}
}

func TestTaskCompletionRedactsSummaryBeforePersistence(t *testing.T) {
	store, _, device := openTaskTestStore(t)
	task, _, _ := store.CreateTask(device.ID, validTaskSpec("redacted-result-001"))
	lease, _ := store.LeaseNext(device.ID, "development", "agent-session-0001", time.Minute)
	completed, err := store.Complete(device.ID, task.ID, lease.LeaseID, TaskResult{
		Outcome: OutcomeFailed,
		Summary: "Authorization: Bearer secret-token-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.ResultSummary == "Authorization: Bearer secret-token-value" || completed.ResultSummary == "" {
		t.Fatalf("summary=%q", completed.ResultSummary)
	}
}

func TestTaskSpecAndLeaseInputsAreBounded(t *testing.T) {
	store, _, device := openTaskTestStore(t)
	tests := []TaskSpec{
		{},
		{IdempotencyKey: "short", Workcell: "development", Objective: Objective{Kind: ObjectiveValidate, Summary: "x"}, Restrictions: validRestrictions()},
		{IdempotencyKey: "valid-key-0001", Workcell: "security", Objective: Objective{Kind: ObjectiveValidate, Summary: "x"}, Restrictions: validRestrictions()},
		{IdempotencyKey: "valid-key-0002", Workcell: "development", Objective: Objective{Kind: ObjectiveValidate, Summary: string(make([]byte, 2049))}, Restrictions: validRestrictions()},
		{IdempotencyKey: "valid-key-0003", Workcell: "development", Objective: Objective{Kind: ObjectiveValidate, Summary: "Authorization: Bearer secret-token-value"}, Restrictions: validRestrictions()},
	}
	for index, spec := range tests {
		if _, _, err := store.CreateTask(device.ID, spec); err == nil {
			t.Fatalf("invalid spec %d accepted", index)
		}
	}
	if _, err := store.LeaseNext(device.ID, "development", "bad holder", time.Minute); err == nil {
		t.Fatal("invalid lease holder accepted")
	}
	if _, err := store.LeaseNext(device.ID, "development", "agent-session-0001", 11*time.Minute); err == nil {
		t.Fatal("oversized lease accepted")
	}
}

func openTaskTestStore(t *testing.T) (*Store, *time.Time, Device) {
	t.Helper()
	now := time.Date(2026, 7, 14, 21, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "wsl-development", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return store, &now, device
}

func validTaskSpec(key string) TaskSpec {
	return TaskSpec{
		IdempotencyKey: key,
		Workcell:       "development",
		Objective: Objective{
			Kind:       ObjectiveValidate,
			Summary:    "validate the checked-out project",
			Acceptance: []string{"checks pass", "no files outside workspace change"},
		},
		Restrictions: validRestrictions(),
	}
}

func validRestrictions() Restrictions {
	return Restrictions{
		Workspace:          "portfolio-charlez",
		NetworkPolicy:      NetworkNone,
		MaxDurationSeconds: 600,
		MaxOutputBytes:     262144,
	}
}
