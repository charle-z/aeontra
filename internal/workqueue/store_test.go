package workqueue

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLegalAndIllegalTransitions(t *testing.T) {
	legal := [][2]State{
		{StateBlocked, StateQueued}, {StateBlocked, StateFailed}, {StateBlocked, StateCancelled},
		{StateQueued, StateLeased}, {StateQueued, StateCancelled},
		{StateLeased, StateQueued}, {StateLeased, StateSucceeded}, {StateLeased, StateFailed}, {StateLeased, StateCancelled},
	}
	for _, pair := range legal {
		if !legalTransition(pair[0], pair[1]) {
			t.Errorf("expected legal transition %s -> %s", pair[0], pair[1])
		}
	}
	states := []State{StateBlocked, StateQueued, StateLeased, StateSucceeded, StateFailed, StateCancelled}
	for _, from := range states {
		for _, to := range states {
			want := false
			for _, pair := range legal {
				want = want || pair == [2]State{from, to}
			}
			if legalTransition(from, to) != want {
				t.Errorf("transition %s -> %s mismatch", from, to)
			}
		}
	}
}

func TestConcurrentEqualEnqueueReturnsOneJob(t *testing.T) {
	store := openTestStore(t, Config{})
	spec := testSpec("equal-enqueue-0001", "alpha")
	const workers = 20
	jobs := make(chan Job, workers)
	errs := make(chan error, workers)
	created := make(chan bool, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			job, wasCreated, err := store.Enqueue(spec)
			jobs <- job
			created <- wasCreated
			errs <- err
		}()
	}
	wait.Wait()
	close(jobs)
	close(created)
	close(errs)
	var id string
	createdCount := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for job := range jobs {
		if id == "" {
			id = job.ID
		}
		if job.ID != id {
			t.Fatalf("dedup returned different jobs %q and %q", id, job.ID)
		}
	}
	for value := range created {
		if value {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created=%d want=1", createdCount)
	}
}

func TestBoundsAndIdempotencyConflict(t *testing.T) {
	store := openTestStore(t, Config{MaxJobs: 3, MaxJobsPerWorkspace: 2})
	first, _, err := store.Enqueue(testSpec("bounds-job-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	conflict := testSpec("bounds-job-0001", "alpha")
	conflict.Profile = "other"
	if _, _, err := store.Enqueue(conflict); err == nil || err.Error() != "workqueue: idempotency key conflicts" {
		t.Fatalf("conflict err=%v", err)
	}
	if _, _, err := store.Enqueue(testSpec("bounds-job-0002", "alpha")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Enqueue(testSpec("bounds-job-0003", "alpha")); err == nil || err.Error() != "workqueue: workspace job bound reached" {
		t.Fatalf("workspace bound err=%v", err)
	}
	if _, _, err := store.Enqueue(testSpec("bounds-job-0003", "beta")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Enqueue(testSpec("bounds-job-0004", "gamma")); err == nil || err.Error() != "workqueue: global job bound reached" {
		t.Fatalf("global bound err=%v", err)
	}
	if got, found, err := store.Get(first.ID); err != nil || !found || got.ID != first.ID {
		t.Fatalf("get=%+v found=%v err=%v", got, found, err)
	}
}

func TestTerminalJobsDoNotConsumeQueueBounds(t *testing.T) {
	store := openTestStore(t, Config{MaxJobs: 1, MaxJobsPerWorkspace: 1})
	first, _, err := store.Enqueue(testSpec("terminal-bound-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(first.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Enqueue(testSpec("terminal-bound-0002", "alpha")); err != nil {
		t.Fatalf("terminal job consumed queue bound: %v", err)
	}
	if err := store.Integrity(); err != nil {
		t.Fatalf("terminal evidence exceeded integrity bound: %v", err)
	}
}

func TestLeaseFencingExpiryAndStaleCompletion(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	job, _, err := store.Enqueue(testSpec("lease-job-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Job.ID != job.ID || first.Fence != 1 || first.Attempt != 1 {
		t.Fatalf("first lease=%+v", first)
	}
	if _, err := store.LeaseNext("vps.build", "worker-vps-0002", time.Minute); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("second active lease err=%v", err)
	}
	now = now.Add(2 * time.Minute)
	second, err := store.LeaseNext("vps.build", "worker-vps-0002", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fence != 2 || second.Attempt != 2 || second.ID == first.ID {
		t.Fatalf("second lease=%+v", second)
	}
	result := Result{Outcome: StateSucceeded, Summary: "done"}
	if _, err := store.Complete(job.ID, first.ID, first.Fence, result); err == nil || err.Error() != "workqueue: stale fenced completion rejected" {
		t.Fatalf("stale completion err=%v", err)
	}
	completed, err := store.Complete(job.ID, second.ID, second.Fence, result)
	if err != nil || completed.State != StateSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if again, err := store.Complete(job.ID, second.ID, second.Fence, result); err != nil || again.ID != completed.ID {
		t.Fatalf("idempotent completion=%+v err=%v", again, err)
	}
}

func TestExpiredLeaseReturnsBehindQueuedWork(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	front, _, err := store.Enqueue(testSpec("expired-front-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	behind, _, err := store.Enqueue(testSpec("queued-behind-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	next, err := store.LeaseNext("vps.build", "worker-vps-0002", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.Job.ID != behind.ID {
		t.Fatalf("expired job %s was selected before queued job %s", front.ID, behind.ID)
	}
}

func TestExpiredLeaseRecoveryBudgetFailsClosed(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 13, 2, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	job, _, err := store.Enqueue(testSpec("recovery-budget-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= MaxLeaseAttempts; attempt++ {
		lease, leaseErr := store.LeaseNext("vps.build", "worker-vps-0001", MinLeaseTTL)
		if leaseErr != nil || lease.Job.ID != job.ID || lease.Attempt != attempt {
			t.Fatalf("attempt=%d lease=%+v err=%v", attempt, lease, leaseErr)
		}
		now = now.Add(MinLeaseTTL + time.Second)
	}
	if _, err := store.LeaseNext("vps.build", "worker-vps-0002", MinLeaseTTL); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("post-budget lease err=%v", err)
	}
	failed, found, err := store.Get(job.ID)
	if err != nil || !found {
		t.Fatalf("failed job unavailable: found=%t err=%v", found, err)
	}
	if failed.State != StateFailed || failed.Reason != ReasonRecoveryExhausted || failed.Outcome != StateFailed || failed.Summary != string(ReasonRecoveryExhausted) {
		t.Fatalf("failed job=%+v", failed)
	}
}

func TestLeaseNextUsesFairSchedulingAcrossWorkspaces(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 13, 5, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	alphaFirst, _, err := store.Enqueue(testSpec("fair-alpha-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Nanosecond)
	alphaSecond, _, err := store.Enqueue(testSpec("fair-alpha-0002", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Nanosecond)
	betaFirst, _, err := store.Enqueue(testSpec("fair-beta-0001", "beta"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil || first.Job.ID != alphaFirst.ID {
		t.Fatalf("first lease=%+v err=%v", first, err)
	}
	second, err := store.LeaseNext("vps.build", "worker-vps-0002", time.Minute)
	if err != nil || second.Job.ID != betaFirst.ID {
		t.Fatalf("second lease=%+v err=%v", second, err)
	}
	third, err := store.LeaseNext("vps.build", "worker-vps-0003", time.Minute)
	if err != nil || third.Job.ID != alphaSecond.ID {
		t.Fatalf("third lease=%+v err=%v", third, err)
	}
}

func TestDependenciesPropagateSuccessAndFailure(t *testing.T) {
	store := openTestStore(t, Config{})
	first, _, err := store.Enqueue(testSpec("dependency-root-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	dependentSpec := testSpec("dependency-child-0001", "alpha")
	dependentSpec.Dependencies = []string{first.ID}
	dependent, _, err := store.Enqueue(dependentSpec)
	if err != nil || dependent.State != StateBlocked {
		t.Fatalf("dependent=%+v err=%v", dependent, err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(first.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "root done"}); err != nil {
		t.Fatal(err)
	}
	resolved, found, err := store.Get(dependent.ID)
	if err != nil || !found || resolved.State != StateQueued {
		t.Fatalf("resolved=%+v found=%v err=%v", resolved, found, err)
	}

	failedRootSpec := testSpec("dependency-root-0002", "beta")
	failedRootSpec.Pool = "edge.parrot.build"
	failedRoot, _, err := store.Enqueue(failedRootSpec)
	if err != nil {
		t.Fatal(err)
	}
	failedChildSpec := testSpec("dependency-child-0002", "beta")
	failedChildSpec.Dependencies = []string{failedRoot.ID}
	failedChild, _, err := store.Enqueue(failedChildSpec)
	if err != nil {
		t.Fatal(err)
	}
	failedLease, err := store.LeaseNext("edge.parrot.build", "worker-edge-0002", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if failedLease.Job.ID != failedRoot.ID {
		t.Fatalf("leased=%s want=%s", failedLease.Job.ID, failedRoot.ID)
	}
	if _, err := store.Complete(failedRoot.ID, failedLease.ID, failedLease.Fence, Result{Outcome: StateFailed, Summary: "root failed"}); err != nil {
		t.Fatal(err)
	}
	blocked, found, err := store.Get(failedChild.ID)
	if err != nil || !found || blocked.State != StateFailed || blocked.Reason != "dependency_failed" {
		t.Fatalf("blocked=%+v found=%v err=%v", blocked, found, err)
	}
}

func TestCancelQueuedAndRunning(t *testing.T) {
	store := openTestStore(t, Config{})
	queued, _, err := store.Enqueue(testSpec("cancel-queued-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.Cancel(queued.ID)
	if err != nil || cancelled.State != StateCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	running, _, err := store.Enqueue(testSpec("cancel-running-0001", "beta"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil || lease.Job.ID != running.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	requested, err := store.Cancel(running.ID)
	if err != nil || requested.State != StateLeased || !requested.CancelRequested {
		t.Fatalf("requested=%+v err=%v", requested, err)
	}
	status, err := store.Heartbeat(running.ID, lease.ID, lease.Fence, time.Minute)
	if err != nil || !status.CancelRequested {
		t.Fatalf("heartbeat=%+v err=%v", status, err)
	}
	if _, err := store.Complete(running.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "ignored cancel"}); err == nil {
		t.Fatal("running cancellation accepted successful result")
	}
	final, err := store.Complete(running.ID, lease.ID, lease.Fence, Result{Outcome: StateCancelled, Summary: "cancelled"})
	if err != nil || final.State != StateCancelled {
		t.Fatalf("final=%+v err=%v", final, err)
	}
}

func TestReopenIntegritySingleWriterAndUnsupportedWriters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	config := Config{Root: root, ControllerID: "control-plane", Writers: 1}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := store.Enqueue(testSpec("reopen-job-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(config); err == nil || err.Error() != "workqueue: writer lock is already held" {
		t.Fatalf("second writer err=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Integrity(); err != nil {
		t.Fatal(err)
	}
	if got, found, err := reopened.Get(job.ID); err != nil || !found || got.ID != job.ID {
		t.Fatalf("reopened=%+v found=%v err=%v", got, found, err)
	}
	if _, err := Open(Config{Root: filepath.Join(t.TempDir(), "multi"), ControllerID: "control-plane", Writers: 2}); err == nil || err.Error() != "workqueue: configuration is invalid" {
		t.Fatalf("multi writer err=%v", err)
	}
	if _, err := reopened.List(MaxListResults + 1); err == nil {
		t.Fatal("oversized list was accepted")
	}
}

func openTestStore(t *testing.T, config Config) *Store {
	t.Helper()
	if config.Root == "" {
		config.Root = filepath.Join(t.TempDir(), "queue")
	}
	if config.ControllerID == "" {
		config.ControllerID = "control-plane"
	}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testSpec(key, workspace string) Spec {
	return Spec{
		IdempotencyKey: key,
		Workspace:      workspace,
		Pool:           "vps.build",
		Profile:        "build-heavy",
		PayloadHash:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func FuzzValidateSpec(f *testing.F) {
	f.Add("valid-key-0001", "alpha", "vps.build", "build-heavy")
	f.Fuzz(func(t *testing.T, key, workspace, pool, profile string) {
		_ = validateSpec(Spec{IdempotencyKey: key, Workspace: workspace, Pool: pool, Profile: profile, PayloadHash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	})
}
