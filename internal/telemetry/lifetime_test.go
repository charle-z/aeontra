package telemetry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestLifetimeAndRealWindowsSurviveReopenWithoutDuplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	observe := func(input, output int64) {
		t.Helper()
		if err := store.Observe(observability.Event{
			Name: observability.EventRPCRequest, Method: observability.MethodToolsCall,
			Transport: observability.TransportInternal, Outcome: observability.OutcomeSuccess,
			InputBytes: input, OutputBytes: output, ExternalWaitMS: 7,
		}); err != nil {
			t.Fatal(err)
		}
	}
	observe(400, 200)
	now = now.Add(-48 * time.Hour)
	observe(800, 400)
	now = now.Add(-8 * 24 * time.Hour)
	observe(1600, 800)
	now = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	activity, err := store.Activity()
	if err != nil {
		t.Fatal(err)
	}
	if activity.Last24Hours.RequestCount != 1 || activity.Last7Days.RequestCount != 2 || activity.Last30Days.RequestCount != 3 {
		t.Fatalf("activity=%+v", activity)
	}
	if activity.Lifetime.RequestCount != 3 || activity.Lifetime.ToolCallCount != 3 || activity.Lifetime.InputBytes != 2800 || activity.Lifetime.OutputBytes != 1400 || activity.Lifetime.ExternalWaitMS != 21 {
		t.Fatalf("lifetime=%+v", activity.Lifetime)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	lifetime, err := reopened.Lifetime()
	if err != nil || lifetime.RequestCount != 3 || lifetime.InputBytes != 2800 {
		t.Fatalf("reopened lifetime=%+v err=%v", lifetime, err)
	}
}

func TestLifetimeUpdateRollsBackWithBucketFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`DROP TABLE metric_buckets`); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(observability.Event{Name: observability.EventHTTPRequest, Transport: observability.TransportHTTP}); err == nil {
		t.Fatal("observation unexpectedly succeeded")
	}
	lifetime, err := store.Lifetime()
	if err != nil || lifetime.RequestCount != 0 {
		t.Fatalf("lifetime changed after rollback: %+v err=%v", lifetime, err)
	}
}
