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
	"path/filepath"
	"regexp"
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
)

var (
	projectProcessIDPattern              = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	projectProcessIdempotencyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	ErrProjectProcessNotFound            = errors.New("project process not found")
	ErrProjectProcessIdempotencyConflict = errors.New("project process idempotency conflict")
	ErrProjectProcessIdentityChanged     = errors.New("project process identity changed")
)

type ProjectProcessIdentity struct {
	PID            int
	ProcessGroupID int
	StartTicks     uint64
}

type ProjectProcessExit struct {
	ExitKnown      bool
	ExitCode       int
	TerminalSignal ProjectProcessSignal
	Reason         string
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
	db                 *sql.DB
	stateRoot, logRoot string
	platform           ProjectProcessPlatform
	resolveExecutable  bool
	maxProcesses       int
	maxLogBytes        int64
	newID              func() (string, error)
	now                func() time.Time
	watchMu            sync.Mutex
	watching           map[string]bool
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
		platform = osProjectProcessPlatform{}
	}
	newID := config.NewID
	if newID == nil {
		newID = newProjectProcessID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ProjectProcessManager{db: db, stateRoot: root, logRoot: logRoot, platform: platform, resolveExecutable: resolveExecutable, maxProcesses: maxProcesses, maxLogBytes: maxLogBytes, newID: newID, now: now, watching: map[string]bool{}}, nil
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
	stdoutWriter, err := manager.openLogWriter(processID, "stdout")
	if err != nil {
		return ProjectProcessSnapshot{}, false, err
	}
	stderrWriter, err := manager.openLogWriter(processID, "stderr")
	if err != nil {
		_ = stdoutWriter.Close()
		return ProjectProcessSnapshot{}, false, err
	}
	started := manager.now().UTC()
	record := projectProcessRecord{ProcessID: processID, IdempotencyKey: request.IdempotencyKey, RequestDigest: digest, OperationID: request.OperationID, WorkspaceID: request.Workspace.ID, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias, State: ProjectProcessStarting, StartedAt: started}
	if err := manager.insertRecord(record); err != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		return ProjectProcessSnapshot{}, false, err
	}
	spec, err := prepareDirectWorkcellProcessSpec(DirectWorkcellCommandRequest{OperationID: request.OperationID, Workspace: request.Workspace, Argv: request.Argv, CWD: request.CWD, Stdin: request.Stdin, Environment: request.Environment, TimeoutSeconds: 1}, stdoutWriter, stderrWriter, manager.resolveExecutable)
	if err != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = manager.finishFailed(processID, "process_contract_invalid")
		return ProjectProcessSnapshot{}, false, err
	}
	if err := ctx.Err(); err != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = manager.finishFailed(processID, "process_start_cancelled")
		return ProjectProcessSnapshot{}, false, err
	}
	identity, exits, err := manager.platform.Start(spec)
	if err != nil || identity.PID < 1 || identity.ProcessGroupID < 1 || identity.StartTicks == 0 || exits == nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = manager.finishFailed(processID, "process_start_failed")
		return ProjectProcessSnapshot{}, false, errors.New("project process start failed")
	}
	if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=? AND state=?`, identity.PID, identity.ProcessGroupID, identity.StartTicks, ProjectProcessRunning, processID, ProjectProcessStarting); err != nil {
		_ = manager.platform.Signal(identity, ProjectProcessKill)
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		return ProjectProcessSnapshot{}, false, errors.New("project process journal unavailable")
	}
	record.Identity = identity
	record.State = ProjectProcessRunning
	manager.watchMu.Lock()
	manager.watching[processID] = true
	manager.watchMu.Unlock()
	go manager.watch(processID, exits, stdoutWriter, stderrWriter)
	return manager.snapshot(record), true, nil
}

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
			_ = manager.finishFailed(record.ProcessID, "process_identity_changed")
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
				_ = manager.finishFailed(record.ProcessID, "process_lost")
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
		_ = manager.finishFailed(record.ProcessID, "process_identity_changed")
		record, _ = manager.boundRecord(request.ProcessID, request.ProjectAlias, request.TargetAlias)
		return manager.snapshot(record), nil
	}
	if err != nil || !alive {
		_ = manager.finishFailed(record.ProcessID, "process_lost")
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
	path := filepath.Join(manager.logRoot, processID+"."+stream+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("project process log unavailable")
	}
	return &projectProcessLogWriter{file: file, marker: path + ".truncated", max: manager.maxLogBytes}, nil
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

type osProjectProcessPlatform struct{}

func (osProjectProcessPlatform) Start(spec DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
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
	identity := ProjectProcessIdentity{PID: command.Process.Pid, ProcessGroupID: group, StartTicks: startTicks}
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
func (osProjectProcessPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	ticks, err := linuxProcessStartTicks(identity.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	group, err := syscall.Getpgid(identity.PID)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
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
	if err := syscall.Kill(-identity.ProcessGroupID, unix); errors.Is(err, syscall.ESRCH) {
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
