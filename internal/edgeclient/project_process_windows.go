//go:build windows

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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"

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
	projectProcessStdinDirectory  = "process-stdin"
)

var (
	projectProcessIDPattern              = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	projectProcessIdempotencyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	projectProcessReasonPattern          = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	ErrProjectProcessNotFound            = errors.New("project process not found")
	ErrProjectProcessIdempotencyConflict = errors.New("project process idempotency conflict")
	ErrProjectProcessLimitReached        = errors.New("project process limit reached")
	ErrProjectProcessStdinConflict       = errors.New("project process stdin offset conflict")
	ErrProjectProcessStdinClosed         = errors.New("project process stdin is closed")
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
	WriteStdin(ProjectProcessIdentity, ProjectProcessStdinWrite) (ProjectProcessStdinReceipt, error)
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

type ProjectProcessStdinRequest struct {
	ProcessID, ProjectAlias, TargetAlias string
	FrameID                              string
	ExpectedOffset                       int64
	Data                                 string
	Close                                bool
}

type ProjectProcessStdinWrite struct {
	FrameID        string
	ExpectedOffset int64
	Data           []byte
	Close          bool
}

type ProjectProcessStdinReceipt struct {
	NextOffset    int64
	AcceptedBytes int
	Closed        bool
	Reused        bool
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
	stdinRoot                      string
	platform                       ProjectProcessPlatform
	resolveExecutable              bool
	maxProcesses                   int
	maxLogBytes                    int64
	newID                          func() (string, error)
	now                            func() time.Time
	startMu                        sync.Mutex
	effectLocks                    [64]sync.Mutex
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
	stdinRoot := filepath.Join(root, projectProcessStdinDirectory)
	if err := os.MkdirAll(stdinRoot, 0o700); err != nil || os.Chmod(stdinRoot, 0o700) != nil {
		return nil, errors.New("project process stdin state is unavailable")
	}
	databasePath := filepath.Join(root, projectProcessDatabaseFile)
	if info, err := os.Lstat(databasePath); err == nil {
		if !validWindowsPrivateProjectProcessFile(databasePath, info) || info.Size() > 32<<20 {
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
	if err := securePrivateRegularPath(databasePath); err != nil {
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
		platform = &windowsProjectProcessPlatform{stateRoot: root, workerRoot: workerRoot, stdinRoot: stdinRoot}
	}
	newID := config.NewID
	if newID == nil {
		newID = newProjectProcessID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	manager := &ProjectProcessManager{db: db, stateRoot: root, logRoot: logRoot, workerRoot: workerRoot, stdinRoot: stdinRoot, platform: platform, resolveExecutable: resolveExecutable, maxProcesses: maxProcesses, maxLogBytes: maxLogBytes, newID: newID, now: now, watching: map[string]bool{}}
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
	if len(request.Stdin) > edge.MaxProjectProcessStdinTotalBytes || !utf8.ValidString(request.Stdin) || strings.ContainsRune(request.Stdin, 0) {
		return ProjectProcessSnapshot{}, false, errors.New("project process initial stdin exceeds durable limit")
	}
	digest, err := projectProcessRequestDigest(request)
	if err != nil {
		return ProjectProcessSnapshot{}, false, err
	}
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if existing, err := manager.recordByIdempotency(request.IdempotencyKey); err == nil {
		if existing.RequestDigest != digest {
			return ProjectProcessSnapshot{}, false, ErrProjectProcessIdempotencyConflict
		}
		return manager.snapshot(existing), false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ProjectProcessSnapshot{}, false, errors.New("project process journal unavailable")
	}
	var active int
	if err := manager.db.QueryRow(`SELECT COUNT(*) FROM project_processes WHERE state IN ('starting','running','stopping')`).Scan(&active); err != nil {
		return ProjectProcessSnapshot{}, false, errors.New("project process journal unavailable")
	}
	if active >= manager.maxProcesses {
		return ProjectProcessSnapshot{}, false, ErrProjectProcessLimitReached
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
	// The public direct-workcell contract deliberately rejects Persistent. The
	// durable manager adds the private worker fields below after the ordinary
	// Windows workcell request has been validated; the platform Start method
	// then requires those fields before it can launch the worker.
	spec, err := prepareDirectWorkcellProcessSpec(DirectWorkcellCommandRequest{OperationID: request.OperationID, Workspace: request.Workspace, WindowsDevRoot: request.Workspace.WindowsDevRoot, Argv: request.Argv, CWD: request.CWD, Stdin: request.Stdin, Environment: request.Environment, TimeoutSeconds: 1}, stdoutTarget, stderrTarget, manager.resolveExecutable)
	if err != nil {
		_ = stdoutCloser.Close()
		_ = stderrCloser.Close()
		_ = manager.finishFailed(processID, "process_contract_invalid")
		return ProjectProcessSnapshot{}, false, err
	}
	spec.PersistentProcessID = processID
	spec.PersistentStdin = request.Stdin
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

func (manager *ProjectProcessManager) WriteStdin(request ProjectProcessStdinRequest) (ProjectProcessSnapshot, ProjectProcessStdinReceipt, error) {
	if !projectProcessIDPattern.MatchString(request.ProcessID) || !projectProcessIdempotencyPattern.MatchString(request.FrameID) || request.ExpectedOffset < 0 ||
		request.ExpectedOffset > edge.MaxProjectProcessStdinTotalBytes-int64(len(request.Data)) || len(request.Data) > edge.MaxProjectProcessStdinBytes ||
		!utf8.ValidString(request.Data) || strings.ContainsRune(request.Data, 0) || request.Data == "" && !request.Close {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, errors.New("project process stdin request is invalid")
	}
	if _, redacted := policy.Redact(request.Data); redacted {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, errors.New("project process stdin contains secret material")
	}
	effectLock := manager.projectProcessEffectLock(request.ProcessID)
	effectLock.Lock()
	defer effectLock.Unlock()
	record, err := manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
	if err != nil {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, err
	}
	if record.State != ProjectProcessRunning {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, ErrProjectProcessNotFound
	}
	alive, err := manager.platform.Alive(record.Identity)
	if errors.Is(err, ErrProjectProcessIdentityChanged) || err == nil && !alive {
		_ = manager.reconcileRecord(record)
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, ErrProjectProcessIdentityChanged
	}
	if err != nil {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, errors.New("project process stdin liveness unavailable")
	}
	receipt, err := manager.platform.WriteStdin(record.Identity, ProjectProcessStdinWrite{FrameID: request.FrameID, ExpectedOffset: request.ExpectedOffset, Data: []byte(request.Data), Close: request.Close})
	if err != nil {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, err
	}
	if receipt.NextOffset < 0 || receipt.AcceptedBytes < 0 || receipt.AcceptedBytes > len(request.Data) ||
		receipt.NextOffset != request.ExpectedOffset+int64(receipt.AcceptedBytes) || receipt.AcceptedBytes < len(request.Data) && !receipt.Closed || request.Close && !receipt.Closed {
		return ProjectProcessSnapshot{}, ProjectProcessStdinReceipt{}, errors.New("project process stdin receipt is invalid")
	}
	return manager.snapshot(record), receipt, nil
}

func (manager *ProjectProcessManager) projectProcessEffectLock(processID string) *sync.Mutex {
	index := uint32(0)
	for position := 0; position < len(processID); position++ {
		index = index*33 + uint32(processID[position])
	}
	return &manager.effectLocks[index%uint32(len(manager.effectLocks))]
}

func (manager *ProjectProcessManager) Stop(ctx context.Context, request ProjectProcessStopRequest) (ProjectProcessSnapshot, error) {
	if !projectProcessIDPattern.MatchString(request.ProcessID) || request.GracePeriod <= 0 || request.GracePeriod > 30*time.Second {
		return ProjectProcessSnapshot{}, errors.New("project process stop request is invalid")
	}
	effectLock := manager.projectProcessEffectLock(request.ProcessID)
	effectLock.Lock()
	defer effectLock.Unlock()
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
	effectLock := manager.projectProcessEffectLock(request.ProcessID)
	effectLock.Lock()
	defer effectLock.Unlock()
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
		if err != nil || !validWindowsPrivateProjectProcessFile(path, info) {
			return errors.New("project process log is unsafe")
		}
		if err := os.Remove(path); err != nil {
			return errors.New("project process log cleanup failed")
		}
	}
	for _, kind := range []string{"request", "identity", "child", "ready", "control", "exit", "stdin-receipt"} {
		path := projectProcessWorkerPath(manager.workerRoot, processID, kind)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !validWindowsPrivateProjectProcessFile(path, info) {
			return errors.New("project process worker state is unsafe")
		}
		if err := os.Remove(path); err != nil {
			return errors.New("project process worker cleanup failed")
		}
	}
	socketPath := projectProcessStdinSocketPath(manager.stdinRoot, processID)
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) {
			return errors.New("project process stdin endpoint is unsafe")
		}
		if err := os.Remove(socketPath); err != nil {
			return errors.New("project process stdin cleanup failed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("project process stdin cleanup failed")
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
	if err := securePrivateFile(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
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
		if secureErr := securePrivateFile(file); secureErr != nil {
			_ = file.Close()
			_ = os.Remove(writer.marker)
			return
		}
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
	if markerErr == nil && !validWindowsPrivateProjectProcessFile(path+".truncated", markerInfo) {
		return "", 0, false, false, errors.New("project process log is unsafe")
	}
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return "", 0, false, false, errors.New("project process log unavailable")
	}
	return value, next, next >= info.Size(), truncated, nil
}

func openPrivateProjectProcessLog(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil || !validWindowsPrivateProjectProcessFile(path, before) {
		return nil, nil, errors.New("project process log is unsafe")
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, errors.New("project process log is unsafe")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(after) {
		_ = file.Close()
		return nil, nil, errors.New("project process log is unsafe")
	}
	return file, after, nil
}

type projectProcessWorkerRequest struct {
	Executable  string
	Args        []string
	Dir         string
	Env         []string
	Stdin       string
	MaxLogBytes int64
}
type projectProcessStdinWireRequest struct {
	Identity       ProjectProcessIdentity
	FrameID        string
	ExpectedOffset int64
	Data           string
	Close          bool
}
type projectProcessStdinWireResponse struct {
	NextOffset    int64
	AcceptedBytes int
	Closed        bool
	Reused        bool
	Error         string
}
type projectProcessControlRequest struct {
	Kind     string
	Signal   string
	Identity ProjectProcessIdentity
}

type windowsProjectProcessPlatform struct {
	stateRoot  string
	workerRoot string
	stdinRoot  string
}

func (platform *windowsProjectProcessPlatform) Start(spec DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
	if platform == nil || !projectProcessIDPattern.MatchString(spec.PersistentProcessID) ||
		filepath.Clean(spec.PersistentStateRoot) != filepath.Clean(platform.stateRoot) ||
		spec.PersistentMaxLogBytes < 1 || spec.PersistentMaxLogBytes > 1<<30 {
		return ProjectProcessIdentity{}, nil, errors.New("project process worker contract is invalid")
	}
	request := projectProcessWorkerRequest{Executable: spec.Executable, Args: append([]string(nil), spec.Args...), Dir: spec.Dir, Env: append([]string(nil), spec.Env...), Stdin: spec.PersistentStdin, MaxLogBytes: spec.PersistentMaxLogBytes}
	body, err := json.Marshal(request)
	if err != nil || len(body) > 128<<10 {
		return ProjectProcessIdentity{}, nil, errors.New("project process worker request is invalid")
	}
	if err := writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(platform.workerRoot, spec.PersistentProcessID, "request"), body); err != nil {
		return ProjectProcessIdentity{}, nil, err
	}
	executable, err := os.Executable()
	if err != nil || !filepath.IsAbs(executable) {
		return ProjectProcessIdentity{}, nil, errors.New("project process worker executable is unavailable")
	}
	command := exec.Command(executable, "project-process-worker", "--state", platform.stateRoot, "--process-id", spec.PersistentProcessID)
	command.Dir = platform.stateRoot
	command.Env = []string{"SystemRoot=" + windowsDirectSystemRoot(), "PATH=" + os.Getenv("PATH"), "MCP_DEVBOX_MODE=" + string(WorkspaceModeDev)}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		return ProjectProcessIdentity{}, nil, err
	}
	identity, err := windowsWorkerIdentity(uint32(command.Process.Pid), spec.PersistentProcessID)
	if err != nil {
		_ = command.Process.Kill()
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
	ready := projectProcessWorkerPath(platform.workerRoot, spec.PersistentProcessID, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, statErr := os.Lstat(ready); statErr == nil && info.Mode().IsRegular() && ownedByCurrentUIDPortable(info) {
			_ = os.Remove(ready)
			return identity, exits, nil
		}
		select {
		case <-exits:
			return ProjectProcessIdentity{}, nil, errors.New("project process worker failed before ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	_ = command.Process.Kill()
	return ProjectProcessIdentity{}, nil, errors.New("project process worker readiness timeout")
}

func windowsWorkerIdentity(pid uint32, processID string) (ProjectProcessIdentity, error) {
	if pid == 0 || !projectProcessIDPattern.MatchString(processID) {
		return ProjectProcessIdentity{}, errors.New("project process worker identity is invalid")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ProjectProcessIdentity{}, err
	}
	defer windows.CloseHandle(process)
	identity, err := windowsProcessIdentityFromHandle(process, pid)
	if err != nil {
		return ProjectProcessIdentity{}, err
	}
	return ProjectProcessIdentity{ProcessID: processID, PID: int(identity.ProcessID), ProcessGroupID: int(identity.ProcessID), StartTicks: identity.CreationTime}, nil
}

func (platform *windowsProjectProcessPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	if platform == nil || identity.PID < 1 || identity.ProcessGroupID != identity.PID || identity.StartTicks == 0 {
		return false, ErrProjectProcessIdentityChanged
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(identity.PID))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(process)
	current, err := windowsProcessIdentityFromHandle(process, uint32(identity.PID))
	if err != nil {
		return false, err
	}
	if current.CreationTime != identity.StartTicks {
		return false, ErrProjectProcessIdentityChanged
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return false, err
	}
	return exitCode == windowsProcessStillActive, nil
}

func (platform *windowsProjectProcessPlatform) Signal(identity ProjectProcessIdentity, signal ProjectProcessSignal) error {
	if signal != ProjectProcessInterrupt && signal != ProjectProcessTerminate && signal != ProjectProcessKill {
		return errors.New("project process signal is invalid")
	}
	_, err := platform.sendPipe(identity, projectProcessControlRequest{Kind: "signal", Signal: string(signal), Identity: identity})
	return err
}

func (platform *windowsProjectProcessPlatform) WriteStdin(identity ProjectProcessIdentity, write ProjectProcessStdinWrite) (ProjectProcessStdinReceipt, error) {
	if err := validateWindowsStdinWrite(write); err != nil {
		return ProjectProcessStdinReceipt{}, err
	}
	response, err := platform.sendPipe(identity, projectProcessStdinWireRequest{Identity: identity, FrameID: write.FrameID, ExpectedOffset: write.ExpectedOffset, Data: string(write.Data), Close: write.Close})
	if err != nil {
		return ProjectProcessStdinReceipt{}, err
	}
	switch response.Error {
	case "":
	case "identity_changed":
		return ProjectProcessStdinReceipt{}, ErrProjectProcessIdentityChanged
	case "offset_conflict":
		return ProjectProcessStdinReceipt{}, ErrProjectProcessStdinConflict
	case "stdin_closed":
		return ProjectProcessStdinReceipt{}, ErrProjectProcessStdinClosed
	default:
		return ProjectProcessStdinReceipt{}, errors.New("project process stdin failed")
	}
	if response.AcceptedBytes < 0 || response.AcceptedBytes > len(write.Data) || response.NextOffset != write.ExpectedOffset+int64(response.AcceptedBytes) || (write.Close && !response.Closed) {
		return ProjectProcessStdinReceipt{}, errors.New("project process stdin receipt is invalid")
	}
	return ProjectProcessStdinReceipt{NextOffset: response.NextOffset, AcceptedBytes: response.AcceptedBytes, Closed: response.Closed, Reused: response.Reused}, nil
}

func (platform *windowsProjectProcessPlatform) sendPipe(identity ProjectProcessIdentity, request any) (projectProcessStdinWireResponse, error) {
	if platform == nil || identity.PID < 1 || identity.StartTicks == 0 {
		return projectProcessStdinWireResponse{}, ErrProjectProcessIdentityChanged
	}
	alive, err := platform.Alive(identity)
	if err != nil || !alive {
		if err != nil {
			return projectProcessStdinWireResponse{}, err
		}
		return projectProcessStdinWireResponse{}, ErrProjectProcessIdentityChanged
	}
	name, err := windows.UTF16PtrFromString(windowsProjectProcessPipeName(identity.ProcessID))
	if err != nil {
		return projectProcessStdinWireResponse{}, err
	}
	var handle windows.Handle
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handle, err = windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return projectProcessStdinWireResponse{}, errors.New("project process control endpoint unavailable")
	}
	file := os.NewFile(uintptr(handle), "process-control")
	defer file.Close()
	if err := json.NewEncoder(file).Encode(request); err != nil {
		return projectProcessStdinWireResponse{}, errors.New("project process control write failed")
	}
	var response projectProcessStdinWireResponse
	decoder := json.NewDecoder(io.LimitReader(file, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return projectProcessStdinWireResponse{}, errors.New("project process control response invalid")
	}
	return response, nil
}

func validateWindowsStdinWrite(write ProjectProcessStdinWrite) error {
	if !projectProcessIdempotencyPattern.MatchString(write.FrameID) || write.ExpectedOffset < 0 || len(write.Data) > edge.MaxProjectProcessStdinBytes || write.ExpectedOffset > edge.MaxProjectProcessStdinTotalBytes-int64(len(write.Data)) || !utf8.Valid(write.Data) || strings.ContainsRune(string(write.Data), 0) || len(write.Data) == 0 && !write.Close {
		return errors.New("project process stdin request is invalid")
	}
	return nil
}
func windowsProjectProcessPipeName(processID string) string {
	return "\\\\.\\pipe\\mcp-devbox-project-process-" + processID
}
func projectProcessWorkerPath(workerRoot, processID, kind string) string {
	return filepath.Join(workerRoot, processID+"."+kind+".json")
}

func writePrivateProjectProcessWorkerFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("project process worker state is unavailable")
	}
	if err := securePrivateFile(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
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
	if err != nil || !validWindowsPrivateProjectProcessFile(path, info) || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("project process worker state is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("project process worker state is unsafe")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() > limit {
		return nil, errors.New("project process worker state is unsafe")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("project process worker state is unavailable")
	}
	return body, nil
}
func writeProjectProcessWorkerExit(workerRoot, processID string, exit ProjectProcessExit) error {
	body, err := json.Marshal(exit)
	if err != nil {
		return err
	}
	return writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "exit"), body)
}
func writeProjectProcessWorkerIdentity(workerRoot string, identity ProjectProcessIdentity) error {
	body, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, identity.ProcessID, "identity"), body)
}
func writeProjectProcessWorkerChildIdentity(workerRoot string, identity ProjectProcessIdentity) error {
	body, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, identity.ProcessID, "child"), body)
}
func readProjectProcessWorkerIdentity(workerRoot, processID string) (ProjectProcessIdentity, error) {
	return readProjectProcessWorkerIdentityKindWindows(workerRoot, processID, "identity")
}
func readProjectProcessWorkerChildIdentity(workerRoot, processID string) (ProjectProcessIdentity, error) {
	return readProjectProcessWorkerIdentityKindWindows(workerRoot, processID, "child")
}
func readProjectProcessWorkerIdentityKindWindows(workerRoot, processID, kind string) (ProjectProcessIdentity, error) {
	body, err := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, kind), 2048)
	if err != nil {
		return ProjectProcessIdentity{}, err
	}
	var i ProjectProcessIdentity
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if d.Decode(&i) != nil || i.ProcessID != processID || i.PID < 1 || i.ProcessGroupID != i.PID || i.StartTicks == 0 {
		return ProjectProcessIdentity{}, errors.New("project process worker identity is invalid")
	}
	return i, nil
}
func readProjectProcessWorkerIdentityKind(workerRoot, processID, kind string) (ProjectProcessIdentity, error) {
	return readProjectProcessWorkerIdentityKindWindows(workerRoot, processID, kind)
}
func readProjectProcessWorkerExit(workerRoot, processID string) (ProjectProcessExit, error) {
	body, err := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "exit"), 2048)
	if err != nil {
		return ProjectProcessExit{}, err
	}
	var e ProjectProcessExit
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || (e.ExitKnown && (e.ExitCode < 0 || e.ExitCode > 255)) ||
		(e.TerminalSignal != "" && e.TerminalSignal != ProjectProcessInterrupt && e.TerminalSignal != ProjectProcessTerminate && e.TerminalSignal != ProjectProcessKill) ||
		(e.Reason != "" && !projectProcessReasonPattern.MatchString(e.Reason)) {
		return ProjectProcessExit{}, errors.New("project process worker receipt is invalid")
	}
	return e, nil
}

// Windows uses a named pipe rather than a filesystem socket. This path is a
// non-existent cleanup marker so the shared manager never treats the pipe
// namespace as a host filesystem object.
func projectProcessStdinSocketPath(stdinRoot, processID string) string {
	return filepath.Join(stdinRoot, processID+".named-pipe")
}

type windowsProcessWorkerState struct {
	tree        *WindowsProcessTree
	input       *io.PipeWriter
	identity    ProjectProcessIdentity
	workerRoot  string
	mu          sync.Mutex
	nextOffset  int64
	closed      bool
	lastFrame   string
	lastDigest  string
	lastOffset  int64
	lastClose   bool
	lastReceipt ProjectProcessStdinReceipt
	done        chan struct{}
}
type windowsPersistedStdinReceipt struct {
	FrameID string
	Digest  string
	Offset  int64
	Close   bool
	Receipt ProjectProcessStdinReceipt
}

func RunProjectProcessWorker(stateRoot, processID string) error {
	if !projectProcessIDPattern.MatchString(processID) || !filepath.IsAbs(stateRoot) {
		return errors.New("durable Windows process worker arguments are invalid")
	}
	workerRoot, logRoot := filepath.Join(stateRoot, projectProcessWorkerDirectory), filepath.Join(stateRoot, projectProcessLogDirectory)
	body, err := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "request"), 128<<10)
	if err != nil {
		return err
	}
	var req projectProcessWorkerRequest
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || req.Executable == "" || req.Dir == "" || req.MaxLogBytes < 1 || len(req.Stdin) > edge.MaxProjectProcessStdinTotalBytes {
		return errors.New("project process worker request is invalid")
	}
	stdout, err := openProjectProcessLogWriter(logRoot, processID, "stdout", req.MaxLogBytes)
	if err != nil {
		return err
	}
	stderr, err := openProjectProcessLogWriter(logRoot, processID, "stderr", req.MaxLogBytes)
	if err != nil {
		_ = stdout.Close()
		return err
	}
	ir, iw := io.Pipe()
	cmd := exec.Command(req.Executable, req.Args...)
	cmd.Dir = req.Dir
	cmd.Env = append([]string(nil), req.Env...)
	cmd.Stdin = ir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	tree, err := NewWindowsProcessTree(WindowsProcessTreeLimits{MaxProcesses: 256, MemoryBytes: 512 << 20, CPUTime: 120 * time.Second, WallTime: 10 * time.Minute})
	if err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	wi, err := windowsWorkerIdentity(uint32(os.Getpid()), processID)
	if err != nil {
		_ = tree.Close()
		return err
	}
	if err := writeProjectProcessWorkerIdentity(workerRoot, wi); err != nil {
		_ = tree.Close()
		return err
	}
	state := &windowsProcessWorkerState{tree: tree, input: iw, identity: wi, workerRoot: workerRoot, nextOffset: int64(len(req.Stdin)), done: make(chan struct{})}
	if receiptBody, receiptErr := readPrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "stdin-receipt"), 4096); receiptErr == nil {
		var persisted windowsPersistedStdinReceipt
		decoder := json.NewDecoder(strings.NewReader(string(receiptBody)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&persisted) == nil && projectProcessIdempotencyPattern.MatchString(persisted.FrameID) && len(persisted.Digest) == 64 {
			state.lastFrame, state.lastDigest, state.lastOffset, state.lastClose, state.lastReceipt = persisted.FrameID, persisted.Digest, persisted.Offset, persisted.Close, persisted.Receipt
			state.nextOffset = persisted.Receipt.NextOffset
			state.closed = persisted.Receipt.Closed
		}
	}
	if req.Stdin != "" {
		go func() { _, _ = iw.Write([]byte(req.Stdin)) }()
	}
	if err := tree.Start(context.Background(), cmd); err != nil {
		_ = tree.Close()
		return err
	}
	ci := tree.Identity()
	if err := writeProjectProcessWorkerChildIdentity(workerRoot, ProjectProcessIdentity{ProcessID: processID, PID: int(ci.ProcessID), ProcessGroupID: int(ci.ProcessID), StartTicks: ci.CreationTime}); err != nil {
		_ = tree.Close()
		return err
	}
	if err := writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "ready"), []byte("ready")); err != nil {
		_ = tree.Close()
		return err
	}
	go func() {
		we := tree.Wait()
		_ = iw.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		e := ProjectProcessExit{ExitKnown: false}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			e.ExitKnown = true
			e.ExitCode = cmd.ProcessState.ExitCode()
		}
		if we != nil && !e.ExitKnown {
			e.Reason = "process_wait_failed"
		}
		_ = writeProjectProcessWorkerExit(workerRoot, processID, e)
		close(state.done)
	}()
	for {
		if err := serveOneWindowsProcessPipe(state); errors.Is(err, errWindowsWorkerDone) {
			return nil
		}
	}
}

var errWindowsWorkerDone = errors.New("windows process worker done")

func serveOneWindowsProcessPipe(state *windowsProcessWorkerState) error {
	name, err := windows.UTF16PtrFromString(windowsProjectProcessPipeName(state.identity.ProcessID))
	if err != nil {
		return err
	}
	sidText, sidErr := currentWindowsSIDText()
	if sidErr != nil {
		return sidErr
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + sidText + ")(A;;GA;;;SY)")
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	pipe, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 64<<10, 64<<10, 5000, sa)
	if err != nil {
		return err
	}
	connected := make(chan error, 1)
	go func() { connected <- windows.ConnectNamedPipe(pipe, nil) }()
	select {
	case err = <-connected:
	case <-state.done:
		_ = windows.CloseHandle(pipe)
		return errWindowsWorkerDone
	}
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		_ = windows.CloseHandle(pipe)
		return err
	}
	file := os.NewFile(uintptr(pipe), "process-control")
	defer file.Close()
	dec := json.NewDecoder(io.LimitReader(file, 64<<10))
	dec.DisallowUnknownFields()
	var env map[string]json.RawMessage
	if dec.Decode(&env) != nil {
		return errWindowsPipeClosed
	}
	var out projectProcessStdinWireResponse
	if _, ok := env["Kind"]; ok {
		var req projectProcessControlRequest
		b, _ := json.Marshal(env)
		_ = json.Unmarshal(b, &req)
		if !validWindowsProcessControlRequest(req, state.identity) {
			out.Error = "invalid_control"
		} else if err := state.tree.Terminate(); err != nil && !errors.Is(err, ErrWindowsProcessTreeClosed) {
			out.Error = "signal_failed"
		}
	} else {
		b, _ := json.Marshal(env)
		var req projectProcessStdinWireRequest
		if json.Unmarshal(b, &req) != nil || req.Identity != state.identity {
			out.Error = "identity_changed"
		} else {
			out = handleWindowsWorkerStdin(state, req)
		}
	}
	_ = json.NewEncoder(file).Encode(out)
	return nil
}

func validWindowsProcessControlRequest(request projectProcessControlRequest, identity ProjectProcessIdentity) bool {
	return request.Kind == "signal" && request.Identity == identity &&
		(request.Signal == string(ProjectProcessInterrupt) || request.Signal == string(ProjectProcessTerminate) || request.Signal == string(ProjectProcessKill))
}

var errWindowsPipeClosed = errors.New("windows process pipe closed")

func currentWindowsSIDText() (string, error) {
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		return "", err
	}
	defer token.Close()
	return sid.String(), nil
}
func handleWindowsWorkerStdin(state *windowsProcessWorkerState, req projectProcessStdinWireRequest) projectProcessStdinWireResponse {
	if !projectProcessIdempotencyPattern.MatchString(req.FrameID) || req.ExpectedOffset < 0 || len(req.Data) > edge.MaxProjectProcessStdinBytes || req.ExpectedOffset > edge.MaxProjectProcessStdinTotalBytes-int64(len(req.Data)) || !utf8.ValidString(req.Data) || strings.ContainsRune(req.Data, 0) || len(req.Data) == 0 && !req.Close {
		return projectProcessStdinWireResponse{Error: "invalid_request"}
	}
	sum := sha256.Sum256([]byte(req.Data))
	digest := hex.EncodeToString(sum[:])
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.lastFrame == req.FrameID {
		if state.lastDigest == digest && state.lastOffset == req.ExpectedOffset && state.lastClose == req.Close {
			r := state.lastReceipt
			r.Reused = true
			return projectProcessStdinWireResponse{NextOffset: r.NextOffset, AcceptedBytes: r.AcceptedBytes, Closed: r.Closed, Reused: true}
		}
		return projectProcessStdinWireResponse{Error: "offset_conflict"}
	}
	if state.closed {
		return projectProcessStdinWireResponse{Error: "stdin_closed"}
	}
	if req.ExpectedOffset != state.nextOffset {
		return projectProcessStdinWireResponse{Error: "offset_conflict"}
	}
	accepted := len(req.Data)
	if accepted > 0 {
		done := make(chan error, 1)
		go func() { _, e := state.input.Write([]byte(req.Data)); done <- e }()
		select {
		case e := <-done:
			if e != nil {
				accepted = 0
				state.closed = true
			}
		case <-time.After(4 * time.Second):
			_ = state.input.Close()
			accepted = 0
			state.closed = true
		}
	}
	state.nextOffset += int64(accepted)
	if req.Close || accepted != len(req.Data) {
		state.closed = true
		_ = state.input.Close()
	}
	r := ProjectProcessStdinReceipt{NextOffset: state.nextOffset, AcceptedBytes: accepted, Closed: state.closed}
	state.lastFrame, state.lastDigest, state.lastOffset, state.lastClose, state.lastReceipt = req.FrameID, digest, req.ExpectedOffset, req.Close, r
	_ = persistWindowsStdinReceipt(state, windowsPersistedStdinReceipt{FrameID: req.FrameID, Digest: digest, Offset: req.ExpectedOffset, Close: req.Close, Receipt: r})
	return projectProcessStdinWireResponse{NextOffset: r.NextOffset, AcceptedBytes: r.AcceptedBytes, Closed: r.Closed}
}

func persistWindowsStdinReceipt(state *windowsProcessWorkerState, persisted windowsPersistedStdinReceipt) error {
	body, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	path := projectProcessWorkerPath(state.workerRoot, state.identity.ProcessID, "stdin-receipt")
	if info, statErr := os.Lstat(path); statErr == nil {
		if !validWindowsPrivateProjectProcessFile(path, info) {
			return errors.New("project process stdin receipt is unsafe")
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
		if openErr != nil {
			return openErr
		}
		_, writeErr := file.Write(append(body, '\n'))
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	return writePrivateProjectProcessWorkerFile(path, body)
}

func validWindowsPrivateProjectProcessFile(path string, info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 &&
		ownedByCurrentUIDPortable(info) && requirePrivateRegularFile(path) == nil
}
