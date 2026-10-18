package telemetry

import (
	"database/sql"
	"os"
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

func openLegacyTelemetryStore(t *testing.T, path string, now time.Time) *Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, now: func() time.Time { return now }}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return store
}

func TestOpenBackfillsLegacyDailyBucketsOnceWithoutDoubleCounting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	legacy := openLegacyTelemetryStore(t, path, now)
	bucket := now.Add(-48 * time.Hour)
	daily := metric{
		requests: 3, toolCalls: 4, inputBytes: 300, outputBytes: 120,
		httpMS: 11, toolMS: 7, externalMS: 5, client4xx: 1, server5xx: 2,
	}
	hourly := metric{
		requests: 5, toolCalls: 2, inputBytes: 250, outputBytes: 140,
		httpMS: 9, toolMS: 8, externalMS: 4, client4xx: 3, server5xx: 1,
	}
	if err := upsertMetric(legacy.db, "daily", time.Date(bucket.Year(), bucket.Month(), bucket.Day(), 0, 0, 0, 0, time.UTC), dimensions{}, daily); err != nil {
		t.Fatal(err)
	}
	if err := upsertMetric(legacy.db, "hourly", bucket.Truncate(time.Hour), dimensions{}, hourly); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	lifetime, err := store.Lifetime()
	if err != nil {
		t.Fatal(err)
	}
	if lifetime.RequestCount != 5 || lifetime.ToolCallCount != 4 || lifetime.InputBytes != 300 || lifetime.OutputBytes != 140 ||
		lifetime.HTTPDurationMS != 11 || lifetime.ToolDurationMS != 8 || lifetime.ExternalWaitMS != 5 ||
		lifetime.ClientErrors != 3 || lifetime.ServerErrors != 2 {
		t.Fatalf("backfilled lifetime=%+v", lifetime)
	}
	if err := store.Observe(observability.Event{
		Name: observability.EventRPCRequest, Method: observability.MethodToolsCall,
		Transport: observability.TransportInternal, Outcome: observability.OutcomeSuccess,
		InputBytes: 40, OutputBytes: 20, ExternalWaitMS: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	lifetime, err = reopened.Lifetime()
	if err != nil {
		t.Fatal(err)
	}
	if lifetime.RequestCount != 6 || lifetime.ToolCallCount != 5 || lifetime.InputBytes != 340 || lifetime.OutputBytes != 160 || lifetime.ExternalWaitMS != 8 {
		t.Fatalf("reopened lifetime duplicated or lost data: %+v", lifetime)
	}
}

func TestOpenRaisesOnlyMissingLifetimeCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	legacy := openLegacyTelemetryStore(t, path, now)
	bucket := now.Add(-24 * time.Hour)
	retained := metric{
		requests: 5, toolCalls: 2, inputBytes: 500, outputBytes: 200,
		httpMS: 10, toolMS: 8, externalMS: 4, client4xx: 3, server5xx: 1,
	}
	if err := upsertMetric(legacy.db, "daily", time.Date(bucket.Year(), bucket.Month(), bucket.Day(), 0, 0, 0, 0, time.UTC), dimensions{}, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec(`CREATE TABLE lifetime_metrics (
		id INTEGER PRIMARY KEY CHECK(id=1),request_count INTEGER NOT NULL DEFAULT 0,
		tool_call_count INTEGER NOT NULL DEFAULT 0,input_bytes INTEGER NOT NULL DEFAULT 0,
		output_bytes INTEGER NOT NULL DEFAULT 0,http_duration_ms INTEGER NOT NULL DEFAULT 0,
		tool_duration_ms INTEGER NOT NULL DEFAULT 0,external_wait_ms INTEGER NOT NULL DEFAULT 0,
		client_errors INTEGER NOT NULL DEFAULT 0,server_errors INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec(`INSERT INTO lifetime_metrics(
		id,request_count,tool_call_count,input_bytes,output_bytes,http_duration_ms,
		tool_duration_ms,external_wait_ms,client_errors,server_errors,updated_at)
		VALUES(1,2,9,900,50,20,1,99,0,7,?)`, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lifetime, err := store.Lifetime()
	if err != nil {
		t.Fatal(err)
	}
	if lifetime.RequestCount != 5 || lifetime.ToolCallCount != 9 || lifetime.InputBytes != 900 || lifetime.OutputBytes != 200 ||
		lifetime.HTTPDurationMS != 20 || lifetime.ToolDurationMS != 8 || lifetime.ExternalWaitMS != 99 ||
		lifetime.ClientErrors != 3 || lifetime.ServerErrors != 7 || lifetime.UpdatedAt != now.Unix() {
		t.Fatalf("monotonic backfill=%+v", lifetime)
	}
}

func TestOpenBackfillsHourlyOnlyLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	legacy := openLegacyTelemetryStore(t, path, now)
	if err := upsertMetric(legacy.db, "hourly", now.Add(-time.Hour), dimensions{}, metric{requests: 2, inputBytes: 64}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lifetime, err := store.Lifetime()
	if err != nil || lifetime.RequestCount != 2 || lifetime.InputBytes != 64 {
		t.Fatalf("hourly fallback=%+v err=%v", lifetime, err)
	}
}

func TestOpenBackfillsCommittedWALInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	legacy := openLegacyTelemetryStore(t, path, now)
	defer legacy.Close()
	if _, err := legacy.db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec("CREATE TABLE migration_sentinel(value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec("INSERT INTO migration_sentinel(value) VALUES('preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := upsertMetric(legacy.db, "daily", now.Add(-24*time.Hour), dimensions{}, metric{requests: 6, toolCalls: 4, inputBytes: 512}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("telemetry migration replaced metrics.db")
	}
	var sentinel string
	if err := migrated.db.QueryRow("SELECT value FROM migration_sentinel").Scan(&sentinel); err != nil || sentinel != "preserved" {
		t.Fatalf("sentinel=%q err=%v", sentinel, err)
	}
	lifetime, err := migrated.Lifetime()
	if err != nil || lifetime.RequestCount != 6 || lifetime.ToolCallCount != 4 || lifetime.InputBytes != 512 {
		t.Fatalf("WAL lifetime=%+v err=%v", lifetime, err)
	}
}

func TestOpenBackfillsBeforeRetentionPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	legacy := openLegacyTelemetryStore(t, path, now)
	old := now.Add(-120 * 24 * time.Hour)
	if err := upsertMetric(legacy.db, "daily", old, dimensions{}, metric{requests: 9, inputBytes: 900}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lifetime, err := store.Lifetime()
	if err != nil || lifetime.RequestCount != 9 || lifetime.InputBytes != 900 {
		t.Fatalf("pre-prune lifetime=%+v err=%v", lifetime, err)
	}
	var retained int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM metric_buckets").Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatalf("expired buckets retained=%d", retained)
	}
}
