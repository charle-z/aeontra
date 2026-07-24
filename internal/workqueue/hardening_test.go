package workqueue

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDifferentEnqueueRespectsAllBounds(t *testing.T) {
	store := openTestStore(t, Config{MaxJobs: 10, MaxJobsPerWorkspace: 3})
	const workers = 48
	var wait sync.WaitGroup
	errorsByMessage := make(chan string, workers)
	for index := 0; index < workers; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			spec := testSpec(fmt.Sprintf("different-enqueue-%04d", index), fmt.Sprintf("workspace-%d", index%4))
			_, _, err := store.Enqueue(spec)
			if err != nil {
				errorsByMessage <- err.Error()
			}
		}()
	}
	wait.Wait()
	close(errorsByMessage)
	for message := range errorsByMessage {
		if message != "workqueue: global job bound reached" && message != "workqueue: workspace job bound reached" {
			t.Fatalf("unexpected concurrent enqueue error %q", message)
		}
	}
	jobs, err := store.List(MaxListResults)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 10 {
		t.Fatalf("jobs=%d want=10", len(jobs))
	}
	counts := map[string]int{}
	for _, job := range jobs {
		counts[job.Workspace]++
		if counts[job.Workspace] > 3 {
			t.Fatalf("workspace %q exceeded bound: %d", job.Workspace, counts[job.Workspace])
		}
	}
}

func TestDependencyOrderIsCanonicalForIdempotency(t *testing.T) {
	store := openTestStore(t, Config{})
	first, _, err := store.Enqueue(testSpec("canonical-dependency-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Enqueue(testSpec("canonical-dependency-0002", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec("canonical-child-0001", "alpha")
	spec.Dependencies = []string{second.ID, first.ID}
	created, wasCreated, err := store.Enqueue(spec)
	if err != nil || !wasCreated {
		t.Fatalf("created=%+v wasCreated=%v err=%v", created, wasCreated, err)
	}
	spec.Dependencies = []string{first.ID, second.ID}
	reused, wasCreated, err := store.Enqueue(spec)
	if err != nil || wasCreated || reused.ID != created.ID {
		t.Fatalf("reused=%+v wasCreated=%v err=%v", reused, wasCreated, err)
	}
}

func TestFutureSchemaFailsBeforeCreatingCurrentTables(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=99`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sentinel(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Root: root, ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: schema is unsupported" {
		t.Fatalf("future schema err=%v", err)
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var currentTables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('jobs','dependencies','queue_meta')`).Scan(&currentTables); err != nil {
		t.Fatal(err)
	}
	if currentTables != 0 {
		t.Fatalf("future database was mutated with %d current tables", currentTables)
	}
}

func TestUnknownSchemaZeroDatabaseFailsWithoutAdoption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Root: root, ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: schema is unsupported" {
		t.Fatalf("unknown schema zero err=%v", err)
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var currentTables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('jobs','dependencies','queue_meta')`).Scan(&currentTables); err != nil {
		t.Fatal(err)
	}
	if currentTables != 0 {
		t.Fatalf("unknown schema was adopted with %d current tables", currentTables)
	}
}

func TestReopenRejectsSemanticCorruptionAndInsecureMode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	config := Config{Root: root, ControllerID: "control-plane"}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := store.Enqueue(testSpec("corrupt-job-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE jobs SET reason='unexpected_reason' WHERE job_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(config); err == nil || err.Error() != "workqueue: semantic integrity failed" {
		t.Fatalf("semantic corruption err=%v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(config); err == nil || err.Error() != "workqueue: database path is unsafe" {
		t.Fatalf("insecure database err=%v", err)
	}
}

func TestControllerIdentityAndBackupRootsFailClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store, err := Open(Config{Root: root, ControllerID: "control-plane"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Enqueue(testSpec("controller-job-0001", "alpha")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Backup(filepath.Join(root, "backup")); err == nil || err.Error() != "workqueue: backup root is invalid" {
		t.Fatalf("overlapping backup err=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Root: root, ControllerID: "other-control"}); err == nil || err.Error() != "workqueue: controller identity conflicts" {
		t.Fatalf("controller conflict err=%v", err)
	}
}

func TestExpiredCancelledLeaseBecomesValidTerminalJob(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	job, _, err := store.Enqueue(testSpec("expired-cancel-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil || lease.Job.ID != job.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if _, err := store.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.LeaseNext("vps.build", "worker-vps-0002", time.Minute); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("post-cancel lease err=%v", err)
	}
	cancelled, found, err := store.Get(job.ID)
	if err != nil || !found || cancelled.State != StateCancelled || cancelled.Reason != ReasonCancelled || cancelled.Summary != "cancelled" || cancelled.Fence != lease.Fence || cancelled.LeaseID != "" {
		t.Fatalf("cancelled=%+v found=%v err=%v", cancelled, found, err)
	}
	if err := store.Integrity(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessCrashReleasesWriterLockAndPreservesJob(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	command := exec.Command(os.Args[0], "-test.run=^TestWorkqueueCrashHelper$")
	command.Env = append(os.Environ(), "MCP_WORKQUEUE_CRASH_ROOT="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash helper failed: %v output=%s", err, output)
	}
	store, err := Open(Config{Root: root, ControllerID: "control-plane"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.List(MaxListResults)
	if err != nil || len(jobs) != 1 || jobs[0].IdempotencyKey != "crash-helper-job-0001" {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestWorkqueueCrashHelper(t *testing.T) {
	root := os.Getenv("MCP_WORKQUEUE_CRASH_ROOT")
	if root == "" {
		return
	}
	store, err := Open(Config{Root: root, ControllerID: "control-plane"})
	if err != nil {
		os.Exit(2)
	}
	if _, _, err := store.Enqueue(testSpec("crash-helper-job-0001", "alpha")); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestResultBoundsAndSecretMaterialFailClosed(t *testing.T) {
	store := openTestStore(t, Config{})
	job, _, err := store.Enqueue(testSpec("result-bounds-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-vps-0001", time.Minute)
	if err != nil || lease.Job.ID != job.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	for _, result := range []Result{
		{Outcome: StateSucceeded, Summary: strings.Repeat("x", 2049)},
		{Outcome: StateSucceeded, Summary: "bad result ref", ResultRef: "not-a-result"},
		{Outcome: StateSucceeded, Summary: "token gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"},
	} {
		if _, err := store.Complete(job.ID, lease.ID, lease.Fence, result); err == nil || err.Error() != "workqueue: completion is invalid" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}
