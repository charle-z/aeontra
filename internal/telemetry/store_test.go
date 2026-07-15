package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestStorePersistsExactHourlyAndDailyMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	fixed := time.Date(2026, 7, 14, 15, 16, 17, 0, time.UTC)
	store, err := Open(Config{Path: path, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	events := []observability.Event{
		{Name: observability.EventHTTPRequest, Transport: observability.TransportHTTP, Route: observability.RouteMCP, Outcome: observability.OutcomeSuccess, StatusCode: 204, HTTPDurationMS: 12},
		{Name: observability.EventRPCRequest, Transport: observability.TransportHTTP, Method: observability.MethodToolsCall, Tool: "repo_status", Outcome: observability.OutcomeError, InputBytes: 120, OutputBytes: 80, ToolDurationMS: 9, ExternalWaitMS: 4},
		{Name: observability.EventHTTPRequest, Transport: observability.TransportHTTP, Route: observability.RouteMCP, Outcome: observability.OutcomeError, StatusCode: 503, HTTPDurationMS: 7},
	}
	for _, event := range events {
		if err := store.Observe(event); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.Snapshot("hourly")
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestCount != 2 || got.ToolCallCount != 1 || got.InputBytes != 120 || got.OutputBytes != 80 || got.HTTPDurationMS != 19 || got.ToolDurationMS != 9 || got.ExternalWaitMS != 4 || got.ServerErrors != 1 {
		t.Fatalf("hourly metrics = %+v", got)
	}
	if got.Outcomes["success"] != 1 || got.Outcomes["error"] != 2 || got.ClientErrors != 0 {
		t.Fatalf("outcomes = %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("telemetry dir mode = %o", info.Mode().Perm())
	}
}

func TestStorePrunesRetentionAndRejectsUnsafeDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	store, err := Open(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	if err := store.Observe(observability.Event{Name: observability.EventRPCRequest, Transport: observability.Transport(secret), Tool: secret, Outcome: observability.Outcome(secret)}); err != nil {
		t.Fatal(err)
	}
	queryRows, err := store.db.Query(`SELECT project_id||'|'||task_id||'|'||transport||'|'||route||'|'||tool||'|'||outcome FROM metric_buckets`)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for queryRows.Next() {
		var value string
		if err := queryRows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value)
	}
	_ = queryRows.Close()
	if strings.Contains(strings.Join(rows, " "), secret) {
		t.Fatalf("unsafe dimension persisted: %v", rows)
	}
	if err := store.upsert("hourly", now.Add(-8*24*time.Hour), dimensions{}, metric{}); err != nil {
		t.Fatal(err)
	}
	if err := store.upsert("daily", now.Add(-91*24*time.Hour), dimensions{}, metric{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Prune(); err != nil {
		t.Fatal(err)
	}
	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM metric_buckets WHERE
		(period='hourly' AND bucket_start < ?) OR (period='daily' AND bucket_start < ?)`,
		now.Add(-HourlyRetention).Unix(), now.Add(-DailyRetention).Unix()).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("expired count=%d err=%v", count, err)
	}
}

func TestOpenRejectsSymlinkAndLabelsTokenEstimate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "metrics.db")
	if err := os.Symlink(target, link); err == nil {
		if _, err := Open(Config{Path: link}); err == nil {
			t.Fatal("symlink database must be rejected")
		}
	}
	if TokensEstimateLabel != "estimate_bytes_div_4_not_billing" {
		t.Fatalf("ambiguous token estimate label %q", TokensEstimateLabel)
	}
}

func TestDatabasePageLimitTargets128MiB(t *testing.T) {
	store, err := Open(Config{Path: filepath.Join(t.TempDir(), "telemetry", "metrics.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var pageSize, maxPages int64
	if err := store.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`PRAGMA max_page_count`).Scan(&maxPages); err != nil {
		t.Fatal(err)
	}
	if pageSize*maxPages > TargetMaxBytes || pageSize*maxPages < TargetMaxBytes-(1<<20) {
		t.Fatalf("database cap = %d bytes", pageSize*maxPages)
	}
}
