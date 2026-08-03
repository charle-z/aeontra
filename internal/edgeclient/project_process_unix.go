//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	ossignal "os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"

	_ "modernc.org/sqlite"
)

type ProjectProcessState string
type ProjectProcessSignal string

const (
	ProjectProcessStarting ProjectProcessState = "starting"
	ProjectProcessRunning  ProjectProcessState = "running"
	ProjectProcessStopping ProjectProcessState = "stopping"
	ProjectProcessExited   ProjectProcessState = "exited"
	ProjectProcessFailed   ProjectProcessState = "failed"
	ProjectProcessStopped  ProjectProcessState = "stopped"

	ProjectProcessInterrupt ProjectProcessSignal = "interrupt"
	ProjectProcessTerminate ProjectProcessSignal = "terminate"
	ProjectProcessKill      ProjectProcessSignal = "kill"
)

const (
	defaultProjectProcessLimit    = 256
	defaultProjectProcessLogBytes = 64 << 20
	projectProcessDatabaseFile    = "project-processes.db"
	projectProcessLogDirectory    = "project-process-logs"
	projectProcessWorkerDirectory = "project-process-workers"
)

var (
	projectProcessIDPattern              = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	projectProcessIdempotencyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	projectProcessReasonPattern          = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	ErrProjectProcessNotFound            = errors.New("project process not found")
	ErrProjectProcessIdempotencyConflict = errors.New("project process idempotency conflict")
	ErrProjectProcessIdentityChanged     = errors.New("project process identity changed")
	ErrProjectProcessNotOwned            = errors.New("project process is not owned")
	ErrProjectProcessGroupMissing        = errors.New("project process group is missing")
)

type ProjectProcessIdentity struct {
	ProcessID      string `json:"process_id"`
	PID            int    `json:"pid"`
	ProcessGroupID int    `json:"process_group_id"`
	StartTicks     uint64 `json:"start_ticks"`
}

type ProjectProcessExit struct {
	ExitKnown      bool                 `json:"exit_known"`
	ExitCode       int                  `json:"exit_code"`
	TerminalSignal ProjectProcessSignal `json:"terminal_signal,omitempty"`
	Reason         string               `json:"reason,omitempty"`
}

type ProjectProcessPlatform interface {
	Start(DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error)
	Alive(ProjectProcessIdentity) (bool, error)
	Signal(ProjectProcessIdentity, ProjectProcessSignal) error
}

type ProjectProcessStartRequest struct {
	OperationID, IdempotencyKey, ProjectAlias, TargetAlias string
	Workspace                                              Workspace
	Argv                                                   []string
	CWD, Stdin                                             string
	Environment                                            map[string]string
}

type ProjectProcessReadRequest struct {
	ProcessID, ProjectAlias, TargetAlias string
	StdoutOffset, StderrOffset           int64
	LimitBytes                           int
}

type ProjectProcessStopRequest struct {
	ProcessID, ProjectAlias, TargetAlias string
	GracePeriod                          time.Duration
}

type ProjectProcessSignalRequest struct {
	ProcessID, ProjectAlias, TargetAlias string
	Signal                               ProjectProcessSignal
}

type ProjectProcessListRequest struct {
	ProjectAlias, TargetAlias string
	Limit                     int
}

type ProjectProcessCleanupRequest struct {
	ProcessID, ProjectAlias, TargetAlias string
}

type ProjectProcessCleanupResult struct {
	Removed int
	Active  int
}

type ProjectProcessSnapshot struct {
	ProcessID                        string
	State                            ProjectProcessState
	StartedAt, FinishedAt            time.Time
	ExitKnown                        bool
	ExitCode                         int
	TerminalSignal                   ProjectProcessSignal
	Reason                           string
	Stdout, Stderr                   string
	StdoutNext, StderrNext           int64
	StdoutEOF, StderrEOF             bool
	StdoutTruncated, StderrTruncated bool
}

type ProjectProcessManagerConfig struct {
	StateRoot    string
	Platform     ProjectProcessPlatform
	MaxProcesses int
	MaxLogBytes  int64
	NewID        func() (string, error)
	Now          func() time.Time
}

type ProjectProcessManager struct {
	db                             *sql.DB
	stateRoot, logRoot, workerRoot string
	platform                       ProjectProcessPlatform
	resolveExecutable              bool
	maxProcesses                   int
	maxLogBytes                    int64
	newID                          func() (string, error)
	now                            func() time.Time
	watchMu                        sync.Mutex
	watching                       map[string]bool
}

type projectProcessRecord struct {
	ProcessID, IdempotencyKey, RequestDigest, OperationID string
	WorkspaceID, ProjectAlias, TargetAlias                string
	Identity                                              ProjectProcessIdentity
	State                                                 ProjectProcessState
	StartedAt, FinishedAt                                 time.Time
	ExitKnown                                             bool
	ExitCode                                              int
	TerminalSignal                                        ProjectProcessSignal
	Reason                                                string
}

func OpenProjectProcessManager(config ProjectProcessManagerConfig) (*ProjectProcessManager, error) {
	root := filepath.Clean(strings.TrimSpace(config.StateRoot))
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || root == "." {
		return nil, errors.New("project process state root is invalid")
	}
	if err := preparePrivateRoot(root); err != nil {
		return nil, errors.New("project process state is unavailable")
	}
	logRoot := filepath.Join(root, projectProcessLogDirectory)
	if err := os.MkdirAll(logRoot, 0o700); err != nil || os.Chmod(logRoot, 0o700) != nil {
		return nil, errors.New("project process logs are unavailable")
	}
	workerRoot := filepath.Join(root, projectProcessWorkerDirectory)
	if err := os.MkdirAll(workerRoot, 0o700); err != nil || os.Chmod(workerRoot, 0o700) != nil {
		return nil, errors.New("project process worker state is unavailable")
	}
	databasePath := filepath.Join(root, projectProcessDatabaseFile)
	if info, err := os.Lstat(databasePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() > 32<<20 {
			return nil, errors.New("project process journal is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("project process journal is unavailable")
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, errors.New("project process journal is unavailable")
	}
	db.SetMaxOpenConns(1)
	statements := []string{
		`PRAGMA journal_mode=DELETE`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`, `PRAGMA max_page_count=8192`,
		`CREATE TABLE IF NOT EXISTS project_processes (
			process_id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, request_digest TEXT NOT NULL, operation_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL, project_alias TEXT NOT NULL, target_alias TEXT NOT NULL,
			pid INTEGER NOT NULL DEFAULT 0, process_group_id INTEGER NOT NULL DEFAULT 0, start_ticks INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL, started_at INTEGER NOT NULL, finished_at INTEGER, exit_known INTEGER NOT NULL DEFAULT 0,
			exit_code INTEGER NOT NULL DEFAULT 0, terminal_signal TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT ''
		) WITHOUT ROWID`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("project process journal initialization failed")
		}
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("project process journal permissions failed")
	}
	maxProcesses := config.MaxProcesses
	if maxProcesses == 0 {
		maxProcesses = defaultProjectProcessLimit
	}
	maxLogBytes := config.MaxLogBytes
	if maxLogBytes == 0 {
		maxLogBytes = defaultProjectProcessLogBytes
	}
	if maxProcesses < 1 || maxProcesses > 4096 || maxLogBytes < 1 || maxLogBytes > 1<<30 {
		_ = db.Close()
		return nil, errors.New("project process emergency limits are invalid")
	}
	platform := config.Platform
	resolveExecutable := platform == nil
	if platform == nil {
		platform = osProjectProcessPlatform{stateRoot: root, workerRoot: workerRoot}
	}
	newID := config.NewID
	if newID == nil {
		newID = newProjectProcessID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	manager := &ProjectProcessManager{db: db, stateRoot: root, logRoot: logRoot, workerRoot: workerRoot, platform: platform, resolveExecutable: resolveExecutable, maxProcesses: maxProcesses, maxLogBytes: maxLogBytes, newID: newID, now: now, watching: map[string]bool{}}
	if err := manager.Reconcile(); err != nil {
		_ = db.Close()
		return nil, errors.New("project process reconciliation failed")
	}
	return manager, nil
}

func (manager *ProjectProcessManager) Close() error {
	if manager == nil || manager.db == nil {
		return nil
	}
	return manager.db.Close()
}

func (manager *ProjectProcessManager) Start(ctx context.Context, request ProjectProcessStartRequest) (ProjectProcessSnapshot, bool, error) {
	if !projectProcessIdempotencyPattern.MatchString(request.IdempotencyKey) || !projectAliasPattern.MatchString(request.ProjectAlias) || !projectTargetPattern.MatchString(request.TargetAlias) {
		return ProjectProcessSnapshot{}, false, errors.New("project process start request is invalid")
	}
	if projectProcessRequestContainsSecret(request) {
		return ProjectProcessSnapshot{}, false, errors.New("project process start request contains secret material")
	}
	digest, err := projectProcessRequestDigest(request)
	if err != nil {
		return ProjectProcessSnapshot{}, false, err
	}
	if existing, err := manager.recordByIdempotency(request.IdempotencyKey); err == nil {
		if existing.RequestDigest != digest {
			return ProjectProcessSnapshot{}, false, ErrProjectProcessIdempotencyConflict
		}
		return manager.snapshot(existing), false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ProjectProcessSnapshot{}, false, errors.New("project process journal unavailable")
	}
	var active int
	if err := manager.db.QueryRow(`SELECT COUNT(*) FROM project_processes WHERE state IN ('starting','running','stopping')`).Scan(&active); err != nil || active >= manager.maxProcesses {
		return ProjectProcessSnapshot{}, false, errors.New("project process limit reached")
	}
	processID, err := manager.newID()
	if err != nil || !projectProcessIDPattern.MatchString(processID) {
		return ProjectProcessSnapshot{}, false, errors.New("project process identity generation failed")
	}
	var stdoutTarget, stderrTarget io.Writer = io.Discard, io.Discard
	var stdoutCloser, stderrCloser io.Closer = projectProcessNopCloser{}, projectProcessNopCloser{}
	if !manager.resolveExecutable {
		stdoutWriter, openErr := manager.openLogWriter(processID, "stdout")
		if openErr != nil {
			return ProjectProcessSnapshot{}, false, openErr
		}
		stderrWriter, openErr := manager.openLogWriter(processID, "stderr")
		if openErr != nil {
			_ = stdoutWriter.Close()
			return ProjectProcessSnapshot{}, false, openErr
		}
		stdoutTarget, stderrTarget = stdoutWriter, stderrWriter
		stdoutCloser, stderrCloser = stdoutWriter, stderrWriter
	}
	started := manager.now().UTC()
	record := projectProcessRecord{ProcessID: processID, IdempotencyKey: request.IdempotencyKey, RequestDigest: digest, OperationID: request.OperationID, WorkspaceID: request.Workspace.ID, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias, State: ProjectProcessStarting, StartedAt: started}
	if err := manager.insertRecord(record); err != nil {
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		return ProjectProcessSnapshot{}, false, err
	}
	spec, err := prepareDirectWorkcellProcessSpec(DirectWorkcellCommandRequest{OperationID: request.OperationID, Workspace: request.Workspace, Argv: request.Argv, CWD: request.CWD, Stdin: request.Stdin, Environment: request.Environment, TimeoutSeconds: 1, Persistent: true}, stdoutTarget, stderrTarget, manager.resolveExecutable)
	if err != nil {
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		_ = manager.finishFailed(processID, "process_contract_invalid")
		return ProjectProcessSnapshot{}, false, err
	}
	spec.PersistentProcessID = processID
	if manager.resolveExecutable {
		spec.PersistentStateRoot = manager.stateRoot
		spec.PersistentMaxLogBytes = manager.maxLogBytes
	}
	if err := ctx.Err(); err != nil {
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		_ = manager.finishFailed(processID, "process_start_cancelled")
		return ProjectProcessSnapshot{}, false, err
	}
	identity, exits, err := manager.platform.Start(spec)
	if err != nil || identity.PID < 1 || identity.ProcessGroupID < 1 || identity.StartTicks == 0 || exits == nil {
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		_ = manager.finishFailed(processID, "process_start_failed")
		return ProjectProcessSnapshot{}, false, errors.New("project process start failed")
	}
	if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=? AND state=?`, identity.PID, identity.ProcessGroupID, identity.StartTicks, ProjectProcessRunning, processID, ProjectProcessStarting); err != nil {
		_ = manager.platform.Signal(identity, ProjectProcessKill)
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		return ProjectProcessSnapshot{}, false, errors.New("project process journal unavailable")
	}
	record.Identity = identity
	record.State = ProjectProcessRunning
	manager.watchMu.Lock()
	manager.watching[processID] = true
	manager.watchMu.Unlock()
	go manager.watch(processID, exits, stdoutCloser, stderrCloser)
	return manager.snapshot(record), true, nil
}

type projectProcessNopCloser struct{}

func (projectProcessNopCloser) Close() error { return nil }

func projectProcessRequestContainsSecret(request ProjectProcessStartRequest) bool {
	values := append([]string(nil), request.Argv...)
	values = append(values, request.CWD, request.Stdin)
	for key, value := range request.Environment {
		values = append(values, key+"="+value)
	}
	_, redacted := policy.Redact(strings.Join(values, "\n"))
	return redacted
}

func (manager *ProjectProcessManager) Status(request ProjectProcessReadRequest) (ProjectProcessSnapshot, error) {
	if !projectProcessIDPattern.MatchString(request.ProcessID) || request.LimitBytes < 1 || request.LimitBytes > edge.MaxProjectProcessReadBytes || request.StdoutOffset < 0 || request.StderrOffset < 0 {
		return ProjectProcessSnapshot{}, errors.New("project process status request is invalid")
	}
	record, err := manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
	if err != nil {
		return ProjectProcessSnapshot{}, err
	}
	if record.State == ProjectProcessRunning || record.State == ProjectProcessStopping {
		manager.watchMu.Lock()
		watched := manager.watching[record.ProcessID]
		manager.watchMu.Unlock()
		alive, aliveErr := manager.platform.Alive(record.Identity)
		if errors.Is(aliveErr, ErrProjectProcessIdentityChanged) {
			_ = manager.reconcileRecord(record)
			record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		} else if aliveErr != nil || !alive {
			if watched {
				if terminal, ok := manager.waitTerminal(context.Background(), record.ProcessID, 50*time.Millisecond); ok {
					record = terminal
					aliveErr = nil
					alive = true
				}
			}
			if aliveErr != nil || !alive {
				if watched {
					_ = manager.finishFailed(record.ProcessID, "process_lost")
				} else {
					_ = manager.reconcileRecord(record)
				}
				record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
			}
		}
	}
	snapshot := manager.snapshot(record)
	snapshot.Stdout, snapshot.StdoutNext, snapshot.StdoutEOF, snapshot.StdoutTruncated, err = manager.readLog(record.ProcessID, "stdout", request.StdoutOffset, request.LimitBytes)
	if err != nil {
		return ProjectProcessSnapshot{}, err
	}
	snapshot.Stderr, snapshot.StderrNext, snapshot.StderrEOF, snapshot.StderrTruncated, err = manager.readLog(record.ProcessID, "stderr", request.StderrOffset, request.LimitBytes)
	if err != nil {
		return ProjectProcessSnapshot{}, err
	}
	return snapshot, nil
}

func (manager *ProjectProcessManager) Stop(ctx context.Context, request ProjectProcessStopRequest) (ProjectProcessSnapshot, error) {
	if !projectProcessIDPattern.MatchString(request.ProcessID) || request.GracePeriod <= 0 || request.GracePeriod > 30*time.Second {
		return ProjectProcessSnapshot{}, errors.New("project process stop request is invalid")
	}
	record, err := manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
	if err != nil {
		return ProjectProcessSnapshot{}, err
	}
	if projectProcessTerminal(record.State) {
		return manager.snapshot(record), nil
	}
	alive, err := manager.platform.Alive(record.Identity)
	if errors.Is(err, ErrProjectProcessIdentityChanged) {
		_ = manager.reconcileRecord(record)
		record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		return manager.snapshot(record), nil
	}
	if err != nil || !alive {
		_ = manager.reconcileRecord(record)
		record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		return manager.snapshot(record), nil
	}
	_, _ = manager.db.Exec(`UPDATE project_processes SET state=? WHERE process_id=? AND state IN (?,?)`, ProjectProcessStopping, record.ProcessID, ProjectProcessStarting, ProjectProcessRunning)
	if err := manager.platform.Signal(record.Identity, ProjectProcessTerminate); err != nil {
		return ProjectProcessSnapshot{}, errors.New("project process stop failed")
	}
	if terminal, ok := manager.waitTerminal(ctx, record.ProcessID, request.GracePeriod); ok {
		return manager.snapshot(terminal), nil
	}
	if err := manager.platform.Signal(record.Identity, ProjectProcessKill); err != nil && !errors.Is(err, ErrProjectProcessIdentityChanged) {
		return ProjectProcessSnapshot{}, errors.New("project process kill failed")
	}
	if terminal, ok := manager.waitTerminal(ctx, record.ProcessID, 5*time.Second); ok {
		return manager.snapshot(terminal), nil
	}
	return ProjectProcessSnapshot{}, errors.New("project process did not stop")
}

func (manager *ProjectProcessManager) Signal(request ProjectProcessSignalRequest) (ProjectProcessSnapshot, error) {
	if !projectProcessIDPattern.MatchString(request.ProcessID) ||
		(request.Signal != ProjectProcessInterrupt && request.Signal != ProjectProcessTerminate && request.Signal != ProjectProcessKill) {
		return ProjectProcessSnapshot{}, errors.New("project process signal request is invalid")
	}
	record, err := manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
	if err != nil {
		return ProjectProcessSnapshot{}, err
	}
	if projectProcessTerminal(record.State) {
		return manager.snapshot(record), nil
	}
	alive, err := manager.platform.Alive(record.Identity)
	if errors.Is(err, ErrProjectProcessIdentityChanged) {
		_ = manager.reconcileRecord(record)
		record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		return manager.snapshot(record), nil
	}
	if err != nil || !alive {
		_ = manager.reconcileRecord(record)
		record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		return manager.snapshot(record), nil
	}
	if request.Signal != ProjectProcessInterrupt {
		_, _ = manager.db.Exec(`UPDATE project_processes SET state=? WHERE process_id=? AND state IN (?,?)`, ProjectProcessStopping, record.ProcessID, ProjectProcessStarting, ProjectProcessRunning)
		record.State = ProjectProcessStopping
	}
	if err := manager.platform.Signal(record.Identity, request.Signal); err != nil {
		if errors.Is(err, ErrProjectProcessIdentityChanged) {
			_ = manager.reconcileRecord(record)
			record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
			return manager.snapshot(record), nil
		}
		return ProjectProcessSnapshot{}, errors.New("project process signal failed")
	}
	return manager.snapshot(record), nil
}

func (manager *ProjectProcessManager) List(request ProjectProcessListRequest) ([]ProjectProcessSnapshot, error) {
	if !projectAliasPattern.MatchString(request.ProjectAlias) || !projectTargetPattern.MatchString(request.TargetAlias) || request.Limit < 1 || request.Limit > 100 {
		return nil, errors.New("project process list request is invalid")
	}
	if err := manager.Reconcile(); err != nil {
		return nil, err
	}
	rows, err := manager.db.Query(projectProcessSelect+` WHERE project_alias=? AND target_alias=? ORDER BY started_at DESC LIMIT ?`, request.ProjectAlias, request.TargetAlias, request.Limit)
	if err != nil {
		return nil, errors.New("project process journal unavailable")
	}
	defer rows.Close()
	items := make([]ProjectProcessSnapshot, 0)
	for rows.Next() {
		record, scanErr := scanProjectProcessRecord(rows)
		if scanErr != nil {
			return nil, errors.New("project process journal unavailable")
		}
		items = append(items, manager.snapshot(record))
	}
	if rows.Err() != nil {
		return nil, errors.New("project process journal unavailable")
	}
	return items, nil
}

func (manager *ProjectProcessManager) Cleanup(request ProjectProcessCleanupRequest) (ProjectProcessCleanupResult, error) {
	if !projectAliasPattern.MatchString(request.ProjectAlias) || !projectTargetPattern.MatchString(request.TargetAlias) ||
		(request.ProcessID != "" && !projectProcessIDPattern.MatchString(request.ProcessID)) {
		return ProjectProcessCleanupResult{}, errors.New("project process cleanup request is invalid")
	}
	if err := manager.Reconcile(); err != nil {
		return ProjectProcessCleanupResult{}, err
	}
	query := projectProcessSelect + ` WHERE project_alias=? AND target_alias=?`
	arguments := []any{request.ProjectAlias, request.TargetAlias}
	if request.ProcessID != "" {
		query += ` AND process_id=?`
		arguments = append(arguments, request.ProcessID)
	}
	rows, err := manager.db.Query(query, arguments...)
	if err != nil {
		return ProjectProcessCleanupResult{}, errors.New("project process journal unavailable")
	}
	records := make([]projectProcessRecord, 0)
	for rows.Next() {
		record, scanErr := scanProjectProcessRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return ProjectProcessCleanupResult{}, errors.New("project process journal unavailable")
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return ProjectProcessCleanupResult{}, errors.New("project process journal unavailable")
	}
	if request.ProcessID != "" && len(records) == 0 {
		return ProjectProcessCleanupResult{}, ErrProjectProcessNotFound
	}
	result := ProjectProcessCleanupResult{}
	for _, record := range records {
		if !projectProcessTerminal(record.State) {
			result.Active++
			continue
		}
		active, err := manager.privateProcessActive(record)
		if err != nil {
			return result, errors.New("project process cleanup liveness unavailable")
		}
		if active {
			result.Active++
			continue
		}
		if err := manager.removeLogs(record.ProcessID); err != nil {
			return result, err
		}
		if changed, err := manager.db.Exec(`DELETE FROM project_processes WHERE process_id=? AND state IN (?,?,?)`, record.ProcessID, ProjectProcessExited, ProjectProcessFailed, ProjectProcessStopped); err != nil {
			return result, errors.New("project process journal unavailable")
		} else if count, _ := changed.RowsAffected(); count == 1 {
			result.Removed++
		}
	}
	return result, nil
}

func (manager *ProjectProcessManager) Reconcile() error {
	rows, err := manager.db.Query(projectProcessSelect+` WHERE state IN (?,?,?)`, ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping)
	if err != nil {
		return errors.New("project process journal unavailable")
	}
	records := make([]projectProcessRecord, 0)
	for rows.Next() {
		record, scanErr := scanProjectProcessRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return errors.New("project process journal unavailable")
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return errors.New("project process journal unavailable")
	}
	for _, record := range records {
		manager.watchMu.Lock()
		watched := manager.watching[record.ProcessID]
		manager.watchMu.Unlock()
		if watched {
			continue
		}
		if err := manager.reconcileRecord(record); err != nil {
			return err
		}
	}
	return nil
}

func (manager *ProjectProcessManager) reconcileRecord(record projectProcessRecord) error {
	if record.Identity.PID < 1 || record.Identity.ProcessGroupID < 1 || record.Identity.StartTicks == 0 {
		identity, identityErr := readProjectProcessWorkerIdentity(manager.workerRoot, record.ProcessID)
		if identityErr != nil {
			return manager.finishFailed(record.ProcessID, "process_metadata_incomplete")
		}
		record.Identity = identity
		if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=? AND state=?`, identity.PID, identity.ProcessGroupID, identity.StartTicks, ProjectProcessRunning, record.ProcessID, ProjectProcessStarting); err != nil {
			return errors.New("project process journal unavailable")
		}
		record.State = ProjectProcessRunning
	}
	if recovered, alive, recoveryErr := manager.recoverPrivateWorkerIdentity(record); recoveryErr != nil {
		return recoveryErr
	} else if alive {
		record = recovered
	}
	alive, err := manager.platform.Alive(record.Identity)
	if errors.Is(err, ErrProjectProcessIdentityChanged) {
		return manager.finishFailed(record.ProcessID, "process_identity_changed")
	}
	if errors.Is(err, ErrProjectProcessNotOwned) {
		return manager.finishFailed(record.ProcessID, "process_not_owned")
	}
	if errors.Is(err, ErrProjectProcessGroupMissing) {
		return manager.finishFailed(record.ProcessID, "process_group_missing")
	}
	if err != nil {
		return errors.New("project process reconciliation unavailable")
	}
	if alive {
		if err := manager.validateRecoveredLogs(record.ProcessID); err != nil {
			failureErr := manager.finishFailed(record.ProcessID, "process_logs_incomplete")
			_ = manager.platform.Signal(record.Identity, ProjectProcessKill)
			return failureErr
		}
		if record.State == ProjectProcessStarting {
			_, err = manager.db.Exec(`UPDATE project_processes SET state=? WHERE process_id=? AND state=?`, ProjectProcessRunning, record.ProcessID, ProjectProcessStarting)
		}
		return err
	}
	if exit, receiptErr := readProjectProcessWorkerExit(manager.workerRoot, record.ProcessID); receiptErr == nil {
		return manager.finishRecoveredExit(record, exit)
	}
	state := ProjectProcessExited
	reason := "process_exited_while_offline"
	if record.State == ProjectProcessStopping {
		state = ProjectProcessStopped
		reason = "process_stopped_while_offline"
	}
	_, err = manager.db.Exec(`UPDATE project_processes SET state=?,finished_at=?,reason=? WHERE process_id=? AND state IN (?,?,?)`, state, manager.now().UTC().UnixNano(), reason, record.ProcessID, ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping)
	return err
}

func (manager *ProjectProcessManager) recoverPrivateWorkerIdentity(record projectProcessRecord) (projectProcessRecord, bool, error) {
	identity, exists, err := manager.optionalPrivateProcessIdentity(record.ProcessID, "identity")
	if err != nil || !exists {
		return record, false, err
	}
	alive, err := manager.platform.Alive(identity)
	if errors.Is(err, ErrProjectProcessIdentityChanged) || errors.Is(err, ErrProjectProcessGroupMissing) {
		return record, false, nil
	}
	if err != nil {
		return record, false, errors.New("project process reconciliation unavailable")
	}
	if !alive {
		return record, false, nil
	}
	if record.Identity != identity {
		state := record.State
		if state == ProjectProcessStarting {
			state = ProjectProcessRunning
		}
		if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=? AND state IN (?,?,?)`,
			identity.PID, identity.ProcessGroupID, identity.StartTicks, state, record.ProcessID,
			ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping); err != nil {
			return record, false, errors.New("project process journal unavailable")
		}
		record.Identity = identity
		record.State = state
	}
	return record, true, nil
}

func (manager *ProjectProcessManager) privateProcessActive(record projectProcessRecord) (bool, error) {
	identities := []ProjectProcessIdentity{record.Identity}
	for _, kind := range []string{"identity", "child"} {
		identity, exists, err := manager.optionalPrivateProcessIdentity(record.ProcessID, kind)
		if err != nil {
			return false, err
		}
		if exists && !slices.Contains(identities, identity) {
			identities = append(identities, identity)
		}
	}
	for _, identity := range identities {
		if identity.PID < 1 || identity.ProcessGroupID < 1 || identity.StartTicks == 0 {
			continue
		}
		alive, err := manager.platform.Alive(identity)
		if err == nil && alive {
			return true, nil
		}
		if err != nil && !errors.Is(err, ErrProjectProcessIdentityChanged) && !errors.Is(err, ErrProjectProcessGroupMissing) {
			return false, err
		}
	}
	return false, nil
}

func (manager *ProjectProcessManager) optionalPrivateProcessIdentity(processID, kind string) (ProjectProcessIdentity, bool, error) {
	path := projectProcessWorkerPath(manager.workerRoot, processID, kind)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return ProjectProcessIdentity{}, false, nil
	} else if err != nil {
		return ProjectProcessIdentity{}, false, errors.New("project process worker state is unsafe")
	}
	identity, err := readProjectProcessWorkerIdentityKind(manager.workerRoot, processID, kind)
	if err != nil {
		return ProjectProcessIdentity{}, false, err
	}
	return identity, true, nil
}

func (manager *ProjectProcessManager) finishRecoveredExit(record projectProcessRecord, exit ProjectProcessExit) error {
	state := ProjectProcessExited
	if record.State == ProjectProcessStopping {
		state = ProjectProcessStopped
	}
	if exit.Reason != "" && !exit.ExitKnown && exit.TerminalSignal == "" {
		state = ProjectProcessFailed
	}
	_, err := manager.db.Exec(`UPDATE project_processes SET state=?,finished_at=?,exit_known=?,exit_code=?,terminal_signal=?,reason=? WHERE process_id=? AND state IN (?,?,?)`,
		state, manager.now().UTC().UnixNano(), exit.ExitKnown, exit.ExitCode, exit.TerminalSignal, exit.Reason,
		record.ProcessID, ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping)
	return err
}

func (manager *ProjectProcessManager) validateRecoveredLogs(processID string) error {
	for _, stream := range []string{"stdout", "stderr"} {
		file, _, err := openPrivateProjectProcessLog(filepath.Join(manager.logRoot, processID+"."+stream+".log"))
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (manager *ProjectProcessManager) removeLogs(processID string) error {
	for _, suffix := range []string{".stdout.log", ".stdout.log.truncated", ".stderr.log", ".stderr.log.truncated"} {
		path := filepath.Join(manager.logRoot, processID+suffix)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) {
			return errors.New("project process log is unsafe")
		}
		if err := os.Remove(path); err != nil {
			return errors.New("project process log cleanup failed")
		}
	}
	for _, kind := range []string{"request", "identity", "child", "ready", "control", "exit"} {
		path := projectProcessWorkerPath(manager.workerRoot, processID, kind)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) {
			return errors.New("project process worker state is unsafe")
		}
		if err := os.Remove(path); err != nil {
			return errors.New("project process worker cleanup failed")
		}
	}
	return nil
}

func (manager *ProjectProcessManager) watch(processID string, exits <-chan ProjectProcessExit, stdout, stderr io.Closer) {
	exit, ok := <-exits
	_ = stdout.Close()
	_ = stderr.Close()
	if !ok {
		exit = ProjectProcessExit{Reason: "process_wait_failed"}
	}
	now := manager.now().UTC().UnixNano()
	state := ProjectProcessExited
	var current ProjectProcessState
	_ = manager.db.QueryRow(`SELECT state FROM project_processes WHERE process_id=?`, processID).Scan(&current)
	if current == ProjectProcessStopping {
		state = ProjectProcessStopped
	}
	if exit.Reason != "" && !exit.ExitKnown {
		state = ProjectProcessFailed
	}
	_, _ = manager.db.Exec(`UPDATE project_processes SET state=?,finished_at=?,exit_known=?,exit_code=?,terminal_signal=?,reason=? WHERE process_id=? AND state IN (?,?,?)`, state, now, exit.ExitKnown, exit.ExitCode, exit.TerminalSignal, exit.Reason, processID, ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping)
	manager.watchMu.Lock()
	delete(manager.watching, processID)
	manager.watchMu.Unlock()
}

func (manager *ProjectProcessManager) waitTerminal(ctx context.Context, processID string, timeout time.Duration) (projectProcessRecord, bool) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		record, err := manager.recordByID(processID)
		if err == nil && projectProcessTerminal(record.State) {
			return record, true
		}
		select {
		case <-ctx.Done():
			return projectProcessRecord{}, false
		case <-deadline.C:
			return projectProcessRecord{}, false
		case <-ticker.C:
		}
	}
}

func (manager *ProjectProcessManager) insertRecord(record projectProcessRecord) error {
	_, err := manager.db.Exec(`INSERT INTO project_processes(process_id,idempotency_key,request_digest,operation_id,workspace_id,project_alias,target_alias,state,started_at) VALUES(?,?,?,?,?,?,?,?,?)`, record.ProcessID, record.IdempotencyKey, record.RequestDigest, record.OperationID, record.WorkspaceID, record.ProjectAlias, record.TargetAlias, record.State, record.StartedAt.UnixNano())
	if err != nil {
		return errors.New("project process journal unavailable")
	}
	return nil
}

func (manager *ProjectProcessManager) recordByIdempotency(key string) (projectProcessRecord, error) {
	return scanProjectProcessRecord(manager.db.QueryRow(projectProcessSelect+` WHERE idempotency_key=?`, key))
}
func (manager *ProjectProcessManager) recordByID(id string) (projectProcessRecord, error) {
	return scanProjectProcessRecord(manager.db.QueryRow(projectProcessSelect+` WHERE process_id=?`, id))
}
func (manager *ProjectProcessManager) boundRecord(id, alias, target string) (projectProcessRecord, error) {
	if !projectAliasPattern.MatchString(alias) || !projectTargetPattern.MatchString(target) {
		return projectProcessRecord{}, ErrProjectProcessNotFound
	}
	record, err := scanProjectProcessRecord(manager.db.QueryRow(projectProcessSelect+` WHERE process_id=? AND project_alias=? AND target_alias=?`, id, alias, target))
	if err != nil {
		return projectProcessRecord{}, ErrProjectProcessNotFound
	}
	return record, nil
}

const projectProcessSelect = `SELECT process_id,idempotency_key,request_digest,operation_id,workspace_id,project_alias,target_alias,pid,process_group_id,start_ticks,state,started_at,finished_at,exit_known,exit_code,terminal_signal,reason FROM project_processes`

type projectProcessRow interface{ Scan(...any) error }

func scanProjectProcessRecord(row projectProcessRow) (projectProcessRecord, error) {
	var record projectProcessRecord
	var started int64
	var finished sql.NullInt64
	var exitKnown bool
	err := row.Scan(&record.ProcessID, &record.IdempotencyKey, &record.RequestDigest, &record.OperationID, &record.WorkspaceID, &record.ProjectAlias, &record.TargetAlias, &record.Identity.PID, &record.Identity.ProcessGroupID, &record.Identity.StartTicks, &record.State, &started, &finished, &exitKnown, &record.ExitCode, &record.TerminalSignal, &record.Reason)
	if err != nil {
		return projectProcessRecord{}, err
	}
	record.StartedAt = time.Unix(0, started).UTC()
	record.Identity.ProcessID = record.ProcessID
	record.ExitKnown = exitKnown
	if finished.Valid {
		record.FinishedAt = time.Unix(0, finished.Int64).UTC()
	}
	return record, nil
}

func (manager *ProjectProcessManager) finishFailed(processID, reason string) error {
	_, err := manager.db.Exec(`UPDATE project_processes SET state=?,finished_at=?,reason=? WHERE process_id=? AND state IN (?,?,?)`, ProjectProcessFailed, manager.now().UTC().UnixNano(), reason, processID, ProjectProcessStarting, ProjectProcessRunning, ProjectProcessStopping)
	return err
}

func (manager *ProjectProcessManager) snapshot(record projectProcessRecord) ProjectProcessSnapshot {
	return ProjectProcessSnapshot{ProcessID: record.ProcessID, State: record.State, StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, ExitKnown: record.ExitKnown, ExitCode: record.ExitCode, TerminalSignal: record.TerminalSignal, Reason: record.Reason}
}

func projectProcessTerminal(state ProjectProcessState) bool {
	return state == ProjectProcessExited || state == ProjectProcessFailed || state == ProjectProcessStopped
}

func projectProcessRequestDigest(request ProjectProcessStartRequest) (string, error) {
	body, err := json.Marshal(struct {
		WorkspaceID, ProjectAlias, TargetAlias string
		Argv                                   []string
		CWD, Stdin                             string
		Environment                            map[string]string
	}{request.Workspace.ID, request.ProjectAlias, request.TargetAlias, request.Argv, request.CWD, request.Stdin, request.Environment})
	if err != nil {
		return "", errors.New("project process request is invalid")
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func newProjectProcessID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "pr_" + hex.EncodeToString(raw[:]), nil
}

type projectProcessLogWriter struct {
	file            *os.File
	marker          string
	max             int64
	written         int64
	pending         []byte
	privateKeyBlock bool
	redactUntilEOL  bool
	mu              sync.Mutex
}

const maxProjectProcessPendingLine = 64 << 10

func (manager *ProjectProcessManager) openLogWriter(processID, stream string) (*projectProcessLogWriter, error) {
	return openProjectProcessLogWriter(manager.logRoot, processID, stream, manager.maxLogBytes)
}

func openProjectProcessLogWriter(logRoot, processID, stream string, maxBytes int64) (*projectProcessLogWriter, error) {
	if !projectProcessIDPattern.MatchString(processID) || (stream != "stdout" && stream != "stderr") || maxBytes < 1 || maxBytes > 1<<30 {
		return nil, errors.New("project process log unavailable")
	}
	path := filepath.Join(logRoot, processID+"."+stream+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("project process log unavailable")
	}
	return &projectProcessLogWriter{file: file, marker: path + ".truncated", max: maxBytes}, nil
}
func (writer *projectProcessLogWriter) Write(input []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pending = append(writer.pending, input...)
	for {
		newline := bytes.IndexByte(writer.pending, '\n')
		if newline < 0 {
			if len(writer.pending) <= maxProjectProcessPendingLine {
				break
			}
			writer.pending = writer.pending[:0]
			writer.redactUntilEOL = true
			if err := writer.writeSanitized([]byte("***REDACTED-SECRET***")); err != nil {
				return 0, err
			}
			break
		}
		line := append([]byte(nil), writer.pending[:newline+1]...)
		writer.pending = writer.pending[newline+1:]
		if writer.redactUntilEOL {
			writer.redactUntilEOL = false
			continue
		}
		if err := writer.writeRedactedLine(line); err != nil {
			return 0, err
		}
	}
	return len(input), nil
}

func (writer *projectProcessLogWriter) writeRedactedLine(line []byte) error {
	value := strings.ToValidUTF8(string(line), "�")
	if writer.privateKeyBlock {
		if strings.Contains(value, "-----END ") && strings.Contains(value, "PRIVATE KEY-----") {
			writer.privateKeyBlock = false
		}
		return nil
	}
	if strings.Contains(value, "-----BEGIN ") && strings.Contains(value, "PRIVATE KEY-----") {
		writer.privateKeyBlock = true
		return writer.writeSanitized([]byte("***REDACTED-SECRET***\n"))
	}
	value, _ = policy.Redact(value)
	return writer.writeSanitized([]byte(value))
}

func (writer *projectProcessLogWriter) writeSanitized(data []byte) error {
	remaining := writer.max - writer.written
	if remaining <= 0 {
		writer.markTruncated()
		return nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		writer.markTruncated()
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	written, err := writer.file.Write(data)
	writer.written += int64(written)
	return err
}
func (writer *projectProcessLogWriter) markTruncated() {
	file, err := os.OpenFile(writer.marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_ = file.Close()
	}
}
func (writer *projectProcessLogWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	var err error
	if len(writer.pending) > 0 && !writer.redactUntilEOL && !writer.privateKeyBlock {
		err = writer.writeRedactedLine(writer.pending)
	}
	writer.pending = nil
	if syncErr := writer.file.Sync(); err == nil {
		err = syncErr
	}
	closeErr := writer.file.Close()
	writer.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

func (manager *ProjectProcessManager) readLog(processID, stream string, offset int64, limit int) (string, int64, bool, bool, error) {
	path := filepath.Join(manager.logRoot, processID+"."+stream+".log")
	file, info, err := openPrivateProjectProcessLog(path)
	if err != nil {
		return "", 0, false, false, errors.New("project process log unavailable")
	}
	defer file.Close()
	if offset > info.Size() {
		offset = info.Size()
	}
	buffer := make([]byte, limit)
	read, err := file.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", 0, false, false, errors.New("project process log unavailable")
	}
	value, _ := policy.Redact(strings.ToValidUTF8(string(buffer[:read]), "�"))
	next := offset + int64(read)
	markerInfo, markerErr := os.Lstat(path + ".truncated")
	truncated := markerErr == nil
	if markerErr == nil && (!markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 || markerInfo.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(markerInfo)) {
		return "", 0, false, false, errors.New("project process log is unsafe")
	}
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return "", 0, false, false, errors.New("project process log unavailable")
	}
	return value, next, next >= info.Size(), truncated, nil
}

func openPrivateProjectProcessLog(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(before) {
		return nil, nil, errors.New("project process log is unsafe")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, errors.New("project process log is unsafe")
	}
	file := os.NewFile(uintptr(fd), path)
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(after) {
		_ = file.Close()
		return nil, nil, errors.New("project process log is unsafe")
	}
	return file, after, nil
}

type osProjectProcessPlatform struct {
	stateRoot, workerRoot string
}

func (platform osProjectProcessPlatform) Start(spec DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
	if spec.PersistentProcessID != "" {
		return platform.startWorker(spec)
	}
	command := exec.Command(spec.Executable, spec.Args...)
	command.Dir = spec.Dir
	command.Env = append([]string(nil), spec.Env...)
	command.Stdin = spec.Stdin
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return ProjectProcessIdentity{}, nil, err
	}
	startTicks, err := linuxProcessStartTicks(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		return ProjectProcessIdentity{}, nil, err
	}
	group, err := syscall.Getpgid(command.Process.Pid)
	if err != nil || group != command.Process.Pid {
		_ = command.Process.Kill()
		return ProjectProcessIdentity{}, nil, errors.New("project process group is invalid")
	}
	identity := ProjectProcessIdentity{ProcessID: spec.PersistentProcessID, PID: command.Process.Pid, ProcessGroupID: group, StartTicks: startTicks}
	exits := make(chan ProjectProcessExit, 1)
	go func() {
		err := command.Wait()
		result := ProjectProcessExit{}
		if command.ProcessState != nil {
			code := command.ProcessState.ExitCode()
			if code >= 0 {
				result.ExitKnown = true
				result.ExitCode = code
			}
			if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				result.TerminalSignal = projectProcessSignalFromUnix(status.Signal())
			}
		}
		if err != nil && !result.ExitKnown && result.TerminalSignal == "" {
			result.Reason = "process_wait_failed"
		}
		exits <- result
		close(exits)
	}()
	return identity, exits, nil
}

type projectProcessWorkerRequest struct {
	Executable  string   `json:"executable"`
	Args        []string `json:"args"`
	Dir         string   `json:"dir"`
	Env         []string `json:"env"`
	Stdin       string   `json:"stdin,omitempty"`
	MaxLogBytes int64    `json:"max_log_bytes"`
}

func (platform osProjectProcessPlatform) startWorker(spec DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
	if !projectProcessIDPattern.MatchString(spec.PersistentProcessID) || filepath.Clean(spec.PersistentStateRoot) != filepath.Clean(platform.stateRoot) ||
		platform.workerRoot != filepath.Join(platform.stateRoot, projectProcessWorkerDirectory) || spec.PersistentMaxLogBytes < 1 || spec.PersistentMaxLogBytes > 1<<30 {
		return ProjectProcessIdentity{}, nil, errors.New("project process worker contract is invalid")
	}
	request := projectProcessWorkerRequest{
		Executable: spec.Executable, Args: append([]string(nil), spec.Args...), Dir: spec.Dir,
		Env: append([]string(nil), spec.Env...), Stdin: spec.PersistentStdin, MaxLogBytes: spec.PersistentMaxLogBytes,
	}
	requestBody, err := json.Marshal(request)
	if err != nil || len(requestBody) > 128<<10 {
		return ProjectProcessIdentity{}, nil, errors.New("project process worker request is invalid")
	}
	requestPath := projectProcessWorkerPath(platform.workerRoot, spec.PersistentProcessID, "request")
	if err := writePrivateProjectProcessWorkerFile(requestPath, requestBody); err != nil {
		return ProjectProcessIdentity{}, nil, err
	}
	executable, err := os.Executable()
	if err != nil || !filepath.IsAbs(executable) {
		_ = os.Remove(requestPath)
		return ProjectProcessIdentity{}, nil, errors.New("project process worker executable is unavailable")
	}
	command := exec.Command(executable, "project-process-worker", "--state", platform.stateRoot, "--process-id", spec.PersistentProcessID)
	command.Dir = platform.stateRoot
	command.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + platform.stateRoot, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		_ = os.Remove(requestPath)
		return ProjectProcessIdentity{}, nil, err
	}
	startTicks, err := linuxProcessStartTicks(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return ProjectProcessIdentity{}, nil, err
	}
	group, err := syscall.Getpgid(command.Process.Pid)
	if err != nil || group != command.Process.Pid {
		_ = command.Process.Kill()
		_ = command.Wait()
		return ProjectProcessIdentity{}, nil, errors.New("project process worker group is invalid")
	}
	identity := ProjectProcessIdentity{ProcessID: spec.PersistentProcessID, PID: command.Process.Pid, ProcessGroupID: group, StartTicks: startTicks}
	if err := writeProjectProcessWorkerIdentity(platform.workerRoot, identity); err != nil {
		_ = syscall.Kill(-identity.ProcessGroupID, syscall.SIGKILL)
		_ = command.Wait()
		return ProjectProcessIdentity{}, nil, err
	}
	exits := make(chan ProjectProcessExit, 1)
	go func() {
		waitErr := command.Wait()
		exit, receiptErr := readProjectProcessWorkerExit(platform.workerRoot, spec.PersistentProcessID)
		if receiptErr != nil {
			exit = ProjectProcessExit{Reason: "process_wait_failed"}
			if waitErr == nil {
				exit.Reason = "process_receipt_missing"
			}
		}
		exits <- exit
		close(exits)
	}()
	readyPath := projectProcessWorkerPath(platform.workerRoot, spec.PersistentProcessID, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, statErr := os.Lstat(readyPath); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 && ownedByCurrentUIDPortable(info) {
			_ = os.Remove(readyPath)
			return identity, exits, nil
		}
		select {
		case <-exits:
			return ProjectProcessIdentity{}, nil, errors.New("project process worker failed before ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	_ = syscall.Kill(-identity.ProcessGroupID, syscall.SIGKILL)
	return ProjectProcessIdentity{}, nil, errors.New("project process worker readiness timed out")
}

func RunProjectProcessWorker(stateRoot, processID string) error {
	root := filepath.Clean(strings.TrimSpace(stateRoot))
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || !projectProcessIDPattern.MatchString(processID) {
		return errors.New("project process worker request is invalid")
	}
	if err := preparePrivateRoot(root); err != nil {
		return errors.New("project process worker state is unavailable")
	}
	workerRoot := filepath.Join(root, projectProcessWorkerDirectory)
	logRoot := filepath.Join(root, projectProcessLogDirectory)
	for _, directory := range []string{workerRoot, logRoot} {
		info, statErr := os.Lstat(directory)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUIDPortable(info) {
			return errors.New("project process worker state is unsafe")
		}
	}
	requestPath := projectProcessWorkerPath(workerRoot, processID, "request")
	body, err := readPrivateProjectProcessWorkerFile(requestPath, 128<<10)
	if err != nil {
		return err
	}
	var request projectProcessWorkerRequest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || request.Executable == "" || !filepath.IsAbs(request.Executable) ||
		len(request.Args) == 0 || len(request.Args) > 512 || !filepath.IsAbs(request.Dir) || len(request.Env) > 128 || len(request.Stdin) > edge.MaxProjectExecStdinBytes ||
		request.MaxLogBytes < 1 || request.MaxLogBytes > 1<<30 {
		return errors.New("project process worker request is invalid")
	}
	resolvedExecutable, err := filepath.EvalSymlinks(request.Executable)
	if err != nil || !filepath.IsAbs(resolvedExecutable) {
		return errors.New("project process worker executable is unsafe")
	}
	executableInfo, err := os.Lstat(resolvedExecutable)
	if err != nil || !executableInfo.Mode().IsRegular() || executableInfo.Mode()&os.ModeSymlink != 0 || executableInfo.Mode().Perm()&0o111 == 0 || executableInfo.Mode().Perm()&0o022 != 0 ||
		(!ownedByCurrentUIDPortable(executableInfo) && !ownedByUID(executableInfo, 0)) {
		return errors.New("project process worker executable is unsafe")
	}
	request.Executable = resolvedExecutable
	if err := os.Remove(requestPath); err != nil {
		return errors.New("project process worker request cleanup failed")
	}
	stdout, err := openProjectProcessLogWriter(logRoot, processID, "stdout", request.MaxLogBytes)
	if err != nil {
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_log_unavailable"})
		return err
	}
	stderr, err := openProjectProcessLogWriter(logRoot, processID, "stderr", request.MaxLogBytes)
	if err != nil {
		_ = stdout.Close()
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_log_unavailable"})
		return err
	}
	signals := make(chan os.Signal, 4)
	ossignal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	defer ossignal.Stop(signals)
	commandArgs, needsSandboxInfo, err := projectProcessWorkerCommandArgs(request.Args)
	if err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
		return err
	}
	var sandboxInfoReader, sandboxInfoWriter *os.File
	if needsSandboxInfo {
		sandboxInfoReader, sandboxInfoWriter, err = os.Pipe()
		if err != nil {
			_ = stdout.Close()
			_ = stderr.Close()
			_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
			return errors.New("project process sandbox identity unavailable")
		}
	}
	command := exec.Command(request.Executable, commandArgs...)
	command.Dir = request.Dir
	command.Env = append([]string(nil), request.Env...)
	command.Stdin = strings.NewReader(request.Stdin)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if sandboxInfoWriter != nil {
		command.ExtraFiles = []*os.File{sandboxInfoWriter}
	}
	if err := command.Start(); err != nil {
		if sandboxInfoReader != nil {
			_ = sandboxInfoReader.Close()
			_ = sandboxInfoWriter.Close()
		}
		_ = stdout.Close()
		_ = stderr.Close()
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_start_failed"})
		return errors.New("project process worker start failed")
	}
	if sandboxInfoWriter != nil {
		_ = sandboxInfoWriter.Close()
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	childPID := command.Process.Pid
	if sandboxInfoReader != nil {
		info := make(chan projectProcessSandboxInfoResult, 1)
		go func() {
			defer sandboxInfoReader.Close()
			info <- readProjectProcessSandboxInfo(sandboxInfoReader)
		}()
		select {
		case result := <-info:
			if result.Err != nil {
				_ = command.Process.Kill()
				<-waited
				_ = stdout.Close()
				_ = stderr.Close()
				_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
				return result.Err
			}
			childPID = result.ChildPID
		case <-waited:
			_ = sandboxInfoReader.Close()
			_ = stdout.Close()
			_ = stderr.Close()
			_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
			return errors.New("project process sandbox exited before identity")
		case <-time.After(5 * time.Second):
			_ = sandboxInfoReader.Close()
			_ = command.Process.Kill()
			<-waited
			_ = stdout.Close()
			_ = stderr.Close()
			_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
			return errors.New("project process sandbox identity timed out")
		}
	}
	childTicks, childTicksErr := linuxProcessStartTicks(childPID)
	childGroup, childGroupErr := syscall.Getpgid(childPID)
	childIdentity := ProjectProcessIdentity{ProcessID: processID, PID: childPID, ProcessGroupID: childGroup, StartTicks: childTicks}
	childAlive, childAliveErr := (osProjectProcessPlatform{}).Alive(childIdentity)
	if childTicksErr != nil || childGroupErr != nil || childGroup != childPID || childAliveErr != nil || !childAlive || writeProjectProcessWorkerChildIdentity(workerRoot, childIdentity) != nil {
		_ = command.Process.Kill()
		<-waited
		_ = stdout.Close()
		_ = stderr.Close()
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_identity_invalid"})
		return errors.New("project process worker child identity failed")
	}
	if err := writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "ready"), []byte("ready\n")); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = writeProjectProcessWorkerExit(workerRoot, processID, ProjectProcessExit{Reason: "process_ready_failed"})
		return err
	}
	var waitErr error
	waiting := true
	for waiting {
		select {
		case waitErr = <-waited:
			waiting = false
		case received := <-signals:
			forward := received.(syscall.Signal)
			if forward == syscall.SIGUSR1 {
				controlPath := projectProcessWorkerPath(workerRoot, processID, "control")
				control, controlErr := readPrivateProjectProcessWorkerFile(controlPath, 64)
				if controlErr != nil || strings.TrimSpace(string(control)) != "kill" {
					continue
				}
				_ = os.Remove(controlPath)
				forward = syscall.SIGKILL
			}
			_ = syscall.Kill(-childIdentity.ProcessGroupID, forward)
		}
	}
	_ = stdout.Close()
	_ = stderr.Close()
	exit := ProjectProcessExit{}
	if command.ProcessState != nil {
		code := command.ProcessState.ExitCode()
		if code >= 0 {
			exit.ExitKnown = true
			exit.ExitCode = code
		}
		if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			exit.TerminalSignal = projectProcessSignalFromUnix(status.Signal())
		}
	}
	if waitErr != nil && !exit.ExitKnown && exit.TerminalSignal == "" {
		exit.Reason = "process_wait_failed"
	}
	if err := writeProjectProcessWorkerExit(workerRoot, processID, exit); err != nil {
		return err
	}
	return nil
}

type projectProcessSandboxInfoResult struct {
	ChildPID int
	Err      error
}

func projectProcessWorkerCommandArgs(arguments []string) ([]string, bool, error) {
	needsSandboxInfo := false
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "--" {
			break
		}
		switch arguments[index] {
		case "--new-session":
			needsSandboxInfo = true
		case "--info-fd", "--json-status-fd":
			return nil, false, errors.New("project process sandbox identity arguments are reserved")
		}
	}
	if !needsSandboxInfo {
		return append([]string(nil), arguments...), false, nil
	}
	return append([]string{"--info-fd", "3"}, arguments...), true, nil
}

func readProjectProcessSandboxInfo(reader io.Reader) projectProcessSandboxInfoResult {
	var info struct {
		ChildPID int `json:"child-pid"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 4096))
	if decoder.Decode(&info) != nil || info.ChildPID < 1 {
		return projectProcessSandboxInfoResult{Err: errors.New("project process sandbox identity is invalid")}
	}
	return projectProcessSandboxInfoResult{ChildPID: info.ChildPID}
}

func projectProcessWorkerPath(workerRoot, processID, kind string) string {
	return filepath.Join(workerRoot, processID+"."+kind+".json")
}

func writePrivateProjectProcessWorkerFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("project process worker state is unavailable")
	}
	_, writeErr := file.Write(append(append([]byte(nil), body...), '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.New("project process worker state is unavailable")
	}
	return nil
}

func readPrivateProjectProcessWorkerFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("project process worker state is unsafe")
	}
	file, after, err := openPrivateProjectProcessLog(path)
	if err != nil || after.Size() > limit {
		return nil, errors.New("project process worker state is unsafe")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("project process worker state is unavailable")
	}
	return body, nil
}

func writeProjectProcessWorkerExit(workerRoot, processID string, exit ProjectProcessExit) error {
	body, err := json.Marshal(exit)
	if err != nil {
		return errors.New("project process worker receipt is invalid")
	}
	return writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "exit"), body)
}

func writeProjectProcessWorkerIdentity(workerRoot string, identity ProjectProcessIdentity) error {
	return writeProjectProcessWorkerIdentityKind(workerRoot, identity, "identity")
}

func writeProjectProcessWorkerChildIdentity(workerRoot string, identity ProjectProcessIdentity) error {
	return writeProjectProcessWorkerIdentityKind(workerRoot, identity, "child")
}

func writeProjectProcessWorkerIdentityKind(workerRoot string, identity ProjectProcessIdentity, kind string) error {
	if !projectProcessIDPattern.MatchString(identity.ProcessID) || identity.PID < 1 || identity.ProcessGroupID < 1 || identity.StartTicks == 0 {
		return errors.New("project process worker identity is invalid")
	}
	body, err := json.Marshal(identity)
	if err != nil {
		return errors.New("project process worker identity is invalid")
	}
	return writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, identity.ProcessID, kind), body)
}

func readProjectProcessWorkerIdentity(workerRoot, processID string) (ProjectProcessIdentity, error) {
	return readProjectProcessWorkerIdentityKind(workerRoot, processID, "identity")
}

func readProjectProcessWorkerChildIdentity(workerRoot, processID string) (ProjectProcessIdentity, error) {
	return readProjectProcessWorkerIdentityKind(workerRoot, processID, "child")
}

func readProjectProcessWorkerIdentityKind(workerRoot, processID, kind string) (ProjectProcessIdentity, error) {
	body, err := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, kind), 2048)
	if err != nil {
		return ProjectProcessIdentity{}, err
	}
	var identity ProjectProcessIdentity
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&identity) != nil || decoder.Decode(&struct{}{}) != io.EOF || identity.ProcessID != processID || identity.PID < 1 || identity.ProcessGroupID < 1 || identity.StartTicks == 0 {
		return ProjectProcessIdentity{}, errors.New("project process worker identity is invalid")
	}
	return identity, nil
}

func readProjectProcessWorkerExit(workerRoot, processID string) (ProjectProcessExit, error) {
	body, err := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "exit"), 2048)
	if err != nil {
		return ProjectProcessExit{}, err
	}
	var exit ProjectProcessExit
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&exit) != nil || decoder.Decode(&struct{}{}) != io.EOF || (exit.ExitKnown && (exit.ExitCode < 0 || exit.ExitCode > 255)) ||
		(exit.TerminalSignal != "" && exit.TerminalSignal != ProjectProcessInterrupt && exit.TerminalSignal != ProjectProcessTerminate && exit.TerminalSignal != ProjectProcessKill) ||
		(exit.Reason != "" && !projectProcessReasonPattern.MatchString(exit.Reason)) {
		return ProjectProcessExit{}, errors.New("project process worker receipt is invalid")
	}
	return exit, nil
}
func (osProjectProcessPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	ticks, err := linuxProcessStartTicks(identity.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	processInfo, err := os.Stat(filepath.Join("/proc", strconv.Itoa(identity.PID)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !ownedByCurrentUIDPortable(processInfo) {
		return false, ErrProjectProcessNotOwned
	}
	group, err := syscall.Getpgid(identity.PID)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, ErrProjectProcessGroupMissing
		}
		return false, err
	}
	if ticks != identity.StartTicks || group != identity.ProcessGroupID {
		return false, ErrProjectProcessIdentityChanged
	}
	return true, nil
}
func (platform osProjectProcessPlatform) Signal(identity ProjectProcessIdentity, signal ProjectProcessSignal) error {
	alive, err := platform.Alive(identity)
	if err != nil {
		return err
	}
	if !alive {
		return ErrProjectProcessIdentityChanged
	}
	var unix syscall.Signal
	switch signal {
	case ProjectProcessInterrupt:
		unix = syscall.SIGINT
	case ProjectProcessTerminate:
		unix = syscall.SIGTERM
	case ProjectProcessKill:
		unix = syscall.SIGKILL
	default:
		return errors.New("project process signal is invalid")
	}
	if !projectProcessIDPattern.MatchString(identity.ProcessID) {
		return errors.New("project process identity is invalid")
	}
	childPath := projectProcessWorkerPath(platform.workerRoot, identity.ProcessID, "child")
	if _, statErr := os.Lstat(childPath); statErr == nil {
		child, err := readProjectProcessWorkerChildIdentity(platform.workerRoot, identity.ProcessID)
		if err != nil {
			return err
		}
		childAlive, err := platform.Alive(child)
		if err != nil || !childAlive {
			return ErrProjectProcessIdentityChanged
		}
		if err := syscall.Kill(-child.ProcessGroupID, unix); errors.Is(err, syscall.ESRCH) {
			return ErrProjectProcessIdentityChanged
		} else {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("project process worker state is unsafe")
	}
	// Workers started by an earlier signed release have no child identity file.
	// Retain the closed relay solely so those live processes remain controllable
	// across an Edge update.
	if signal == ProjectProcessKill {
		controlPath := projectProcessWorkerPath(platform.workerRoot, identity.ProcessID, "control")
		if err := writePrivateProjectProcessWorkerFile(controlPath, []byte("kill")); err != nil {
			return err
		}
		unix = syscall.SIGUSR1
	}
	if err := syscall.Kill(identity.PID, unix); errors.Is(err, syscall.ESRCH) {
		return ErrProjectProcessIdentityChanged
	} else {
		return err
	}
}
func linuxProcessStartTicks(pid int) (uint64, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndexByte(string(content), ')')
	if end < 0 {
		return 0, errors.New("process identity is invalid")
	}
	fields := strings.Fields(string(content[end+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("process identity is invalid")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}
func projectProcessSignalFromUnix(signal syscall.Signal) ProjectProcessSignal {
	switch signal {
	case syscall.SIGINT:
		return ProjectProcessInterrupt
	case syscall.SIGTERM:
		return ProjectProcessTerminate
	case syscall.SIGKILL:
		return ProjectProcessKill
	default:
		return ""
	}
}
