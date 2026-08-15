package workqueue

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestConcurrentDifferentEnqueueRespectsBounds(t *testing.T) {
	store := openTestStore(t, Config{MaxJobs: 8, MaxJobsPerWorkspace: 4})
	const workers = 20
	var wait sync.WaitGroup
	errorsCh := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			workspace := "alpha"
			if index%2 == 1 {
				workspace = "beta"
			}
			key := "different-job-" + twoDigits(index)
			_, _, err := store.Enqueue(testSpec(key, workspace))
			errorsCh <- err
		}(index)
	}
	wait.Wait()
	close(errorsCh)
	succeeded := 0
	for err := range errorsCh {
		if err == nil {
			succeeded++
			continue
		}
		if err.Error() != "workqueue: global job bound reached" && err.Error() != "workqueue: workspace job bound reached" {
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}
	if succeeded != 8 {
		t.Fatalf("succeeded=%d want=8", succeeded)
	}
	jobs, err := store.List(MaxListResults)
	if err != nil || len(jobs) != 8 {
		t.Fatalf("jobs=%d err=%v", len(jobs), err)
	}
	counts := map[string]int{}
	for _, job := range jobs {
		counts[job.Workspace]++
	}
	if counts["alpha"] > 4 || counts["beta"] > 4 {
		t.Fatalf("workspace bounds exceeded: %+v", counts)
	}
}

func TestDependencyOrderDoesNotChangeDedupIdentity(t *testing.T) {
	store := openTestStore(t, Config{})
	first, _, err := store.Enqueue(testSpec("dependency-order-root-01", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Enqueue(testSpec("dependency-order-root-02", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec("dependency-order-child1", "alpha")
	spec.Dependencies = []string{first.ID, second.ID}
	job, created, err := store.Enqueue(spec)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	reversed := spec
	reversed.Dependencies = []string{second.ID, first.ID}
	again, created, err := store.Enqueue(reversed)
	if err != nil || created || again.ID != job.ID {
		t.Fatalf("again=%+v created=%v err=%v", again, created, err)
	}
}

func TestOpenRejectsSymlinkDatabaseAndFutureSchema(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(target, []byte("unsafe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "queue.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Root: root, ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: database path is unsafe" {
		t.Fatalf("symlink err=%v", err)
	}

	futureRoot := filepath.Join(t.TempDir(), "future")
	if err := os.MkdirAll(futureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(futureRoot, "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=3`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Config{Root: futureRoot, ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: schema is unsupported" {
		t.Fatalf("future schema err=%v", err)
	}
}

func twoDigits(value int) string {
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
