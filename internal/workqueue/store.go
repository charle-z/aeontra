package workqueue

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 2

type Store struct {
	root     string
	path     string
	config   Config
	db       *sql.DB
	lockFile *os.File
	mu       sync.Mutex
	now      func() time.Time
}

func Open(config Config) (*Store, error) {
	validated, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(strings.TrimSpace(validated.Root))
	if root == "." || root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("workqueue: root must be absolute")
	}
	if err := prepareRoot(root); err != nil {
		return nil, err
	}
	lockFile, err := acquireWriterLock(filepath.Join(root, "queue.lock"), validated.ControllerID)
	if err != nil {
		return nil, err
	}
	cleanupLock := true
	defer func() {
		if cleanupLock {
			releaseWriterLock(lockFile)
		}
	}()
	path := filepath.Join(root, "queue.db")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || info.Size() <= 0 || info.Size() > TargetMaxBytes {
			return nil, errors.New("workqueue: database path is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("workqueue: database path unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("workqueue: database unavailable")
	}
	db.SetMaxOpenConns(1)
	var existingVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&existingVersion); err != nil || existingVersion > schemaVersion {
		_ = db.Close()
		return nil, errors.New("workqueue: schema is unsupported")
	}
	if existingVersion == 0 {
		var existingTables int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&existingTables); err != nil || existingTables != 0 {
			_ = db.Close()
			return nil, errors.New("workqueue: schema is unsupported")
		}
	}
	store := &Store{root: root, path: path, config: validated, db: db, lockFile: lockFile, now: time.Now}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("workqueue: database permissions failed")
	}
	cleanupLock = false
	return store, nil
}

func prepareRoot(root string) error {
	clean := filepath.Clean(root)
	current := clean
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("workqueue: root ancestry is unsafe")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("workqueue: root ancestry unavailable")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New("workqueue: root unavailable")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
		return errors.New("workqueue: root is unsafe")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return errors.New("workqueue: root permissions failed")
	}
	return nil
}

func (s *Store) initialize() error {
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA wal_autocheckpoint=1000`,
		`PRAGMA max_page_count=16384`,
		`CREATE TABLE IF NOT EXISTS queue_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS jobs(
			job_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			workspace TEXT NOT NULL,
			pool TEXT NOT NULL,
			profile TEXT NOT NULL,
			payload_hash TEXT NOT NULL,
			state TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			cancel_requested INTEGER NOT NULL DEFAULT 0,
			attempt INTEGER NOT NULL DEFAULT 0,
			fence INTEGER NOT NULL DEFAULT 0,
			lease_id TEXT,
			lease_holder TEXT,
			lease_until INTEGER,
			outcome TEXT,
			summary TEXT,
			result_ref TEXT
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS jobs_pool_queue ON jobs(pool,state,created_at,job_id)`,
		`CREATE INDEX IF NOT EXISTS jobs_workspace ON jobs(workspace,created_at,job_id)`,
		`CREATE INDEX IF NOT EXISTS jobs_lease_expiry ON jobs(state,lease_until)`,
		`CREATE TABLE IF NOT EXISTS dependencies(
			job_id TEXT NOT NULL,
			dependency_id TEXT NOT NULL,
			PRIMARY KEY(job_id,dependency_id),
			FOREIGN KEY(job_id) REFERENCES jobs(job_id) ON DELETE CASCADE,
			FOREIGN KEY(dependency_id) REFERENCES jobs(job_id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS dependencies_reverse ON dependencies(dependency_id,job_id)`,
		`CREATE TABLE IF NOT EXISTS task_groups(
			task_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			project_alias TEXT NOT NULL,
			target_alias TEXT NOT NULL,
			base_commit TEXT NOT NULL,
			goal_hash TEXT NOT NULL,
			pool TEXT NOT NULL,
			profile TEXT NOT NULL,
			worker_count INTEGER NOT NULL,
			execution_timeout_seconds INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS task_workers(
			task_id TEXT NOT NULL REFERENCES task_groups(task_id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			job_id TEXT NOT NULL UNIQUE REFERENCES jobs(job_id) ON DELETE CASCADE,
			goal_ref TEXT NOT NULL,
			operation_id TEXT NOT NULL DEFAULT '',
			worktree_id TEXT NOT NULL DEFAULT '',
			workspace_id TEXT NOT NULL DEFAULT '',
			runtime_id TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(task_id,ordinal)
		) WITHOUT ROWID`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("workqueue: database initialization failed")
		}
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version > schemaVersion {
		return errors.New("workqueue: schema is unsupported")
	}
	if version < schemaVersion {
		if _, err := s.db.Exec(`PRAGMA user_version=2`); err != nil {
			return errors.New("workqueue: schema activation failed")
		}
	}
	if _, err := s.db.Exec(`INSERT INTO queue_meta(key,value) VALUES('controller_id',?) ON CONFLICT(key) DO NOTHING`, s.config.ControllerID); err != nil {
		return errors.New("workqueue: controller metadata failed")
	}
	var controller string
	if err := s.db.QueryRow(`SELECT value FROM queue_meta WHERE key='controller_id'`).Scan(&controller); err != nil || controller != s.config.ControllerID {
		return errors.New("workqueue: controller identity conflicts")
	}
	return s.Integrity()
}

func (s *Store) Enqueue(spec Spec) (Job, bool, error) {
	if s == nil || s.db == nil {
		return Job{}, false, errors.New("workqueue: store is unavailable")
	}
	if err := validateSpec(spec); err != nil {
		return Job{}, false, err
	}
	spec = canonicalSpec(spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Job{}, false, errors.New("workqueue: enqueue transaction failed")
	}
	defer tx.Rollback()
	existing, found, err := jobByIdempotency(tx, spec.IdempotencyKey)
	if err != nil {
		return Job{}, false, err
	}
	if found {
		dependencies, err := dependencyIDs(tx, existing.ID)
		if err != nil {
			return Job{}, false, err
		}
		if existing.Workspace != spec.Workspace || existing.Pool != spec.Pool || existing.Profile != spec.Profile || existing.PayloadHash != spec.PayloadHash || !sameStrings(dependencies, spec.Dependencies) {
			return Job{}, false, errors.New("workqueue: idempotency key conflicts")
		}
		return existing, false, nil
	}
	var total, workspaceCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&total); err != nil {
		return Job{}, false, errors.New("workqueue: job count unavailable")
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE workspace=?`, spec.Workspace).Scan(&workspaceCount); err != nil {
		return Job{}, false, errors.New("workqueue: workspace count unavailable")
	}
	if total >= s.config.MaxJobs {
		return Job{}, false, errors.New("workqueue: global job bound reached")
	}
	if workspaceCount >= s.config.MaxJobsPerWorkspace {
		return Job{}, false, errors.New("workqueue: workspace job bound reached")
	}
	state, reason, err := dependencyState(tx, spec.Dependencies)
	if err != nil {
		return Job{}, false, err
	}
	jobID, err := randomID("wj_")
	if err != nil {
		return Job{}, false, errors.New("workqueue: job identity unavailable")
	}
	now := s.clock().UTC()
	outcome := any(nil)
	summary := any(nil)
	if state == StateFailed {
		outcome = StateFailed
		summary = "dependency failed"
	}
	if _, err := tx.Exec(`INSERT INTO jobs(job_id,idempotency_key,workspace,pool,profile,payload_hash,state,reason,created_at,updated_at,outcome,summary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		jobID, spec.IdempotencyKey, spec.Workspace, spec.Pool, spec.Profile, spec.PayloadHash, state, reason, now.UnixNano(), now.UnixNano(), outcome, summary); err != nil {
		return Job{}, false, errors.New("workqueue: job persistence failed")
	}
	for _, dependency := range spec.Dependencies {
		if _, err := tx.Exec(`INSERT INTO dependencies(job_id,dependency_id) VALUES(?,?)`, jobID, dependency); err != nil {
			return Job{}, false, errors.New("workqueue: dependency persistence failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, errors.New("workqueue: enqueue commit failed")
	}
	job, _, err := s.getUnlocked(jobID)
	return job, true, err
}

func dependencyState(tx *sql.Tx, dependencies []string) (State, Reason, error) {
	if len(dependencies) == 0 {
		return StateQueued, ReasonNone, nil
	}
	blocked := false
	for _, dependency := range dependencies {
		job, found, err := jobByID(tx, dependency)
		if err != nil {
			return "", "", err
		}
		if !found {
			return "", "", errors.New("workqueue: dependency not found")
		}
		switch job.State {
		case StateSucceeded:
		case StateFailed, StateCancelled:
			return StateFailed, ReasonDependencyFailed, nil
		default:
			blocked = true
		}
	}
	if blocked {
		return StateBlocked, ReasonDependencyPending, nil
	}
	return StateQueued, ReasonNone, nil
}

func (s *Store) LeaseNext(pool, holder string, ttl time.Duration) (Lease, error) {
	if s == nil || s.db == nil || !poolPattern.MatchString(pool) || !holderPattern.MatchString(holder) || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return Lease{}, errors.New("workqueue: lease request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Lease{}, errors.New("workqueue: lease transaction failed")
	}
	defer tx.Rollback()
	now := s.clock().UTC()
	if err := recoverExpired(tx, now); err != nil {
		return Lease{}, err
	}
	var jobID string
	if err := tx.QueryRow(`SELECT job_id FROM jobs WHERE pool=? AND state=? AND cancel_requested=0 ORDER BY created_at,job_id LIMIT 1`, pool, StateQueued).Scan(&jobID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return Lease{}, errors.New("workqueue: lease recovery commit failed")
			}
			return Lease{}, ErrNoJobAvailable
		}
		return Lease{}, errors.New("workqueue: lease selection failed")
	}
	leaseID, err := randomID("wl_")
	if err != nil {
		return Lease{}, errors.New("workqueue: lease identity unavailable")
	}
	expires := now.Add(ttl)
	result, err := tx.Exec(`UPDATE jobs SET state=?,reason='',attempt=attempt+1,fence=fence+1,lease_id=?,lease_holder=?,lease_until=?,updated_at=? WHERE job_id=? AND state=?`,
		StateLeased, leaseID, holder, expires.UnixNano(), now.UnixNano(), jobID, StateQueued)
	if err != nil {
		return Lease{}, errors.New("workqueue: lease update failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Lease{}, errors.New("workqueue: lease lost race")
	}
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found {
		return Lease{}, errors.New("workqueue: leased job unavailable")
	}
	if err := tx.Commit(); err != nil {
		return Lease{}, errors.New("workqueue: lease commit failed")
	}
	return Lease{Job: job, ID: leaseID, Fence: job.Fence, Attempt: job.Attempt, ExpiresAt: expires}, nil
}

func recoverExpired(tx *sql.Tx, now time.Time) error {
	rows, err := tx.Query(`SELECT job_id,cancel_requested FROM jobs WHERE state=? AND lease_until<=?`, StateLeased, now.UnixNano())
	if err != nil {
		return errors.New("workqueue: expired lease scan failed")
	}
	type expired struct {
		id        string
		cancelled bool
	}
	items := make([]expired, 0)
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.id, &item.cancelled); err != nil {
			_ = rows.Close()
			return errors.New("workqueue: expired lease result failed")
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return errors.New("workqueue: expired lease scan failed")
	}
	for _, item := range items {
		state := StateQueued
		reason := ReasonLeaseExpired
		outcome := any(nil)
		summary := any(nil)
		if item.cancelled {
			state = StateCancelled
			reason = ReasonCancelled
			outcome = StateCancelled
			summary = "cancelled"
		}
		if _, err := tx.Exec(`UPDATE jobs SET state=?,reason=?,lease_id=NULL,lease_holder=NULL,lease_until=NULL,outcome=?,summary=?,updated_at=? WHERE job_id=? AND state=?`, state, reason, outcome, summary, now.UnixNano(), item.id, StateLeased); err != nil {
			return errors.New("workqueue: expired lease recovery failed")
		}
		if terminal(state) {
			if err := propagate(tx, item.id, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecoverExpired makes abandoned leases visible to restart-safe coordinators without
// requiring them to guess which pool or worker owned the previous process.
func (s *Store) RecoverExpired() error {
	if s == nil || s.db == nil {
		return errors.New("workqueue: store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("workqueue: recovery transaction failed")
	}
	defer tx.Rollback()
	if err := recoverExpired(tx, s.clock().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("workqueue: recovery commit failed")
	}
	return nil
}

func (s *Store) Heartbeat(jobID, leaseID string, fence uint64, ttl time.Duration) (HeartbeatStatus, error) {
	if s == nil || !jobIDPattern.MatchString(jobID) || !leaseIDPattern.MatchString(leaseID) || fence == 0 || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return HeartbeatStatus{}, errors.New("workqueue: heartbeat is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	expires := now.Add(ttl)
	result, err := s.db.Exec(`UPDATE jobs SET lease_until=?,updated_at=? WHERE job_id=? AND state=? AND lease_id=? AND fence=? AND lease_until>?`, expires.UnixNano(), now.UnixNano(), jobID, StateLeased, leaseID, fence, now.UnixNano())
	if err != nil {
		return HeartbeatStatus{}, errors.New("workqueue: heartbeat failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return HeartbeatStatus{}, errors.New("workqueue: active fenced lease not found")
	}
	var cancelled bool
	if err := s.db.QueryRow(`SELECT cancel_requested FROM jobs WHERE job_id=?`, jobID).Scan(&cancelled); err != nil {
		return HeartbeatStatus{}, errors.New("workqueue: heartbeat state unavailable")
	}
	return HeartbeatStatus{ExpiresAt: expires, CancelRequested: cancelled}, nil
}

func (s *Store) Complete(jobID, leaseID string, fence uint64, result Result) (Job, error) {
	if s == nil || !jobIDPattern.MatchString(jobID) || !leaseIDPattern.MatchString(leaseID) || fence == 0 || validateResult(result) != nil {
		return Job{}, errors.New("workqueue: completion is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Job{}, errors.New("workqueue: completion transaction failed")
	}
	defer tx.Rollback()
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found {
		return Job{}, errors.New("workqueue: job not found")
	}
	if terminal(job.State) {
		if job.LeaseID == leaseID && job.Fence == fence && job.Outcome == result.Outcome && job.Summary == result.Summary && job.ResultRef == result.ResultRef {
			return job, nil
		}
		return Job{}, errors.New("workqueue: terminal completion conflicts")
	}
	if job.State != StateLeased || job.LeaseID != leaseID || job.Fence != fence || !job.LeaseExpiresAt.After(s.clock().UTC()) {
		return Job{}, errors.New("workqueue: stale fenced completion rejected")
	}
	if job.CancelRequested && result.Outcome != StateCancelled {
		return Job{}, errors.New("workqueue: cancelled job requires cancelled result")
	}
	if !legalTransition(job.State, result.Outcome) {
		return Job{}, errors.New("workqueue: illegal completion transition")
	}
	now := s.clock().UTC()
	if _, err := tx.Exec(`UPDATE jobs SET state=?,reason='',outcome=?,summary=?,result_ref=?,updated_at=? WHERE job_id=? AND state=? AND lease_id=? AND fence=?`,
		result.Outcome, result.Outcome, result.Summary, nullableString(result.ResultRef), now.UnixNano(), jobID, StateLeased, leaseID, fence); err != nil {
		return Job{}, errors.New("workqueue: completion update failed")
	}
	if err := propagate(tx, jobID, now); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, errors.New("workqueue: completion commit failed")
	}
	completed, _, err := s.getUnlocked(jobID)
	return completed, err
}

func (s *Store) Cancel(jobID string) (Job, error) {
	if s == nil || !jobIDPattern.MatchString(jobID) {
		return Job{}, errors.New("workqueue: job id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Job{}, errors.New("workqueue: cancel transaction failed")
	}
	defer tx.Rollback()
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found {
		return Job{}, errors.New("workqueue: job not found")
	}
	if terminal(job.State) {
		return job, nil
	}
	now := s.clock().UTC()
	if job.State == StateLeased {
		if _, err := tx.Exec(`UPDATE jobs SET cancel_requested=1,reason=?,updated_at=? WHERE job_id=? AND state=?`, ReasonCancelRequested, now.UnixNano(), jobID, StateLeased); err != nil {
			return Job{}, errors.New("workqueue: running cancellation failed")
		}
	} else {
		if !legalTransition(job.State, StateCancelled) {
			return Job{}, errors.New("workqueue: cancellation transition is illegal")
		}
		if _, err := tx.Exec(`UPDATE jobs SET state=?,reason=?,cancel_requested=1,outcome=?,summary=?,updated_at=? WHERE job_id=? AND state=?`, StateCancelled, ReasonCancelled, StateCancelled, "cancelled", now.UnixNano(), jobID, job.State); err != nil {
			return Job{}, errors.New("workqueue: cancellation failed")
		}
		if err := propagate(tx, jobID, now); err != nil {
			return Job{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Job{}, errors.New("workqueue: cancel commit failed")
	}
	cancelled, _, err := s.getUnlocked(jobID)
	return cancelled, err
}

func propagate(tx *sql.Tx, terminalJobID string, now time.Time) error {
	queue := []string{terminalJobID}
	seen := map[string]bool{}
	for len(queue) > 0 {
		dependency := queue[0]
		queue = queue[1:]
		if seen[dependency] {
			continue
		}
		seen[dependency] = true
		rows, err := tx.Query(`SELECT job_id FROM dependencies WHERE dependency_id=? ORDER BY job_id`, dependency)
		if err != nil {
			return errors.New("workqueue: dependency propagation failed")
		}
		dependents := make([]string, 0)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return errors.New("workqueue: dependency propagation failed")
			}
			dependents = append(dependents, id)
		}
		_ = rows.Close()
		for _, dependentID := range dependents {
			dependent, found, err := jobByID(tx, dependentID)
			if err != nil || !found {
				return errors.New("workqueue: dependent job unavailable")
			}
			if dependent.State != StateBlocked {
				continue
			}
			dependencies, err := dependencyIDs(tx, dependentID)
			if err != nil {
				return err
			}
			state, reason, err := dependencyState(tx, dependencies)
			if err != nil {
				return err
			}
			if state == StateBlocked {
				continue
			}
			if !legalTransition(StateBlocked, state) {
				return errors.New("workqueue: dependency transition is illegal")
			}
			outcome := any(nil)
			summary := any(nil)
			if terminal(state) {
				outcome = state
				summary = "dependency failed"
			}
			if _, err := tx.Exec(`UPDATE jobs SET state=?,reason=?,outcome=?,summary=?,updated_at=? WHERE job_id=? AND state=?`, state, reason, outcome, summary, now.UnixNano(), dependentID, StateBlocked); err != nil {
				return errors.New("workqueue: dependency update failed")
			}
			if terminal(state) {
				queue = append(queue, dependentID)
			}
		}
	}
	return nil
}

func (s *Store) Get(jobID string) (Job, bool, error) {
	if s == nil || !jobIDPattern.MatchString(jobID) {
		return Job{}, false, errors.New("workqueue: job id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getUnlocked(jobID)
}

func (s *Store) getUnlocked(jobID string) (Job, bool, error) {
	return jobByID(s.db, jobID)
}

func (s *Store) List(limit int) ([]Job, error) {
	if s == nil || s.db == nil || limit < 1 || limit > MaxListResults {
		return nil, errors.New("workqueue: list limit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(jobSelect+` ORDER BY created_at,job_id LIMIT ?`, limit)
	if err != nil {
		return nil, errors.New("workqueue: list failed")
	}
	defer rows.Close()
	jobs := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, errors.New("workqueue: list result failed")
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) Integrity() error {
	if s == nil || s.db == nil {
		return errors.New("workqueue: store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var integrity string
	if err := s.db.QueryRow(`PRAGMA integrity_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("workqueue: integrity check failed")
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		return errors.New("workqueue: schema version mismatch")
	}
	var pageSize, pageCount int64
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil || pageSize <= 0 {
		return errors.New("workqueue: page size unavailable")
	}
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil || pageCount < 0 || pageSize*pageCount > TargetMaxBytes {
		return errors.New("workqueue: storage bound exceeded")
	}
	rows, err := s.db.Query(jobSelect)
	if err != nil {
		return errors.New("workqueue: semantic scan failed")
	}
	count := 0
	for rows.Next() {
		if _, err := scanJob(rows); err != nil {
			_ = rows.Close()
			return errors.New("workqueue: semantic integrity failed")
		}
		count++
		if count > s.config.MaxJobs {
			_ = rows.Close()
			return errors.New("workqueue: row bound exceeded")
		}
	}
	if err := rows.Close(); err != nil {
		return errors.New("workqueue: semantic scan failed")
	}
	var oversizedWorkspaces int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM (SELECT workspace FROM jobs GROUP BY workspace HAVING COUNT(*)>?)`, s.config.MaxJobsPerWorkspace).Scan(&oversizedWorkspaces); err != nil || oversizedWorkspaces != 0 {
		return errors.New("workqueue: workspace row bound exceeded")
	}
	var dependencyCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dependencies`).Scan(&dependencyCount); err != nil || dependencyCount < 0 || dependencyCount > s.config.MaxJobs*MaxDependencies {
		return errors.New("workqueue: dependency bound exceeded")
	}
	taskRows, err := s.db.Query(`SELECT task_id FROM task_groups ORDER BY task_id`)
	if err != nil {
		return errors.New("workqueue: task semantic scan failed")
	}
	taskIDs := make([]string, 0)
	for taskRows.Next() {
		var taskID string
		if err := taskRows.Scan(&taskID); err != nil {
			_ = taskRows.Close()
			return errors.New("workqueue: task semantic scan failed")
		}
		taskIDs = append(taskIDs, taskID)
		if len(taskIDs) > s.config.MaxJobs {
			_ = taskRows.Close()
			return errors.New("workqueue: task row bound exceeded")
		}
	}
	if err := taskRows.Close(); err != nil {
		return errors.New("workqueue: task semantic scan failed")
	}
	for _, taskID := range taskIDs {
		if _, found, err := taskByID(s.db, taskID); err != nil || !found {
			return errors.New("workqueue: task semantic integrity failed")
		}
	}
	foreignRows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return errors.New("workqueue: foreign key check failed")
	}
	defer foreignRows.Close()
	if foreignRows.Next() {
		return errors.New("workqueue: foreign key integrity failed")
	}
	return foreignRows.Err()
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var closeErr error
	if s.db != nil {
		_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		closeErr = s.db.Close()
		s.db = nil
	}
	if s.lockFile != nil {
		releaseWriterLock(s.lockFile)
		s.lockFile = nil
	}
	return closeErr
}

const jobSelect = `SELECT job_id,idempotency_key,workspace,pool,profile,payload_hash,state,reason,created_at,updated_at,cancel_requested,attempt,fence,lease_id,lease_holder,lease_until,outcome,summary,result_ref FROM jobs`

type rowScanner interface{ Scan(...any) error }
type queryer interface {
	QueryRow(string, ...any) *sql.Row
}

func jobByID(q queryer, jobID string) (Job, bool, error) {
	job, err := scanJob(q.QueryRow(jobSelect+` WHERE job_id=?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, errors.New("workqueue: job read failed")
	}
	return job, true, nil
}

func jobByIdempotency(q queryer, key string) (Job, bool, error) {
	job, err := scanJob(q.QueryRow(jobSelect+` WHERE idempotency_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, errors.New("workqueue: job read failed")
	}
	return job, true, nil
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var createdAt, updatedAt int64
	var cancelRequested bool
	var leaseID, leaseHolder, outcome, summary, resultRef sql.NullString
	var leaseUntil sql.NullInt64
	var fence int64
	if err := row.Scan(&job.ID, &job.IdempotencyKey, &job.Workspace, &job.Pool, &job.Profile, &job.PayloadHash, &job.State, &job.Reason, &createdAt, &updatedAt, &cancelRequested, &job.Attempt, &fence, &leaseID, &leaseHolder, &leaseUntil, &outcome, &summary, &resultRef); err != nil {
		return Job{}, err
	}
	if fence < 0 {
		return Job{}, errors.New("workqueue: stored fence is invalid")
	}
	job.Fence = uint64(fence)
	job.CancelRequested = cancelRequested
	job.LeaseID = leaseID.String
	job.LeaseHolder = leaseHolder.String
	job.Outcome = State(outcome.String)
	job.Summary = summary.String
	job.ResultRef = resultRef.String
	job.CreatedAt = time.Unix(0, createdAt).UTC()
	job.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if leaseUntil.Valid {
		job.LeaseExpiresAt = time.Unix(0, leaseUntil.Int64).UTC()
	}
	return job, validateStoredJob(job)
}

func validateStoredJob(job Job) error {
	if !jobIDPattern.MatchString(job.ID) || validateSpec(Spec{IdempotencyKey: job.IdempotencyKey, Workspace: job.Workspace, Pool: job.Pool, Profile: job.Profile, PayloadHash: job.PayloadHash}) != nil || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) || job.Attempt < 0 {
		return errors.New("workqueue: stored job is invalid")
	}
	switch job.State {
	case StateBlocked:
		if job.Reason != ReasonDependencyPending || job.CancelRequested || job.LeaseID != "" || job.LeaseHolder != "" || !job.LeaseExpiresAt.IsZero() || job.Outcome != "" {
			return errors.New("workqueue: stored blocked job is invalid")
		}
	case StateQueued:
		if job.Reason != ReasonNone && job.Reason != ReasonLeaseExpired || job.CancelRequested || job.LeaseID != "" || job.LeaseHolder != "" || !job.LeaseExpiresAt.IsZero() || job.Outcome != "" {
			return errors.New("workqueue: stored queued job is invalid")
		}
	case StateLeased:
		if !leaseIDPattern.MatchString(job.LeaseID) || !holderPattern.MatchString(job.LeaseHolder) || job.LeaseExpiresAt.IsZero() || job.Fence == 0 || job.Outcome != "" {
			return errors.New("workqueue: stored lease is invalid")
		}
		if job.CancelRequested != (job.Reason == ReasonCancelRequested) || job.Reason != ReasonNone && job.Reason != ReasonCancelRequested {
			return errors.New("workqueue: stored lease cancellation is invalid")
		}
	case StateSucceeded, StateFailed, StateCancelled:
		if job.Outcome != job.State || validateResult(Result{Outcome: job.Outcome, Summary: job.Summary, ResultRef: job.ResultRef}) != nil {
			return errors.New("workqueue: stored terminal job is invalid")
		}
		hasLeaseIdentity := job.LeaseID != "" || job.LeaseHolder != "" || !job.LeaseExpiresAt.IsZero()
		if hasLeaseIdentity && (!leaseIDPattern.MatchString(job.LeaseID) || !holderPattern.MatchString(job.LeaseHolder) || job.LeaseExpiresAt.IsZero() || job.Fence == 0) {
			return errors.New("workqueue: stored terminal lease is invalid")
		}
		if job.State != StateCancelled && job.CancelRequested {
			return errors.New("workqueue: stored terminal cancellation flag is invalid")
		}
		switch job.State {
		case StateSucceeded:
			if job.Reason != ReasonNone {
				return errors.New("workqueue: stored success reason is invalid")
			}
		case StateFailed:
			if job.Reason != ReasonNone && job.Reason != ReasonDependencyFailed {
				return errors.New("workqueue: stored failure reason is invalid")
			}
		case StateCancelled:
			if job.Reason != ReasonNone && job.Reason != ReasonCancelled {
				return errors.New("workqueue: stored cancellation reason is invalid")
			}
		}
	default:
		return errors.New("workqueue: stored state is invalid")
	}
	return nil
}

func dependencyIDs(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, jobID string) ([]string, error) {
	rows, err := q.Query(`SELECT dependency_id FROM dependencies WHERE job_id=? ORDER BY dependency_id`, jobID)
	if err != nil {
		return nil, errors.New("workqueue: dependency read failed")
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.New("workqueue: dependency read failed")
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func randomID(prefix string) (string, error) {
	body := make([]byte, 16)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(body), nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Store) String() string {
	if s == nil {
		return "workqueue(unavailable)"
	}
	return fmt.Sprintf("workqueue(controller=%s)", s.config.ControllerID)
}
