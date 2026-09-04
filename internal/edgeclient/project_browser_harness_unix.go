//go:build !windows

package edgeclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	projectBrowserHarnessRootRelative = ".mcp-devbox/browser-harness"
	projectBrowserHarnessOutputLimit  = 24 << 10
	projectBrowserHarnessMaxRuns      = 128
	projectBrowserHarnessMaxArtifacts = 100
	projectBrowserHarnessMaxScanItems = 512
	projectBrowserHarnessMaxDepth     = 8
	projectBrowserHarnessMaxFileBytes = 128 << 20
	projectBrowserHarnessMaxReadBytes = 1 << 30
)

var (
	projectBrowserHarnessIDPattern = regexp.MustCompile(`^bh_[a-f0-9]{32}$`)
	projectBrowserProfilePattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)
)

type ProjectBrowserHarnessStartRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	IdempotencyKey            string
	Profile                   string
	Argv                      []string
	CWD                       string
	Environment               map[string]string
	TimeoutSeconds            int
	StorageMiB                int
}

type ProjectBrowserHarnessStatusRequest struct {
	ProjectAlias, TargetAlias  string
	Workspace                  Workspace
	RunID                      string
	StdoutOffset, StderrOffset int64
	Limit                      int
}

type ProjectBrowserHarnessListRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	Limit                     int
}

type ProjectBrowserHarnessStopRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	RunID                     string
	GraceSeconds              int
}

type ProjectBrowserHarnessCleanupRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	RunID                     string
	RemoveProfile             bool
}

type ProjectBrowserHarnessSnapshot struct {
	RunID, State, Profile                       string
	CreatedAt, UpdatedAt, StartedAt, FinishedAt time.Time
	ExitKnown                                   bool
	ExitCode                                    int
	TimeoutSeconds, StorageMiB                  int
	Stdout, Stderr                              string
	StdoutNext, StderrNext                      int64
	StdoutEOF, StderrEOF                        bool
	StdoutTruncated, StderrTruncated            bool
	ArtifactCount                               int
	ArtifactBytes                               int64
}

type ProjectBrowserHarnessSummary struct {
	RunID, State, Profile                       string
	CreatedAt, UpdatedAt, StartedAt, FinishedAt time.Time
	ExitKnown                                   bool
	ExitCode                                    int
	TimeoutSeconds, StorageMiB                  int
}

type ProjectBrowserHarnessCleanupResult struct {
	Runs, Artifacts, Profiles int
}

type projectBrowserHarnessRecord struct {
	RunID          string    `json:"run_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	RequestDigest  string    `json:"request_digest"`
	Profile        string    `json:"profile"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	ExitKnown      bool      `json:"exit_known,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	StorageMiB     int       `json:"storage_mib"`
}

func (manager *ProjectToolboxManager) BrowserHarnessStart(ctx context.Context, request ProjectBrowserHarnessStartRequest) (ProjectBrowserHarnessSnapshot, bool, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectBrowserHarnessStart(request); err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	if _, err := manager.ensureRunning(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	digest := projectBrowserHarnessRequestDigest(request)
	for i := range record.BrowserHarnessRuns {
		run := record.BrowserHarnessRuns[i]
		if run.IdempotencyKey == request.IdempotencyKey {
			if run.RequestDigest != digest {
				return ProjectBrowserHarnessSnapshot{}, false, ErrProjectToolboxUnsafeState
			}
			snapshot, statusErr := manager.browserHarnessStatusLocked(ctx, &record, i, request.Workspace, 0, 0, projectBrowserHarnessOutputLimit)
			return snapshot, true, statusErr
		}
	}
	if len(record.BrowserHarnessRuns) >= projectBrowserHarnessMaxRuns {
		return ProjectBrowserHarnessSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	runID, err := manager.newHarnessID()
	if err != nil || !projectBrowserHarnessIDPattern.MatchString(runID) {
		return ProjectBrowserHarnessSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	paths, err := prepareProjectBrowserHarnessPaths(request.Workspace, runID, request.Profile)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	now := manager.now().UTC()
	run := projectBrowserHarnessRecord{RunID: runID, IdempotencyKey: request.IdempotencyKey, RequestDigest: digest, Profile: request.Profile, State: "starting", CreatedAt: now, UpdatedAt: now, TimeoutSeconds: request.TimeoutSeconds, StorageMiB: request.StorageMiB}
	record.BrowserHarnessRuns = append(record.BrowserHarnessRuns, run)
	record.UpdatedAt = now
	if err := manager.save(record); err != nil {
		_ = removeProjectBrowserHarnessRunRoot(request.Workspace, runID)
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	cwd, err := normalizeProjectToolboxCWD(request.CWD)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, false, err
	}
	args := []string{"exec", "--detach", "--workdir", cwd}
	for _, item := range projectBrowserHarnessEnvironment(runID, request.Profile) {
		args = append(args, "--env", item)
	}
	keys := make([]string, 0, len(request.Environment))
	for key := range request.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := request.Environment[key]
		if !projectToolboxEnvKeyPattern.MatchString(key) || projectToolboxReservedEnvironmentKey(key) || projectToolboxSecretShaped(value) {
			return ProjectBrowserHarnessSnapshot{}, false, ErrProjectToolboxUnsafeState
		}
		args = append(args, "--env", key+"="+value)
	}
	args = append(args, record.ContainerName, "/bin/sh", "-c", projectBrowserHarnessStartScript, "mcp-browser-harness-start", paths.ContainerRunRoot, paths.ContainerProfileRoot, strconv.Itoa(request.TimeoutSeconds), strconv.FormatInt(int64(request.StorageMiB)<<20, 10), runID)
	args = append(args, request.Argv...)
	if _, err := manager.run(ctx, args...); err != nil {
		record.BrowserHarnessRuns = record.BrowserHarnessRuns[:len(record.BrowserHarnessRuns)-1]
		record.UpdatedAt = manager.now().UTC()
		_ = manager.save(record)
		_ = removeProjectBrowserHarnessRunRoot(request.Workspace, runID)
		return ProjectBrowserHarnessSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	index := len(record.BrowserHarnessRuns) - 1
	snapshot, err := manager.browserHarnessStatusLocked(ctx, &record, index, request.Workspace, 0, 0, projectBrowserHarnessOutputLimit)
	return snapshot, false, err
}

func (manager *ProjectToolboxManager) BrowserHarnessStatus(ctx context.Context, request ProjectBrowserHarnessStatusRequest) (ProjectBrowserHarnessSnapshot, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if request.Limit < 1 || request.Limit > projectBrowserHarnessOutputLimit || request.StdoutOffset < 0 || request.StderrOffset < 0 {
		return ProjectBrowserHarnessSnapshot{}, ErrProjectToolboxUnsafeState
	}
	record, index, err := manager.browserHarnessRecord(request.ProjectAlias, request.TargetAlias, request.Workspace, request.RunID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	return manager.browserHarnessStatusLocked(ctx, &record, index, request.Workspace, request.StdoutOffset, request.StderrOffset, request.Limit)
}

func (manager *ProjectToolboxManager) BrowserHarnessList(ctx context.Context, request ProjectBrowserHarnessListRequest) ([]ProjectBrowserHarnessSummary, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return nil, err
	}
	if request.Limit < 1 || request.Limit > 50 {
		return nil, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return nil, err
	}
	if err := manager.verifyOwnership(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return nil, err
	}
	start := len(record.BrowserHarnessRuns) - request.Limit
	if start < 0 {
		start = 0
	}
	out := make([]ProjectBrowserHarnessSummary, 0, len(record.BrowserHarnessRuns)-start)
	for i := len(record.BrowserHarnessRuns) - 1; i >= start; i-- {
		out = append(out, browserHarnessSummary(record.BrowserHarnessRuns[i]))
	}
	return out, nil
}

func (manager *ProjectToolboxManager) BrowserHarnessStop(ctx context.Context, request ProjectBrowserHarnessStopRequest) (ProjectBrowserHarnessSnapshot, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if request.GraceSeconds < 1 || request.GraceSeconds > 30 {
		return ProjectBrowserHarnessSnapshot{}, ErrProjectToolboxUnsafeState
	}
	record, index, err := manager.browserHarnessRecord(request.ProjectAlias, request.TargetAlias, request.Workspace, request.RunID)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	if browserHarnessTerminal(record.BrowserHarnessRuns[index].State) {
		return manager.browserHarnessStatusLocked(ctx, &record, index, request.Workspace, 0, 0, projectBrowserHarnessOutputLimit)
	}
	toolbox, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	if toolbox.State != ProjectToolboxRunning {
		record.BrowserHarnessRuns[index].State = "indeterminate"
		record.BrowserHarnessRuns[index].UpdatedAt = manager.now().UTC()
		_ = manager.save(record)
		return browserHarnessSnapshot(record.BrowserHarnessRuns[index]), nil
	}
	paths := projectBrowserHarnessPathsFor(request.Workspace, request.RunID, record.BrowserHarnessRuns[index].Profile)
	output, runErr := manager.run(ctx, "exec", record.ContainerName, "/bin/sh", "-c", projectBrowserHarnessStopScript, "mcp-browser-harness-stop", paths.ContainerRunRoot, strconv.Itoa(request.GraceSeconds))
	state := strings.TrimSpace(string(output))
	if runErr != nil || state != "stopped" {
		return ProjectBrowserHarnessSnapshot{}, ErrProjectToolboxUnavailable
	}
	run := &record.BrowserHarnessRuns[index]
	run.State, run.UpdatedAt, run.FinishedAt = "stopped", manager.now().UTC(), manager.now().UTC()
	if err := manager.save(record); err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	return manager.browserHarnessStatusLocked(ctx, &record, index, request.Workspace, 0, 0, projectBrowserHarnessOutputLimit)
}

func (manager *ProjectToolboxManager) BrowserHarnessCleanup(request ProjectBrowserHarnessCleanupRequest) (ProjectBrowserHarnessCleanupResult, error) {
	release, err := manager.acquireWorkspaceLock(context.Background(), request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessCleanupResult{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectBrowserHarnessCleanupResult{}, err
	}
	if request.RunID != "" && !projectBrowserHarnessIDPattern.MatchString(request.RunID) {
		return ProjectBrowserHarnessCleanupResult{}, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectBrowserHarnessCleanupResult{}, err
	}
	selected := map[string]bool{}
	retainedProfiles := map[string]bool{}
	for _, run := range record.BrowserHarnessRuns {
		remove := (request.RunID == "" || request.RunID == run.RunID) && browserHarnessTerminal(run.State)
		if remove {
			selected[run.RunID] = true
		} else {
			retainedProfiles[run.Profile] = true
		}
	}
	if request.RunID != "" && !selected[request.RunID] {
		return ProjectBrowserHarnessCleanupResult{}, ErrProjectToolboxUnsafeState
	}
	result := ProjectBrowserHarnessCleanupResult{}
	kept := make([]projectBrowserHarnessRecord, 0, len(record.BrowserHarnessRuns))
	removedProfiles := map[string]bool{}
	for _, run := range record.BrowserHarnessRuns {
		if !selected[run.RunID] {
			kept = append(kept, run)
			continue
		}
		count, _, scanErr := scanProjectBrowserHarnessArtifacts(request.Workspace, run.RunID, projectBrowserHarnessMaxArtifacts)
		if scanErr != nil {
			return result, scanErr
		}
		if err := removeProjectBrowserHarnessRunRoot(request.Workspace, run.RunID); err != nil {
			return result, err
		}
		result.Runs++
		result.Artifacts += count
		if request.RemoveProfile && !retainedProfiles[run.Profile] && !removedProfiles[run.Profile] {
			if err := removeProjectBrowserHarnessProfileRoot(request.Workspace, run.Profile); err != nil {
				return result, err
			}
			removedProfiles[run.Profile] = true
			result.Profiles++
		}
	}
	record.BrowserHarnessRuns = kept
	record.UpdatedAt = manager.now().UTC()
	if err := manager.save(record); err != nil {
		return result, err
	}
	return result, nil
}

func (manager *ProjectToolboxManager) browserHarnessStatusLocked(ctx context.Context, record *projectToolboxRecord, index int, workspace Workspace, stdoutOffset, stderrOffset int64, limit int) (ProjectBrowserHarnessSnapshot, error) {
	if record == nil || index < 0 || index >= len(record.BrowserHarnessRuns) {
		return ProjectBrowserHarnessSnapshot{}, ErrProjectToolboxUnsafeState
	}
	run := &record.BrowserHarnessRuns[index]
	paths := projectBrowserHarnessPathsFor(workspace, run.RunID, run.Profile)
	toolbox, err := manager.status(ctx, *record, record.ProjectAlias, record.TargetAlias, workspace)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	if toolbox.State == ProjectToolboxRunning && !browserHarnessTerminal(run.State) {
		output, statusErr := manager.run(ctx, "exec", record.ContainerName, "/bin/sh", "-c", projectBrowserHarnessStatusScript, "mcp-browser-harness-status", paths.ContainerRunRoot)
		state := strings.TrimSpace(string(output))
		if statusErr != nil || !browserHarnessStateValid(state) {
			return ProjectBrowserHarnessSnapshot{}, ErrProjectToolboxUnavailable
		}
		run.State = state
	} else if toolbox.State != ProjectToolboxRunning && !browserHarnessTerminal(run.State) {
		run.State = "indeterminate"
	}
	applyProjectBrowserHarnessControlFiles(paths.HostRunRoot, run)
	run.UpdatedAt = manager.now().UTC()
	stdout, stdoutNext, stdoutEOF, stdoutTruncated, err := readProjectBrowserHarnessLog(paths.HostRunRoot, "stdout.log", stdoutOffset, limit)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	stderr, stderrNext, stderrEOF, stderrTruncated, err := readProjectBrowserHarnessLog(paths.HostRunRoot, "stderr.log", stderrOffset, limit)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	count, bytes, err := scanProjectBrowserHarnessArtifacts(workspace, run.RunID, projectBrowserHarnessMaxArtifacts)
	if err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	record.UpdatedAt = run.UpdatedAt
	if err := manager.save(*record); err != nil {
		return ProjectBrowserHarnessSnapshot{}, err
	}
	snapshot := browserHarnessSnapshot(*run)
	snapshot.Stdout, snapshot.Stderr = stdout, stderr
	snapshot.StdoutNext, snapshot.StderrNext = stdoutNext, stderrNext
	snapshot.StdoutEOF, snapshot.StderrEOF = stdoutEOF, stderrEOF
	snapshot.StdoutTruncated, snapshot.StderrTruncated = stdoutTruncated, stderrTruncated
	snapshot.ArtifactCount, snapshot.ArtifactBytes = count, bytes
	return snapshot, nil
}

func (manager *ProjectToolboxManager) browserHarnessRecord(alias, target string, workspace Workspace, runID string) (projectToolboxRecord, int, error) {
	if err := validateProjectToolboxBinding(alias, target, workspace); err != nil || !projectBrowserHarnessIDPattern.MatchString(runID) {
		return projectToolboxRecord{}, -1, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(workspace.ID)
	if err != nil {
		return projectToolboxRecord{}, -1, err
	}
	if record.ProjectAlias != alias || record.TargetAlias != target {
		return projectToolboxRecord{}, -1, ErrProjectToolboxNotOwned
	}
	for i := range record.BrowserHarnessRuns {
		if record.BrowserHarnessRuns[i].RunID == runID {
			return record, i, nil
		}
	}
	return projectToolboxRecord{}, -1, ErrProjectToolboxNotFound
}

func validateProjectBrowserHarnessStart(request ProjectBrowserHarnessStartRequest) error {
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return err
	}
	if !projectOperationIdempotencyPatternPortable(request.IdempotencyKey) || !projectBrowserProfilePattern.MatchString(request.Profile) || len(request.Argv) < 1 || len(request.Argv) > 128 || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 604800 || request.StorageMiB < 64 || request.StorageMiB > 65536 {
		return ErrProjectToolboxUnsafeState
	}
	if _, err := normalizeProjectToolboxCWD(request.CWD); err != nil {
		return err
	}
	for _, argument := range request.Argv {
		if argument == "" || len(argument) > 8192 || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || projectToolboxSecretShaped(argument) {
			return ErrProjectToolboxUnsafeState
		}
	}
	for key, value := range request.Environment {
		if !projectToolboxEnvKeyPattern.MatchString(key) || projectToolboxReservedEnvironmentKey(key) || len(value) > 4096 || projectToolboxSecretShaped(value) {
			return ErrProjectToolboxUnsafeState
		}
	}
	return nil
}

func projectOperationIdempotencyPatternPortable(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == ':' || r == '-') || i == 0 && !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func projectBrowserHarnessRequestDigest(request ProjectBrowserHarnessStartRequest) string {
	body, _ := json.Marshal(struct {
		Profile                    string
		Argv                       []string
		CWD                        string
		Environment                map[string]string
		TimeoutSeconds, StorageMiB int
	}{request.Profile, request.Argv, request.CWD, request.Environment, request.TimeoutSeconds, request.StorageMiB})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func projectBrowserHarnessEnvironment(runID, profile string) []string {
	base := "/workspace/" + projectBrowserHarnessRootRelative
	return []string{
		"MCP_BROWSER_RUN_ID=" + runID,
		"MCP_BROWSER_RUN_DIR=" + base + "/runs/" + runID,
		"MCP_BROWSER_ARTIFACTS_DIR=" + base + "/runs/" + runID + "/artifacts",
		"MCP_BROWSER_DOWNLOADS_DIR=" + base + "/runs/" + runID + "/downloads",
		"MCP_BROWSER_PROFILE_DIR=" + base + "/profiles/" + profile,
		"PLAYWRIGHT_BROWSERS_PATH=/var/lib/mcp-devbox/browser-browsers",
		"PUPPETEER_CACHE_DIR=/var/lib/mcp-devbox/browser-browsers/puppeteer",
		"SELENIUM_MANAGER_CACHE=/var/lib/mcp-devbox/browser-browsers/selenium",
	}
}

func validProjectBrowserHarnessRecords(runs []projectBrowserHarnessRecord) bool {
	if len(runs) > projectBrowserHarnessMaxRuns {
		return false
	}
	seenID, seenKey := map[string]bool{}, map[string]bool{}
	for _, run := range runs {
		if !projectBrowserHarnessIDPattern.MatchString(run.RunID) || !projectOperationIdempotencyPatternPortable(run.IdempotencyKey) || len(run.RequestDigest) != 64 || !projectBrowserProfilePattern.MatchString(run.Profile) || !browserHarnessStateValid(run.State) || run.CreatedAt.IsZero() || run.UpdatedAt.Before(run.CreatedAt) || run.TimeoutSeconds < 1 || run.TimeoutSeconds > 604800 || run.StorageMiB < 64 || run.StorageMiB > 65536 || seenID[run.RunID] || seenKey[run.IdempotencyKey] {
			return false
		}
		seenID[run.RunID], seenKey[run.IdempotencyKey] = true, true
	}
	return true
}

func browserHarnessStateValid(state string) bool {
	switch state {
	case "starting", "running", "exited", "failed", "stopped", "timed_out", "storage_exceeded", "indeterminate":
		return true
	}
	return false
}
func browserHarnessTerminal(state string) bool {
	return state == "exited" || state == "failed" || state == "stopped" || state == "timed_out" || state == "storage_exceeded" || state == "indeterminate"
}
func browserHarnessSnapshot(run projectBrowserHarnessRecord) ProjectBrowserHarnessSnapshot {
	return ProjectBrowserHarnessSnapshot{RunID: run.RunID, State: run.State, Profile: run.Profile, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ExitKnown: run.ExitKnown, ExitCode: run.ExitCode, TimeoutSeconds: run.TimeoutSeconds, StorageMiB: run.StorageMiB}
}
func browserHarnessSummary(run projectBrowserHarnessRecord) ProjectBrowserHarnessSummary {
	return ProjectBrowserHarnessSummary{RunID: run.RunID, State: run.State, Profile: run.Profile, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ExitKnown: run.ExitKnown, ExitCode: run.ExitCode, TimeoutSeconds: run.TimeoutSeconds, StorageMiB: run.StorageMiB}
}

func newProjectBrowserHarnessID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate browser harness id: %w", err)
	}
	return "bh_" + hex.EncodeToString(buffer), nil
}

func applyProjectBrowserHarnessControlFiles(root string, run *projectBrowserHarnessRecord) {
	if run == nil {
		return
	}
	if value, err := readBrowserHarnessSmallFile(root, "state", 64); err == nil && browserHarnessStateValid(value) {
		run.State = value
	}
	if value, err := readBrowserHarnessSmallFile(root, "started_at", 32); err == nil {
		if unix, e := strconv.ParseInt(value, 10, 64); e == nil && unix > 0 {
			run.StartedAt = time.Unix(unix, 0).UTC()
		}
	}
	if value, err := readBrowserHarnessSmallFile(root, "finished_at", 32); err == nil {
		if unix, e := strconv.ParseInt(value, 10, 64); e == nil && unix > 0 {
			run.FinishedAt = time.Unix(unix, 0).UTC()
		}
	}
	if value, err := readBrowserHarnessSmallFile(root, "exit_code", 16); err == nil {
		if code, e := strconv.Atoi(value); e == nil && code >= 0 && code <= 255 {
			run.ExitKnown, run.ExitCode = true, code
		}
	}
}

func readBrowserHarnessSmallFile(root, name string, limit int64) (string, error) {
	path := filepath.Join(root, name)
	file, info, err := openStableOwnedRegularUnder(root, path)
	if err != nil {
		return "", ErrProjectToolboxUnsafeState
	}
	defer file.Close()
	if info.Size() < 1 || info.Size() > limit {
		return "", ErrProjectToolboxUnsafeState
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) != info.Size() {
		return "", ErrProjectToolboxUnsafeState
	}
	value := strings.TrimSpace(string(body))
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", ErrProjectToolboxUnsafeState
	}
	return value, nil
}

func readProjectBrowserHarnessLog(root, name string, offset int64, limit int) (string, int64, bool, bool, error) {
	path := filepath.Join(root, name)
	file, info, err := openStableOwnedRegularUnder(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return "", offset, true, false, nil
	}
	if err != nil {
		return "", 0, false, false, ErrProjectToolboxUnsafeState
	}
	defer file.Close()
	if offset > info.Size() {
		return "", 0, false, false, ErrProjectToolboxUnsafeState
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return "", 0, false, false, ErrProjectToolboxUnsafeState
	}
	remaining := info.Size() - offset
	if remaining == 0 {
		return "", offset, true, false, nil
	}
	size := int64(limit)
	truncated := remaining > size
	if remaining < size {
		size = remaining
	}
	buffer := make([]byte, size)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return "", 0, false, false, ErrProjectToolboxUnsafeState
	}
	buffer = buffer[:n]
	text, _ := policy.Redact(string(buffer))
	next := offset + int64(n)
	return text, next, next == info.Size(), truncated, nil
}

const projectBrowserHarnessStartScript = `root=$1; profile=$2; timeout=$3; storage=$4; run=$5; shift 5; umask 077; mkdir -p "$root/artifacts" "$root/downloads" "$profile" /var/lib/mcp-devbox/browser-browsers; : > "$root/stdout.log"; : > "$root/stderr.log"; rm -f "$root/stop_reason" "$root/exit_code" "$root/finished_at"; date +%s > "$root/started_at"; printf 'starting\n' > "$root/state"; if command -v setsid >/dev/null 2>&1; then setsid "$@" >>"$root/stdout.log" 2>>"$root/stderr.log" & else "$@" >>"$root/stdout.log" 2>>"$root/stderr.log" & fi; pid=$!; ticks=$(awk '{print $22}' "/proc/$pid/stat" 2>/dev/null || true); printf '%s %s\n' "$pid" "$ticks" > "$root/identity"; printf 'running\n' > "$root/state"; started=$(date +%s); terminate(){ kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true; i=0; while kill -0 "$pid" 2>/dev/null && test "$i" -lt 100; do sleep 0.1; i=$((i+1)); done; kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true; }; while kill -0 "$pid" 2>/dev/null; do now=$(date +%s); if test $((now-started)) -ge "$timeout"; then printf 'timed_out\n' > "$root/stop_reason"; terminate; break; fi; bytes=$(du -sb "$root" "$profile" 2>/dev/null | awk '{sum+=$1} END {print sum+0}'); case "$bytes" in ''|*[!0-9]*) bytes=0;; esac; if test "$bytes" -gt "$storage"; then printf 'storage_exceeded\n' > "$root/stop_reason"; terminate; break; fi; sleep 1; done; wait "$pid" 2>/dev/null; code=$?; printf '%s\n' "$code" > "$root/exit_code"; state=exited; test "$code" -eq 0 || state=failed; if test -s "$root/stop_reason"; then state=$(cat "$root/stop_reason"); fi; printf '%s\n' "$state" > "$root/state"; date +%s > "$root/finished_at"`
const projectBrowserHarnessStatusScript = `root=$1; state=$(cat "$root/state" 2>/dev/null || printf 'indeterminate'); case "$state" in exited|failed|stopped|timed_out|storage_exceeded|indeterminate) printf '%s\n' "$state"; exit 0;; starting) test -r "$root/identity" || { printf 'starting\n'; exit 0; };; esac; test -r "$root/identity" || { printf 'indeterminate\n'; exit 0; }; read pid ticks < "$root/identity"; case "$pid:$ticks" in *[!0-9:]*|:*|*:) printf 'indeterminate\n'; exit 0;; esac; test -r "/proc/$pid/stat" || { printf 'indeterminate\n'; exit 0; }; current=$(awk '{print $22}' "/proc/$pid/stat"); if test "$current" = "$ticks"; then printf 'running\n'; else printf 'indeterminate\n'; fi`
const projectBrowserHarnessStopScript = `root=$1; grace=$2; state=$(cat "$root/state" 2>/dev/null || printf 'indeterminate'); case "$state" in exited|failed|stopped|timed_out|storage_exceeded|indeterminate) printf '%s\n' "$state"; exit 0;; esac; test -r "$root/identity" || { printf 'stopped\n'; exit 0; }; read pid ticks < "$root/identity"; case "$pid:$ticks" in *[!0-9:]*|:*|*:) printf 'stopped\n'; exit 0;; esac; test -r "/proc/$pid/stat" || { printf 'stopped\n'; exit 0; }; current=$(awk '{print $22}' "/proc/$pid/stat"); test "$current" = "$ticks" || { printf 'stopped\n'; exit 0; }; printf 'stopped\n' > "$root/stop_reason"; kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true; i=0; limit=$((grace*10)); while kill -0 "$pid" 2>/dev/null && test "$i" -lt "$limit"; do sleep 0.1; i=$((i+1)); done; kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true; printf 'stopped\n' > "$root/state"; date +%s > "$root/finished_at"; printf 'stopped\n'`
