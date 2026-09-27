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
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

const (
	projectWorktreeTestProfileFile       = "worktree-test-profile.json"
	projectWorktreeTestProfileMaxBytes   = 16 << 10
	projectWorktreeTestMaxArgBytes       = 4096
	projectWorktreeTestMaxArgvBytes      = 8 << 10
	projectWorktreeTestStopGrace         = 2 * time.Second
	projectWorktreeTestProfileDomain     = "mcp-devbox-worktree-test-profile-v1\x00"
	projectWorktreeTestRequestDomain     = "mcp-devbox-worktree-test-request-v1\x00"
	projectWorktreeTestProfileMaxSeconds = 3600
)

var (
	projectWorktreeTestProfileIDRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	projectWorktreeTestProcessIDRE  = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	projectWorktreeTestJobIDRE      = regexp.MustCompile(`^wj_[a-f0-9]{32}$`)
	projectWorktreeTestLeaseIDRE    = regexp.MustCompile(`^wl_[a-f0-9]{32}$`)
	projectWorktreeTestWorktreeIDRE = regexp.MustCompile(`^wt_[a-f0-9]{32}$`)
	projectWorktreeTestCommitRE     = regexp.MustCompile(`^[a-f0-9]{40}$`)
	projectWorktreeTestDigestRE     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	projectWorktreeTestAliasRE      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	projectWorktreeTestTargetRE     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
	projectWorktreeTestKeyRE        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

	ErrProjectWorktreeTestUnavailable        = errors.New("managed worktree test process is unavailable")
	ErrProjectWorktreeTestProfileUnavailable = errors.New("managed worktree test profile is unavailable")
	ErrProjectWorktreeTestProfileMismatch    = errors.New("managed worktree test profile does not match")
	ErrProjectWorktreeTestNotFound           = errors.New("managed worktree test process not found")
	ErrProjectWorktreeTestConflict           = errors.New("managed worktree test request conflicts")
	ErrProjectWorktreeTestAttemptExists      = errors.New("managed worktree test attempt already exists")
	ErrProjectWorktreeTestStaleFence         = errors.New("managed worktree test lease or fence is stale")
)

// ProjectWorktreeTestProfileDocument is stored only in the Edge's private
// state root. Its argv is operator-owned; task requests select the immutable
// profile by ID and digest and cannot provide argv, cwd, stdin, or environment.
type ProjectWorktreeTestProfileDocument struct {
	Version        int      `json:"version"`
	ProfileID      string   `json:"profile_id"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type ProjectWorktreeTestProfile struct {
	ID             string
	Digest         string
	TimeoutSeconds int
	argv           []string
}

type ProjectWorktreeTestStartRequest struct {
	OperationID, IdempotencyKey, Alias, TargetAlias string
	WorktreeID, JobID, LeaseID                      string
	Fence                                           uint64
	ProfileID, ProfileDigest                        string
}

type ProjectWorktreeTestReadRequest struct {
	ProcessID, Alias, TargetAlias, WorktreeID, JobID, LeaseID string
	ProfileID, ProfileDigest                                  string
	Fence                                                     uint64
	StdoutOffset, StderrOffset                                int64
	LimitBytes                                                int
}

type ProjectWorktreeTestStopRequest struct {
	ProcessID, Alias, TargetAlias, WorktreeID, JobID, LeaseID string
	ProfileID, ProfileDigest                                  string
	Fence                                                     uint64
}

type ProjectWorktreeTestSnapshot struct {
	ProcessID, State, ProfileID, ProfileDigest    string
	ContentDigest, BaseCommit, HeadCommit, Branch string
	WorktreeID, WorkspaceID, JobID, LeaseID       string
	Alias, TargetAlias                            string
	Fence                                         uint64
	TimeoutSeconds                                int
	StartedAt, FinishedAt                         time.Time
	ExitKnown                                     bool
	ExitCode                                      int
	TerminalSignal, Reason                        string
	Stdout, Stderr                                string
	StdoutNext, StderrNext                        int64
	StdoutEOF, StderrEOF                          bool
	StdoutTruncated, StderrTruncated              bool
	Stale                                         bool
	StaleReason                                   string
	TimedOut                                      bool
}

type ProjectWorktreeTestProcessManagerConfig struct {
	StateRoot  string
	Processes  *ProjectProcessManager
	Now        func() time.Time
	Generation string // test seam; production generates a fresh value per Edge process.
}

type ProjectWorktreeTestProcessManager struct {
	stateRoot  string
	processes  *ProjectProcessManager
	generation string
	now        func() time.Time
	mu         sync.Mutex
	closed     bool
	timers     map[string]*time.Timer
}

type projectWorktreeTestRecord struct {
	ProcessID, IdempotencyKey, ProcessKey, RequestDigest, OperationID       string
	Alias, TargetAlias, WorkspaceID, WorktreeID, JobID, LeaseID             string
	Fence                                                                   uint64
	ProfileID, ProfileDigest, ContentDigest, BaseCommit, HeadCommit, Branch string
	EdgeGeneration, InvalidatedReason                                       string
	TimeoutSeconds                                                          int
	StartedAt                                                               time.Time
	TimedOut                                                                bool
}

func OpenProjectWorktreeTestProcessManager(config ProjectWorktreeTestProcessManagerConfig) (*ProjectWorktreeTestProcessManager, error) {
	root := filepath.Clean(strings.TrimSpace(config.StateRoot))
	if !filepath.IsAbs(root) || root == "." || root == string(filepath.Separator) || config.Processes == nil || config.Processes.db == nil {
		return nil, ErrProjectWorktreeTestUnavailable
	}
	generation := config.Generation
	if generation == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, ErrProjectWorktreeTestUnavailable
		}
		generation = hex.EncodeToString(raw[:])
	}
	if len(generation) > 64 || generation == "" {
		return nil, ErrProjectWorktreeTestUnavailable
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	manager := &ProjectWorktreeTestProcessManager{stateRoot: root, processes: config.Processes, generation: generation, now: now, timers: map[string]*time.Timer{}}
	if err := manager.initialize(); err != nil {
		return nil, err
	}
	if err := manager.invalidatePriorGeneration(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (manager *ProjectWorktreeTestProcessManager) Close() error {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	for processID, timer := range manager.timers {
		timer.Stop()
		delete(manager.timers, processID)
	}
	manager.mu.Unlock()
	return manager.invalidateGeneration(manager.generation)
}

func (manager *ProjectWorktreeTestProcessManager) Profile(profileID string) (ProjectWorktreeTestProfile, error) {
	profile, err := readProjectWorktreeTestProfile(manager.stateRoot)
	if err != nil {
		return ProjectWorktreeTestProfile{}, err
	}
	if profileID != "" && profileID != profile.ID {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileMismatch
	}
	return profile, nil
}

func (manager *ProjectWorktreeTestProcessManager) Start(ctx context.Context, worktrees *ProjectWorktreeManager, request ProjectWorktreeTestStartRequest) (ProjectWorktreeTestSnapshot, bool, error) {
	if manager == nil || manager.processes == nil || worktrees == nil || ctx == nil || !validProjectWorktreeTestStartRequest(request) {
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestConflict
	}
	profile, err := manager.Profile(request.ProfileID)
	if err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	}
	if profile.Digest != request.ProfileDigest {
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestProfileMismatch
	}
	requestDigest := projectWorktreeTestRequestDigest(request, profile)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestUnavailable
	}
	if existing, found, err := manager.recordByIdempotency(request.IdempotencyKey); err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	} else if found {
		if existing.RequestDigest != requestDigest || existing.WorktreeID != request.WorktreeID || existing.JobID != request.JobID || existing.LeaseID != request.LeaseID || existing.Fence != request.Fence || existing.ProfileDigest != profile.Digest {
			return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestConflict
		}
		if existing.EdgeGeneration != manager.generation || existing.InvalidatedReason != "" || existing.ProcessID == "" {
			return projectWorktreeTestSnapshot(existing, ProjectProcessSnapshot{ProcessID: existing.ProcessID, State: ProjectProcessFailed, StartedAt: existing.StartedAt, FinishedAt: existing.StartedAt, Reason: "edge_restarted"}, true, "edge_restarted"), false, ErrProjectWorktreeTestUnavailable
		}
		process, err := manager.processStatus(existing, 0, 0, 1)
		if err != nil {
			return ProjectWorktreeTestSnapshot{}, false, err
		}
		return projectWorktreeTestSnapshot(existing, process, false, ""), false, nil
	}
	if _, found, err := manager.recordByAuthority(request.WorktreeID, request.JobID, request.LeaseID, request.Fence); err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	} else if found {
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestAttemptExists
	}
	active, err := manager.hasActiveAttempt(request.WorktreeID)
	if err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	}
	if active {
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestAttemptExists
	}
	if err := ctx.Err(); err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	}
	initial, workspace, roots, err := captureProjectWorktreeTestInput(ctx, worktrees, request)
	if err != nil {
		return ProjectWorktreeTestSnapshot{}, false, err
	}
	processKey := projectWorktreeTestProcessKey(request.IdempotencyKey)
	started := manager.now().UTC()
	record := projectWorktreeTestRecord{
		IdempotencyKey: request.IdempotencyKey, ProcessKey: processKey, RequestDigest: requestDigest, OperationID: request.OperationID,
		Alias: initial.Alias, TargetAlias: initial.TargetAlias, WorkspaceID: workspace.ID, WorktreeID: initial.ID, JobID: request.JobID,
		LeaseID: request.LeaseID, Fence: request.Fence, ProfileID: profile.ID, ProfileDigest: profile.Digest,
		ContentDigest: initial.ContentDigest, BaseCommit: initial.BaseCommit, HeadCommit: initial.HeadCommit, Branch: initial.Branch,
		EdgeGeneration: manager.generation, TimeoutSeconds: profile.TimeoutSeconds, StartedAt: started,
	}
	if err := manager.insertRecord(record); err != nil {
		if existing, found, lookupErr := manager.recordByIdempotency(request.IdempotencyKey); lookupErr == nil && found && existing.RequestDigest == requestDigest {
			return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestAttemptExists
		}
		return ProjectWorktreeTestSnapshot{}, false, err
	}
	process, created, err := manager.processes.Start(ctx, ProjectProcessStartRequest{
		OperationID: request.OperationID, IdempotencyKey: processKey,
		ProjectAlias: projectWorktreeTestSyntheticAlias(initial.ID), TargetAlias: initial.TargetAlias,
		ProjectOwner: projectWorktreeTestOwner(initial.Repository), ProjectRepository: projectWorktreeTestRepository(initial.Repository),
		ProjectClaimGeneration: request.Fence, ProjectState: string(ProjectCheckoutReady), Workspace: workspace, WorkspaceRoots: roots,
		Argv: append([]string(nil), profile.argv...), CWD: "", Stdin: "", Environment: nil,
	})
	if err != nil {
		_ = manager.updateFailure(record.IdempotencyKey, "process_start_failed")
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestUnavailable
	}
	record.ProcessID = process.ProcessID
	if err := manager.setProcessID(record.IdempotencyKey, record.ProcessID); err != nil {
		_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, process.ProcessID))
		return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestUnavailable
	}
	// Test commands are non-interactive. Close the private process stdin so a
	// prompt cannot leave an otherwise bounded acceptance run waiting forever.
	_, _, stdinErr := manager.processes.WriteStdin(ProjectProcessStdinRequest{
		ProcessID: process.ProcessID, ProjectAlias: projectWorktreeTestSyntheticAlias(initial.ID), TargetAlias: initial.TargetAlias,
		WorkspaceID: workspace.ID, FrameID: "wttest-close-" + strings.TrimPrefix(processKey, "wttest-"), Close: true,
	})
	if stdinErr != nil {
		// A non-interactive command may have already exited before its stdin
		// can be closed. Preserve a journaled, known exit rather than turning
		// that legitimate result into a synthetic start failure.
		current, statusErr := manager.processStatus(record, 0, 0, 1)
		if statusErr != nil || current.State != ProjectProcessExited || !current.ExitKnown {
			_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, process.ProcessID))
			_ = manager.updateFailure(record.IdempotencyKey, "process_start_failed")
			return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestUnavailable
		}
		process = current
	}
	if !projectProcessTerminal(process.State) {
		if err := manager.scheduleTimeout(record); err != nil {
			_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, process.ProcessID))
			_ = manager.updateFailure(record.IdempotencyKey, "process_start_failed")
			return ProjectWorktreeTestSnapshot{}, false, err
		}
	}
	if !created {
		// A process journal record without its corresponding test binding is
		// recovered only through this exact idempotency key and input digest.
		binding, bindErr := manager.processes.Binding(process.ProcessID)
		if bindErr != nil || binding.ProjectAlias != projectWorktreeTestSyntheticAlias(initial.ID) || binding.WorkspaceID != workspace.ID {
			_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, process.ProcessID))
			_ = manager.updateFailure(record.IdempotencyKey, "process_start_failed")
			return ProjectWorktreeTestSnapshot{}, false, ErrProjectWorktreeTestConflict
		}
	}
	return projectWorktreeTestSnapshot(record, process, false, ""), true, nil
}

func (manager *ProjectWorktreeTestProcessManager) Status(ctx context.Context, worktrees *ProjectWorktreeManager, request ProjectWorktreeTestReadRequest) (ProjectWorktreeTestSnapshot, error) {
	if manager == nil || manager.processes == nil || ctx == nil || !validProjectWorktreeTestReadRequest(request) {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestConflict
	}
	record, found, err := manager.recordByProcessID(request.ProcessID)
	if err != nil || !found || !sameProjectWorktreeTestAuthority(record, request.WorktreeID, request.JobID, request.LeaseID, request.Fence) || record.Alias != request.Alias || record.TargetAlias != request.TargetAlias || record.ProfileID != request.ProfileID || record.ProfileDigest != request.ProfileDigest {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestNotFound
	}
	process, err := manager.processStatus(record, request.StdoutOffset, request.StderrOffset, request.LimitBytes)
	if err != nil {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestUnavailable
	}
	stale, reason := manager.currentEvidence(ctx, worktrees, record)
	return projectWorktreeTestSnapshot(record, process, stale, reason), nil
}

func (manager *ProjectWorktreeTestProcessManager) Stop(ctx context.Context, request ProjectWorktreeTestStopRequest) (ProjectWorktreeTestSnapshot, error) {
	if manager == nil || manager.processes == nil || ctx == nil || !validProjectWorktreeTestStopRequest(request) {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestConflict
	}
	record, found, err := manager.recordByProcessID(request.ProcessID)
	if err != nil || !found || !sameProjectWorktreeTestAuthority(record, request.WorktreeID, request.JobID, request.LeaseID, request.Fence) || record.Alias != request.Alias || record.TargetAlias != request.TargetAlias || record.ProfileID != request.ProfileID || record.ProfileDigest != request.ProfileDigest {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestNotFound
	}
	// Stop deliberately uses only the persisted process identity and captured
	// worktree/job/lease/fence binding. A dirty, changed, or unavailable checkout
	// must never strand a process that this journal already owns.
	process, err := manager.processes.Stop(ctx, projectWorktreeTestStopRequest(record, request.ProcessID))
	if err != nil {
		return ProjectWorktreeTestSnapshot{}, ErrProjectWorktreeTestUnavailable
	}
	manager.stopTimer(request.ProcessID)
	_ = manager.setProcessState(request.ProcessID, process.State)
	staleReason := record.InvalidatedReason
	if staleReason == "" {
		// Stop intentionally does not inspect mutable worktree contents. Its
		// result therefore cannot be used as current acceptance evidence.
		staleReason = "evidence_unavailable"
	}
	return projectWorktreeTestSnapshot(record, process, true, staleReason), nil
}

func (manager *ProjectWorktreeTestProcessManager) initialize() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS worktree_test_processes (
			idempotency_key TEXT PRIMARY KEY, process_id TEXT NOT NULL DEFAULT '', process_key TEXT NOT NULL UNIQUE, request_digest TEXT NOT NULL,
			operation_id TEXT NOT NULL, project_alias TEXT NOT NULL, target_alias TEXT NOT NULL, workspace_id TEXT NOT NULL,
			worktree_id TEXT NOT NULL, job_id TEXT NOT NULL, lease_id TEXT NOT NULL, fence INTEGER NOT NULL,
			profile_id TEXT NOT NULL, profile_digest TEXT NOT NULL, timeout_seconds INTEGER NOT NULL,
			content_digest TEXT NOT NULL, base_commit TEXT NOT NULL, head_commit TEXT NOT NULL, branch TEXT NOT NULL, edge_generation TEXT NOT NULL,
			started_at INTEGER NOT NULL, timed_out INTEGER NOT NULL DEFAULT 0, invalidated_reason TEXT NOT NULL DEFAULT '',
			process_state TEXT NOT NULL DEFAULT 'starting',
			UNIQUE(worktree_id,job_id)
		) WITHOUT ROWID`,
		`CREATE UNIQUE INDEX IF NOT EXISTS worktree_test_process_id ON worktree_test_processes(process_id) WHERE process_id<>''`,
		`CREATE INDEX IF NOT EXISTS worktree_test_process_generation ON worktree_test_processes(edge_generation,invalidated_reason)`,
	}
	for _, statement := range statements {
		if _, err := manager.processes.db.Exec(statement); err != nil {
			return ErrProjectWorktreeTestUnavailable
		}
	}
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) invalidatePriorGeneration() error {
	rows, err := manager.processes.db.Query(`SELECT `+projectWorktreeTestSelect+` FROM worktree_test_processes WHERE edge_generation<>? AND invalidated_reason=''`, manager.generation)
	if err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	records := make([]projectWorktreeTestRecord, 0)
	for rows.Next() {
		record, scanErr := scanProjectWorktreeTestRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return ErrProjectWorktreeTestUnavailable
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	for _, record := range records {
		if _, err := manager.processes.db.Exec(`UPDATE worktree_test_processes SET invalidated_reason='edge_restarted' WHERE idempotency_key=? AND invalidated_reason=''`, record.IdempotencyKey); err != nil {
			return ErrProjectWorktreeTestUnavailable
		}
		if record.ProcessID == "" {
			if process, lookupErr := manager.processes.recordByIdempotency(record.ProcessKey); lookupErr == nil {
				record.ProcessID = process.ProcessID
				_ = manager.setProcessID(record.IdempotencyKey, process.ProcessID)
			} else {
				continue
			}
		}
		if process, statusErr := manager.processStatus(record, 0, 0, 1); statusErr == nil && !projectProcessTerminal(process.State) {
			_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, record.ProcessID))
		}
	}
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) invalidateGeneration(generation string) error {
	rows, err := manager.processes.db.Query(`SELECT `+projectWorktreeTestSelect+` FROM worktree_test_processes WHERE edge_generation=? AND invalidated_reason=''`, generation)
	if err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	records := make([]projectWorktreeTestRecord, 0)
	for rows.Next() {
		record, scanErr := scanProjectWorktreeTestRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return ErrProjectWorktreeTestUnavailable
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	for _, record := range records {
		if _, err := manager.processes.db.Exec(`UPDATE worktree_test_processes SET invalidated_reason='edge_restarted' WHERE idempotency_key=? AND invalidated_reason=''`, record.IdempotencyKey); err != nil {
			return ErrProjectWorktreeTestUnavailable
		}
		manager.stopTimer(record.ProcessID)
		if record.ProcessID != "" {
			if process, statusErr := manager.processStatus(record, 0, 0, 1); statusErr == nil && !projectProcessTerminal(process.State) {
				_, _ = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, record.ProcessID))
			}
		}
	}
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) processStatus(record projectWorktreeTestRecord, stdoutOffset, stderrOffset int64, limit int) (ProjectProcessSnapshot, error) {
	if record.ProcessID == "" {
		return ProjectProcessSnapshot{}, ErrProjectWorktreeTestNotFound
	}
	return manager.processes.Status(ProjectProcessReadRequest{
		ProcessID: record.ProcessID, ProjectAlias: projectWorktreeTestSyntheticAlias(record.WorktreeID),
		TargetAlias: record.TargetAlias, WorkspaceID: record.WorkspaceID,
		StdoutOffset: stdoutOffset, StderrOffset: stderrOffset, LimitBytes: limit,
	})
}

func (manager *ProjectWorktreeTestProcessManager) currentEvidence(ctx context.Context, worktrees *ProjectWorktreeManager, record projectWorktreeTestRecord) (bool, string) {
	if record.EdgeGeneration != manager.generation || record.InvalidatedReason != "" {
		return true, "edge_restarted"
	}
	profile, err := manager.Profile(record.ProfileID)
	if err != nil || profile.Digest != record.ProfileDigest {
		return true, "profile_changed"
	}
	if worktrees == nil {
		return true, "evidence_unavailable"
	}
	request := ProjectWorktreeClaimRequest{ID: record.WorktreeID, JobID: record.JobID, LeaseID: record.LeaseID, Fence: record.Fence}
	snapshot, err := worktrees.Status(ctx, record.WorktreeID)
	if err != nil {
		return true, "evidence_unavailable"
	}
	if snapshot.JobID != record.JobID || snapshot.LeaseID != record.LeaseID || snapshot.Fence != record.Fence {
		return true, "evidence_unavailable"
	}
	if snapshot.HeadCommit != record.HeadCommit {
		return true, "head_changed"
	}
	if snapshot.Branch != record.Branch {
		return true, "branch_changed"
	}
	digest, err := worktrees.ContentDigest(ctx, request)
	if err != nil {
		return true, "evidence_unavailable"
	}
	if digest != record.ContentDigest {
		return true, "content_changed"
	}
	return false, ""
}

func (manager *ProjectWorktreeTestProcessManager) scheduleTimeout(record projectWorktreeTestRecord) error {
	if record.ProcessID == "" || record.TimeoutSeconds < 1 || record.TimeoutSeconds > projectWorktreeTestProfileMaxSeconds {
		return ErrProjectWorktreeTestUnavailable
	}
	manager.timers[record.ProcessID] = time.AfterFunc(time.Duration(record.TimeoutSeconds)*time.Second, func() {
		manager.timeoutProcess(record.ProcessID)
	})
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) timeoutProcess(processID string) {
	record, found, err := manager.recordByProcessID(processID)
	if err != nil || !found || record.EdgeGeneration != manager.generation || record.InvalidatedReason != "" {
		return
	}
	process, err := manager.processStatus(record, 0, 0, 1)
	if err != nil || projectProcessTerminal(process.State) {
		return
	}
	_, err = manager.processes.Stop(context.Background(), projectWorktreeTestStopRequest(record, processID))
	if err == nil {
		_, _ = manager.processes.db.Exec(`UPDATE worktree_test_processes SET timed_out=1,process_state=? WHERE process_id=?`, ProjectProcessStopped, processID)
	}
}

func (manager *ProjectWorktreeTestProcessManager) stopTimer(processID string) {
	manager.mu.Lock()
	if timer := manager.timers[processID]; timer != nil {
		timer.Stop()
		delete(manager.timers, processID)
	}
	manager.mu.Unlock()
}

func (manager *ProjectWorktreeTestProcessManager) hasActiveAttempt(worktreeID string) (bool, error) {
	var count int
	err := manager.processes.db.QueryRow(`SELECT COUNT(*) FROM worktree_test_processes t JOIN project_processes p ON p.process_id=t.process_id WHERE t.worktree_id=? AND p.state IN ('starting','running','stopping')`, worktreeID).Scan(&count)
	return count != 0, err
}

func (manager *ProjectWorktreeTestProcessManager) recordByIdempotency(key string) (projectWorktreeTestRecord, bool, error) {
	record, err := scanProjectWorktreeTestRecord(manager.processes.db.QueryRow(`SELECT `+projectWorktreeTestSelect+` FROM worktree_test_processes WHERE idempotency_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return projectWorktreeTestRecord{}, false, nil
	}
	if err != nil {
		return projectWorktreeTestRecord{}, false, ErrProjectWorktreeTestUnavailable
	}
	return record, true, nil
}

func (manager *ProjectWorktreeTestProcessManager) recordByProcessID(processID string) (projectWorktreeTestRecord, bool, error) {
	record, err := scanProjectWorktreeTestRecord(manager.processes.db.QueryRow(`SELECT `+projectWorktreeTestSelect+` FROM worktree_test_processes WHERE process_id=?`, processID))
	if errors.Is(err, sql.ErrNoRows) {
		return projectWorktreeTestRecord{}, false, nil
	}
	if err != nil {
		return projectWorktreeTestRecord{}, false, ErrProjectWorktreeTestUnavailable
	}
	return record, true, nil
}

func (manager *ProjectWorktreeTestProcessManager) recordByAuthority(worktreeID, jobID, leaseID string, fence uint64) (projectWorktreeTestRecord, bool, error) {
	record, err := scanProjectWorktreeTestRecord(manager.processes.db.QueryRow(`SELECT `+projectWorktreeTestSelect+` FROM worktree_test_processes WHERE worktree_id=? AND job_id=? AND lease_id=? AND fence=?`, worktreeID, jobID, leaseID, fence))
	if errors.Is(err, sql.ErrNoRows) {
		return projectWorktreeTestRecord{}, false, nil
	}
	if err != nil {
		return projectWorktreeTestRecord{}, false, ErrProjectWorktreeTestUnavailable
	}
	return record, true, nil
}

func (manager *ProjectWorktreeTestProcessManager) insertRecord(record projectWorktreeTestRecord) error {
	_, err := manager.processes.db.Exec(`INSERT INTO worktree_test_processes(idempotency_key,process_key,request_digest,operation_id,project_alias,target_alias,workspace_id,worktree_id,job_id,lease_id,fence,profile_id,profile_digest,timeout_seconds,content_digest,base_commit,head_commit,branch,edge_generation,started_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.IdempotencyKey, record.ProcessKey, record.RequestDigest, record.OperationID, record.Alias, record.TargetAlias,
		record.WorkspaceID, record.WorktreeID, record.JobID, record.LeaseID, record.Fence, record.ProfileID, record.ProfileDigest,
		record.TimeoutSeconds, record.ContentDigest, record.BaseCommit, record.HeadCommit, record.Branch, record.EdgeGeneration, record.StartedAt.UnixNano())
	if err != nil {
		return ErrProjectWorktreeTestAttemptExists
	}
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) setProcessID(key, processID string) error {
	if !projectWorktreeTestProcessIDRE.MatchString(processID) {
		return ErrProjectWorktreeTestUnavailable
	}
	_, err := manager.processes.db.Exec(`UPDATE worktree_test_processes SET process_id=?,process_state=? WHERE idempotency_key=?`, processID, ProjectProcessRunning, key)
	if err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) updateFailure(key, reason string) error {
	_, err := manager.processes.db.Exec(`UPDATE worktree_test_processes SET process_state=? WHERE idempotency_key=?`, ProjectProcessFailed, key)
	if err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	_ = reason // Failure details are reported through the Edge operation safe code.
	return nil
}

func (manager *ProjectWorktreeTestProcessManager) setProcessState(processID string, state ProjectProcessState) error {
	_, err := manager.processes.db.Exec(`UPDATE worktree_test_processes SET process_state=? WHERE process_id=?`, state, processID)
	if err != nil {
		return ErrProjectWorktreeTestUnavailable
	}
	return nil
}

const projectWorktreeTestSelect = `idempotency_key,process_id,process_key,request_digest,operation_id,project_alias,target_alias,workspace_id,worktree_id,job_id,lease_id,fence,profile_id,profile_digest,timeout_seconds,content_digest,base_commit,head_commit,branch,edge_generation,started_at,timed_out,invalidated_reason`

func scanProjectWorktreeTestRecord(row projectProcessRow) (projectWorktreeTestRecord, error) {
	var record projectWorktreeTestRecord
	var fence int64
	var started int64
	var timedOut bool
	err := row.Scan(&record.IdempotencyKey, &record.ProcessID, &record.ProcessKey, &record.RequestDigest, &record.OperationID,
		&record.Alias, &record.TargetAlias, &record.WorkspaceID, &record.WorktreeID, &record.JobID, &record.LeaseID, &fence,
		&record.ProfileID, &record.ProfileDigest, &record.TimeoutSeconds, &record.ContentDigest, &record.BaseCommit, &record.HeadCommit, &record.Branch,
		&record.EdgeGeneration, &started, &timedOut, &record.InvalidatedReason)
	if errors.Is(err, sql.ErrNoRows) {
		return projectWorktreeTestRecord{}, err
	}
	if err != nil || fence <= 0 || started <= 0 {
		return projectWorktreeTestRecord{}, ErrProjectWorktreeTestUnavailable
	}
	record.Fence, record.StartedAt, record.TimedOut = uint64(fence), time.Unix(0, started).UTC(), timedOut
	return record, nil
}

func validProjectWorktreeTestStartRequest(request ProjectWorktreeTestStartRequest) bool {
	return regexp.MustCompile(`^eo_[a-f0-9]{32}$`).MatchString(request.OperationID) && projectWorktreeTestKeyRE.MatchString(request.IdempotencyKey) &&
		projectWorktreeTestAliasRE.MatchString(request.Alias) && projectWorktreeTestTargetRE.MatchString(request.TargetAlias) &&
		projectWorktreeTestWorktreeIDRE.MatchString(request.WorktreeID) && projectWorktreeTestJobIDRE.MatchString(request.JobID) &&
		projectWorktreeTestLeaseIDRE.MatchString(request.LeaseID) && request.Fence > 0 && request.Fence <= uint64(1<<63-1) &&
		projectWorktreeTestProfileIDRE.MatchString(request.ProfileID) && projectWorktreeTestDigestRE.MatchString(request.ProfileDigest)
}

func validProjectWorktreeTestReadRequest(request ProjectWorktreeTestReadRequest) bool {
	return projectWorktreeTestProcessIDRE.MatchString(request.ProcessID) && projectWorktreeTestAliasRE.MatchString(request.Alias) && projectWorktreeTestTargetRE.MatchString(request.TargetAlias) &&
		projectWorktreeTestWorktreeIDRE.MatchString(request.WorktreeID) && projectWorktreeTestJobIDRE.MatchString(request.JobID) && projectWorktreeTestLeaseIDRE.MatchString(request.LeaseID) &&
		projectWorktreeTestProfileIDRE.MatchString(request.ProfileID) && projectWorktreeTestDigestRE.MatchString(request.ProfileDigest) &&
		projectWorktreeTestFenceValid(request.Fence) && request.StdoutOffset >= 0 && request.StderrOffset >= 0 && request.LimitBytes >= 1 && request.LimitBytes <= edge.MaxProjectProcessReadBytes
}

func validProjectWorktreeTestStopRequest(request ProjectWorktreeTestStopRequest) bool {
	return projectWorktreeTestProcessIDRE.MatchString(request.ProcessID) && projectWorktreeTestAliasRE.MatchString(request.Alias) && projectWorktreeTestTargetRE.MatchString(request.TargetAlias) &&
		projectWorktreeTestWorktreeIDRE.MatchString(request.WorktreeID) && projectWorktreeTestJobIDRE.MatchString(request.JobID) && projectWorktreeTestLeaseIDRE.MatchString(request.LeaseID) &&
		projectWorktreeTestProfileIDRE.MatchString(request.ProfileID) && projectWorktreeTestDigestRE.MatchString(request.ProfileDigest) && projectWorktreeTestFenceValid(request.Fence)
}

func projectWorktreeTestFenceValid(fence uint64) bool { return fence > 0 && fence <= uint64(1<<63-1) }

func projectWorktreeTestRequestDigest(request ProjectWorktreeTestStartRequest, profile ProjectWorktreeTestProfile) string {
	body, _ := json.Marshal(struct {
		Domain, Alias, TargetAlias, WorktreeID, JobID, LeaseID, ProfileID, ProfileDigest string
		Fence                                                                            uint64
	}{projectWorktreeTestRequestDomain, request.Alias, request.TargetAlias, request.WorktreeID, request.JobID, request.LeaseID, profile.ID, profile.Digest, request.Fence})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func projectWorktreeTestProcessKey(idempotencyKey string) string {
	sum := sha256.Sum256([]byte(idempotencyKey))
	return "wttest-" + hex.EncodeToString(sum[:])
}

func projectWorktreeTestSyntheticAlias(worktreeID string) string {
	return "wttest-" + strings.TrimPrefix(worktreeID, "wt_")
}

func projectWorktreeTestOwner(repository string) string {
	owner, _, _ := strings.Cut(repository, "/")
	return owner
}

func projectWorktreeTestRepository(repository string) string {
	_, name, _ := strings.Cut(repository, "/")
	return name
}

func projectWorktreeTestStopRequest(record projectWorktreeTestRecord, processID string) ProjectProcessStopRequest {
	return ProjectProcessStopRequest{ProcessID: processID, ProjectAlias: projectWorktreeTestSyntheticAlias(record.WorktreeID), TargetAlias: record.TargetAlias, WorkspaceID: record.WorkspaceID, GracePeriod: projectWorktreeTestStopGrace}
}

func sameProjectWorktreeTestAuthority(record projectWorktreeTestRecord, worktreeID, jobID, leaseID string, fence uint64) bool {
	return record.WorktreeID == worktreeID && record.JobID == jobID && record.LeaseID == leaseID && record.Fence == fence
}

func projectWorktreeTestSnapshot(record projectWorktreeTestRecord, process ProjectProcessSnapshot, stale bool, staleReason string) ProjectWorktreeTestSnapshot {
	return ProjectWorktreeTestSnapshot{
		ProcessID: record.ProcessID, State: string(process.State), ProfileID: record.ProfileID, ProfileDigest: record.ProfileDigest,
		ContentDigest: record.ContentDigest, BaseCommit: record.BaseCommit, HeadCommit: record.HeadCommit, Branch: record.Branch,
		WorktreeID: record.WorktreeID, WorkspaceID: record.WorkspaceID, JobID: record.JobID, LeaseID: record.LeaseID, Fence: record.Fence,
		Alias: record.Alias, TargetAlias: record.TargetAlias, TimeoutSeconds: record.TimeoutSeconds,
		StartedAt: process.StartedAt, FinishedAt: process.FinishedAt, ExitKnown: process.ExitKnown, ExitCode: process.ExitCode,
		TerminalSignal: string(process.TerminalSignal), Reason: process.Reason,
		Stdout: process.Stdout, Stderr: process.Stderr, StdoutNext: process.StdoutNext, StderrNext: process.StderrNext,
		StdoutEOF: process.StdoutEOF, StderrEOF: process.StderrEOF, StdoutTruncated: process.StdoutTruncated, StderrTruncated: process.StderrTruncated,
		Stale: stale, StaleReason: staleReason, TimedOut: record.TimedOut,
	}
}

type projectWorktreeTestInput struct {
	ProjectWorktreeSnapshot
	ContentDigest string
}

func captureProjectWorktreeTestInput(ctx context.Context, manager *ProjectWorktreeManager, request ProjectWorktreeTestStartRequest) (projectWorktreeTestInput, Workspace, WorkspaceRoots, error) {
	claim := ProjectWorktreeClaimRequest{ID: request.WorktreeID, JobID: request.JobID, LeaseID: request.LeaseID, Fence: request.Fence}
	first, err := manager.Status(ctx, request.WorktreeID)
	if err != nil || !sameProjectWorktreeTestAuthority(projectWorktreeTestRecord{WorktreeID: first.ID, JobID: first.JobID, LeaseID: first.LeaseID, Fence: first.Fence}, request.WorktreeID, request.JobID, request.LeaseID, request.Fence) ||
		first.Alias != request.Alias || first.TargetAlias != request.TargetAlias || first.Role != ProjectWorktreeWriter {
		return projectWorktreeTestInput{}, Workspace{}, WorkspaceRoots{}, ErrProjectWorktreeTestStaleFence
	}
	workspace, err := manager.workspaces.Get(first.WorkspaceID)
	if err != nil || workspace.Path != first.path || workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev {
		return projectWorktreeTestInput{}, Workspace{}, WorkspaceRoots{}, ErrProjectWorktreeUnsafe
	}
	digest, err := manager.ContentDigest(ctx, claim)
	if err != nil {
		return projectWorktreeTestInput{}, Workspace{}, WorkspaceRoots{}, err
	}
	second, err := manager.Status(ctx, request.WorktreeID)
	if err != nil || first.HeadCommit != second.HeadCommit || first.Branch != second.Branch || first.ID != second.ID ||
		first.JobID != second.JobID || first.LeaseID != second.LeaseID || first.Fence != second.Fence {
		return projectWorktreeTestInput{}, Workspace{}, WorkspaceRoots{}, ErrProjectWorktreeUnsafe
	}
	secondDigest, err := manager.ContentDigest(ctx, claim)
	if err != nil || digest != secondDigest {
		return projectWorktreeTestInput{}, Workspace{}, WorkspaceRoots{}, ErrProjectWorktreeUnsafe
	}
	return projectWorktreeTestInput{ProjectWorktreeSnapshot: second, ContentDigest: digest}, workspace, manager.roots, nil
}

func readProjectWorktreeTestProfile(stateRoot string) (ProjectWorktreeTestProfile, error) {
	if runtime.GOOS != "linux" {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	root := filepath.Clean(strings.TrimSpace(stateRoot))
	if !filepath.IsAbs(root) || root == "." || root == string(filepath.Separator) {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	path := filepath.Join(root, projectWorktreeTestProfileFile)
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || pathInfo.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(pathInfo) || pathInfo.Size() < 2 || pathInfo.Size() > projectWorktreeTestProfileMaxBytes {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 || !os.SameFile(pathInfo, opened) {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(file, projectWorktreeTestProfileMaxBytes+1))
	if err != nil || len(body) > projectWorktreeTestProfileMaxBytes {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	var document ProjectWorktreeTestProfileDocument
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(bytes.TrimSuffix(body, []byte("\n")), canonical) {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	if document.Version != 1 || !projectWorktreeTestProfileIDRE.MatchString(document.ProfileID) || len(document.Argv) == 0 || len(document.Argv) > 64 || document.TimeoutSeconds < 1 || document.TimeoutSeconds > projectWorktreeTestProfileMaxSeconds {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	argvBytes := 0
	for index, arg := range document.Argv {
		if arg == "" || len(arg) > projectWorktreeTestMaxArgBytes || strings.ContainsRune(arg, 0) || index == 0 && strings.TrimSpace(arg) == "" {
			return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
		}
		argvBytes += len(arg)
	}
	if argvBytes > projectWorktreeTestMaxArgvBytes {
		return ProjectWorktreeTestProfile{}, ErrProjectWorktreeTestProfileUnavailable
	}
	digestInput := append([]byte(projectWorktreeTestProfileDomain), canonical...)
	sum := sha256.Sum256(digestInput)
	return ProjectWorktreeTestProfile{ID: document.ProfileID, Digest: "sha256:" + hex.EncodeToString(sum[:]), TimeoutSeconds: document.TimeoutSeconds, argv: append([]string(nil), document.Argv...)}, nil
}
