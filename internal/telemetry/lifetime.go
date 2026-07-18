package telemetry

import (
	"database/sql"
	"errors"
	"time"
)

const lifetimeRowID = 1

// DurableActivity exposes actual retained windows plus an all-time accumulator.
type DurableActivity struct {
	Last24Hours Snapshot
	Last7Days   Snapshot
	Last30Days  Snapshot
	Last90Days  Snapshot
	Lifetime    Snapshot
}

func (s *Store) ensureLifetime() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS lifetime_metrics (
			id INTEGER PRIMARY KEY CHECK(id=1),
			request_count INTEGER NOT NULL DEFAULT 0,
			tool_call_count INTEGER NOT NULL DEFAULT 0,
			input_bytes INTEGER NOT NULL DEFAULT 0,
			output_bytes INTEGER NOT NULL DEFAULT 0,
			http_duration_ms INTEGER NOT NULL DEFAULT 0,
			tool_duration_ms INTEGER NOT NULL DEFAULT 0,
			external_wait_ms INTEGER NOT NULL DEFAULT 0,
			client_errors INTEGER NOT NULL DEFAULT 0,
			server_errors INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO lifetime_metrics(id) VALUES(1)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("telemetry lifetime initialization failed")
		}
	}
	return nil
}

func (s *Store) observeTransactional(now time.Time, d dimensions, m metric) error {
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("telemetry transaction failed")
	}
	defer tx.Rollback()
	for _, period := range []string{"hourly", "daily"} {
		bucket := now.Truncate(time.Hour)
		if period == "daily" {
			bucket = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		}
		if err := upsertMetric(tx, period, bucket, d, m); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE lifetime_metrics SET
		request_count=request_count+?,tool_call_count=tool_call_count+?,
		input_bytes=input_bytes+?,output_bytes=output_bytes+?,
		http_duration_ms=http_duration_ms+?,tool_duration_ms=tool_duration_ms+?,
		external_wait_ms=external_wait_ms+?,client_errors=client_errors+?,
		server_errors=server_errors+?,updated_at=? WHERE id=?`,
		m.requests, m.toolCalls, m.inputBytes, m.outputBytes, m.httpMS, m.toolMS,
		m.externalMS, m.client4xx, m.server5xx, now.Unix(), lifetimeRowID); err != nil {
		return errors.New("telemetry lifetime update failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("telemetry transaction commit failed")
	}
	return nil
}

type metricExecer interface {
	Exec(string, ...any) (sql.Result, error)
}

func upsertMetric(execer metricExecer, period string, bucket time.Time, d dimensions, m metric) error {
	_, err := execer.Exec(`INSERT INTO metric_buckets (
		period,bucket_start,project_id,task_id,transport,route,tool,outcome,
		request_count,tool_call_count,input_bytes,output_bytes,http_duration_ms,tool_duration_ms,external_wait_ms,client_errors,server_errors
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(period,bucket_start,project_id,task_id,transport,route,tool,outcome) DO UPDATE SET
		request_count=request_count+excluded.request_count,
		tool_call_count=tool_call_count+excluded.tool_call_count,
		input_bytes=input_bytes+excluded.input_bytes,
		output_bytes=output_bytes+excluded.output_bytes,
		http_duration_ms=http_duration_ms+excluded.http_duration_ms,
		tool_duration_ms=tool_duration_ms+excluded.tool_duration_ms,
		external_wait_ms=external_wait_ms+excluded.external_wait_ms,
		client_errors=client_errors+excluded.client_errors,
		server_errors=server_errors+excluded.server_errors`,
		period, bucket.Unix(), d.projectID, d.taskID, d.transport, d.route, d.tool, d.outcome,
		m.requests, m.toolCalls, m.inputBytes, m.outputBytes, m.httpMS, m.toolMS, m.externalMS, m.client4xx, m.server5xx)
	if err != nil {
		return errors.New("telemetry update failed")
	}
	return nil
}

func (s *Store) Window(window time.Duration) (Snapshot, error) {
	if s == nil || s.db == nil || window <= 0 || window > DailyRetention {
		return Snapshot{}, errors.New("invalid telemetry window")
	}
	period := "hourly"
	cutoff := s.now().UTC().Add(-window).Truncate(time.Hour)
	if window > HourlyRetention {
		period = "daily"
		value := s.now().UTC().Add(-window)
		cutoff = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	}
	return s.snapshotQuery(`FROM metric_buckets WHERE period=? AND bucket_start>=?`, period, cutoff.Unix())
}

func (s *Store) Lifetime() (Snapshot, error) {
	if s == nil || s.db == nil {
		return Snapshot{}, errors.New("telemetry store unavailable")
	}
	result := Snapshot{Outcomes: map[string]int64{}}
	err := s.db.QueryRow(`SELECT request_count,tool_call_count,input_bytes,output_bytes,
		http_duration_ms,tool_duration_ms,external_wait_ms,client_errors,server_errors,updated_at
		FROM lifetime_metrics WHERE id=?`, lifetimeRowID).Scan(
		&result.RequestCount, &result.ToolCallCount, &result.InputBytes, &result.OutputBytes,
		&result.HTTPDurationMS, &result.ToolDurationMS, &result.ExternalWaitMS,
		&result.ClientErrors, &result.ServerErrors, &result.UpdatedAt)
	if err != nil {
		return Snapshot{}, errors.New("telemetry lifetime snapshot failed")
	}
	return result, nil
}

func (s *Store) Activity() (DurableActivity, error) {
	var activity DurableActivity
	windows := []struct {
		duration time.Duration
		target   *Snapshot
	}{
		{24 * time.Hour, &activity.Last24Hours},
		{7 * 24 * time.Hour, &activity.Last7Days},
		{30 * 24 * time.Hour, &activity.Last30Days},
		{90 * 24 * time.Hour, &activity.Last90Days},
	}
	for _, item := range windows {
		snapshot, err := s.Window(item.duration)
		if err != nil {
			return DurableActivity{}, err
		}
		*item.target = snapshot
	}
	lifetime, err := s.Lifetime()
	if err != nil {
		return DurableActivity{}, err
	}
	activity.Lifetime = lifetime
	return activity, nil
}

func (s *Store) snapshotQuery(from string, args ...any) (Snapshot, error) {
	result := Snapshot{Outcomes: make(map[string]int64)}
	query := `SELECT COALESCE(SUM(request_count),0),COALESCE(SUM(tool_call_count),0),
		COALESCE(SUM(input_bytes),0),COALESCE(SUM(output_bytes),0),COALESCE(SUM(http_duration_ms),0),
		COALESCE(SUM(tool_duration_ms),0),COALESCE(SUM(external_wait_ms),0),COALESCE(SUM(client_errors),0),
		COALESCE(SUM(server_errors),0),COALESCE(MAX(bucket_start),0) ` + from
	if err := s.db.QueryRow(query, args...).Scan(&result.RequestCount, &result.ToolCallCount, &result.InputBytes,
		&result.OutputBytes, &result.HTTPDurationMS, &result.ToolDurationMS, &result.ExternalWaitMS,
		&result.ClientErrors, &result.ServerErrors, &result.UpdatedAt); err != nil {
		return Snapshot{}, errors.New("telemetry window snapshot failed")
	}
	return result, nil
}
