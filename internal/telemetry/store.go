// Package telemetry persists bounded, content-free operational aggregates.
package telemetry

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
	_ "modernc.org/sqlite"
)

const (
	HourlyRetention           = 7 * 24 * time.Hour
	DailyRetention            = 90 * 24 * time.Hour
	TargetMaxBytes      int64 = 128 << 20
	TokensEstimateLabel       = "estimate_bytes_div_4_not_billing"
	pruneEvery                = 256
)

type Config struct {
	Path string
	Now  func() time.Time
}

type Store struct {
	mu       sync.Mutex
	db       *sql.DB
	now      func() time.Time
	observed uint64
}

type Snapshot struct {
	RequestCount   int64
	ToolCallCount  int64
	InputBytes     int64
	OutputBytes    int64
	HTTPDurationMS int64
	ToolDurationMS int64
	ExternalWaitMS int64
	ClientErrors   int64
	ServerErrors   int64
	UpdatedAt      int64
	Outcomes       map[string]int64
}

func Open(cfg Config) (*Store, error) {
	path := filepath.Clean(strings.TrimSpace(cfg.Path))
	if path == "." || !filepath.IsAbs(path) {
		return nil, errors.New("telemetry database path must be absolute")
	}
	if err := preparePrivateDatabasePath(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("telemetry database is unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, now: cfg.Now}
	if store.now == nil {
		store.now = time.Now
	}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ensureLifetime(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("telemetry database permissions could not be secured")
	}
	if err := store.Prune(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize() error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=32768`,
		`CREATE TABLE IF NOT EXISTS metric_buckets (
			period TEXT NOT NULL CHECK(period IN ('hourly','daily')),
			bucket_start INTEGER NOT NULL,
			project_id TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL DEFAULT '',
			request_count INTEGER NOT NULL DEFAULT 0,
			tool_call_count INTEGER NOT NULL DEFAULT 0,
			input_bytes INTEGER NOT NULL DEFAULT 0,
			output_bytes INTEGER NOT NULL DEFAULT 0,
			http_duration_ms INTEGER NOT NULL DEFAULT 0,
			tool_duration_ms INTEGER NOT NULL DEFAULT 0,
			external_wait_ms INTEGER NOT NULL DEFAULT 0,
			client_errors INTEGER NOT NULL DEFAULT 0,
			server_errors INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(period,bucket_start,project_id,task_id,transport,route,tool,outcome)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS metric_buckets_retention ON metric_buckets(period,bucket_start)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("telemetry database initialization failed")
		}
	}
	return nil
}

func (s *Store) Observe(event observability.Event) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := s.now().UTC()
	metric := metricFor(event)
	dimensions := safeDimensions(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.observeTransactional(now, dimensions, metric); err != nil {
		return err
	}
	s.observed++
	if s.observed%pruneEvery == 0 {
		return s.pruneLocked(now)
	}
	return nil
}

type dimensions struct {
	projectID string
	taskID    string
	transport string
	route     string
	tool      string
	outcome   string
}

type metric struct {
	requests, toolCalls, inputBytes, outputBytes     int64
	httpMS, toolMS, externalMS, client4xx, server5xx int64
}

func metricFor(event observability.Event) metric {
	var value metric
	if event.Name == observability.EventHTTPRequest || (event.Name == observability.EventRPCRequest && event.Transport != observability.TransportHTTP) {
		value.requests = 1
	}
	if event.Name == observability.EventRPCRequest && event.Method == observability.MethodToolsCall {
		value.toolCalls = 1
	}
	value.inputBytes = nonnegative(event.InputBytes)
	value.outputBytes = nonnegative(event.OutputBytes)
	value.httpMS = nonnegative(event.HTTPDurationMS)
	value.toolMS = nonnegative(event.ToolDurationMS)
	value.externalMS = nonnegative(event.ExternalWaitMS)
	if event.StatusCode >= 400 && event.StatusCode < 500 {
		value.client4xx = 1
	}
	if event.StatusCode >= 500 && event.StatusCode < 600 {
		value.server5xx = 1
	}
	return value
}

var opaqueIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func safeDimensions(event observability.Event) dimensions {
	return dimensions{
		projectID: safeOpaque(event.ProjectID), taskID: safeOpaque(event.TaskID),
		transport: allowed(string(event.Transport), "stdio", "http", "internal", "other"),
		route:     allowed(string(event.Route), "", "mcp", "health", "version", "console", "oauth", "other"),
		tool:      safeOpaque(event.Tool),
		outcome:   allowed(string(event.Outcome), "", "success", "accepted", "denied", "error", "cancelled", "other"),
	}
}

func safeOpaque(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	redacted, changed := policy.Redact(value)
	if changed || redacted != value || !opaqueIDPattern.MatchString(value) {
		return "redacted"
	}
	return value
}

func allowed(value string, values ...string) string {
	for _, candidate := range values {
		if value == candidate {
			return value
		}
	}
	return "other"
}

func nonnegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func (s *Store) upsert(period string, bucket time.Time, d dimensions, m metric) error {
	_, err := s.db.Exec(`INSERT INTO metric_buckets (
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

func (s *Store) Prune() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneLocked(s.now().UTC())
}

func (s *Store) pruneLocked(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM metric_buckets
		WHERE (period='hourly' AND bucket_start < ?)
		   OR (period='daily' AND bucket_start < ?)`, now.Add(-HourlyRetention).Unix(), now.Add(-DailyRetention).Unix())
	if err != nil {
		return errors.New("telemetry retention prune failed")
	}
	return nil
}

func (s *Store) Snapshot(period string) (Snapshot, error) {
	if period != "hourly" && period != "daily" {
		return Snapshot{}, errors.New("invalid telemetry period")
	}
	result := Snapshot{Outcomes: make(map[string]int64)}
	err := s.db.QueryRow(`SELECT COALESCE(SUM(request_count),0),COALESCE(SUM(tool_call_count),0),
		COALESCE(SUM(input_bytes),0),COALESCE(SUM(output_bytes),0),COALESCE(SUM(http_duration_ms),0),
		COALESCE(SUM(tool_duration_ms),0),COALESCE(SUM(external_wait_ms),0),COALESCE(SUM(client_errors),0),COALESCE(SUM(server_errors),0)
		FROM metric_buckets WHERE period=?`, period).Scan(&result.RequestCount, &result.ToolCallCount, &result.InputBytes,
		&result.OutputBytes, &result.HTTPDurationMS, &result.ToolDurationMS, &result.ExternalWaitMS, &result.ClientErrors, &result.ServerErrors)
	if err != nil {
		return Snapshot{}, errors.New("telemetry snapshot failed")
	}
	rows, err := s.db.Query(`SELECT outcome,COALESCE(SUM(request_count+tool_call_count),0) FROM metric_buckets WHERE period=? GROUP BY outcome`, period)
	if err != nil {
		return Snapshot{}, errors.New("telemetry outcomes failed")
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count int64
		if err := rows.Scan(&outcome, &count); err != nil {
			return Snapshot{}, errors.New("telemetry outcomes failed")
		}
		result.Outcomes[outcome] += count
	}
	return result, rows.Err()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func preparePrivateDatabasePath(path string) error {
	directory := filepath.Dir(path)
	if err := rejectSymlinkAncestors(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return errors.New("telemetry directory is unavailable")
		}
		info, err = os.Lstat(directory)
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("telemetry directory is not private")
	}
	if fileInfo, err := os.Lstat(path); err == nil {
		if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
			return errors.New("telemetry database is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("telemetry database is unavailable")
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("telemetry directory ancestry is unsafe")
		}
	}
	return nil
}
