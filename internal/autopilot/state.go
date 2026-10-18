package autopilot

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const StateFile = "autopilot-state.json"
const RunUntilCompletedOrCancelled = "completed_or_cancelled"

type JobState string

const (
	StateRunning   JobState = "running"
	StatePaused    JobState = "paused"
	StateBlocked   JobState = "blocked"
	StateCompleted JobState = "completed"
	StateCancelled JobState = "cancelled"
)

var workspacePattern = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)
var safeCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

type State struct {
	Version               int       `json:"version"`
	JobID                 string    `json:"job_id"`
	WorkspaceID           string    `json:"workspace_id"`
	State                 JobState  `json:"state"`
	RunUntil              string    `json:"run_until"`
	ProgressRevision      uint64    `json:"progress_revision"`
	CheckpointRevision    uint64    `json:"checkpoint_revision"`
	CycleCount            uint64    `json:"cycle_count"`
	ConsecutiveNoProgress int       `json:"consecutive_no_progress"`
	RepeatedFailureCount  int       `json:"repeated_failure_count"`
	LastFailureCode       string    `json:"last_failure_code,omitempty"`
	LastActionDigest      string    `json:"last_action_digest,omitempty"`
	RepeatedActionCount   int       `json:"repeated_action_count"`
	SafeCode              string    `json:"safe_code,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	LastProgressAt        time.Time `json:"last_progress_at"`
	NextCycleAt           time.Time `json:"next_cycle_at,omitempty"`
}

type CycleResult struct {
	Progress          bool
	CheckpointChanged bool
	FailureCode       string
	Completed         bool
	ProviderBlocked   bool
	ActionDigest      string
}

type Store struct {
	Workspace string
	Now       func() time.Time
}

func (s Store) Start(workspaceID, runUntil string) (State, bool, error) {
	if !workspacePattern.MatchString(workspaceID) || runUntil != RunUntilCompletedOrCancelled {
		return State{}, false, errors.New("autopilot request is invalid")
	}
	if existing, err := s.Load(); err == nil {
		if existing.WorkspaceID != workspaceID {
			return State{}, false, errors.New("autopilot workspace conflicts with durable job")
		}
		if existing.State == StateRunning || existing.State == StatePaused || existing.State == StateBlocked {
			return existing, false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, false, err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return State{}, false, errors.New("autopilot job generation failed")
	}
	now := s.now()
	job := State{Version: 1, JobID: "aj_" + hex.EncodeToString(idBytes), WorkspaceID: workspaceID, State: StateRunning, RunUntil: runUntil, CreatedAt: now, UpdatedAt: now, LastProgressAt: now}
	if err := s.write(job); err != nil {
		return State{}, false, err
	}
	return job, true, nil
}

func (s Store) Load() (State, error) {
	path, err := s.path()
	if err != nil {
		return State{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return State{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) || info.Size() <= 0 || info.Size() > 64<<10 {
		return State{}, errors.New("autopilot state is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return State{}, errors.New("autopilot state unavailable")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var job State
	if decoder.Decode(&job) != nil || !validState(job) {
		return State{}, errors.New("autopilot state is invalid")
	}
	return job, nil
}

func (s Store) Pause() (State, error) { return s.transition(StatePaused, "paused") }
func (s Store) Resume() (State, error) {
	job, err := s.Load()
	if err != nil {
		return State{}, err
	}
	if job.State != StatePaused && job.State != StateBlocked {
		return State{}, errors.New("autopilot job is not resumable")
	}
	job.State = StateRunning
	job.SafeCode = ""
	job.ConsecutiveNoProgress = 0
	job.RepeatedFailureCount = 0
	job.LastFailureCode = ""
	job.LastActionDigest = ""
	job.RepeatedActionCount = 0
	job.NextCycleAt = time.Time{}
	job.UpdatedAt = s.now()
	return job, s.write(job)
}
func (s Store) Cancel() (State, error) { return s.transition(StateCancelled, "cancelled") }

func (s Store) transition(next JobState, code string) (State, error) {
	job, err := s.Load()
	if err != nil {
		return State{}, err
	}
	if job.State == StateCompleted || job.State == StateCancelled {
		return job, nil
	}
	job.State = next
	job.SafeCode = code
	job.NextCycleAt = time.Time{}
	job.UpdatedAt = s.now()
	return job, s.write(job)
}

func (s Store) RecordCycle(result CycleResult) (State, error) {
	job, err := s.Load()
	if err != nil {
		return State{}, err
	}
	if job.State != StateRunning {
		return State{}, errors.New("autopilot job is not running")
	}
	if result.FailureCode != "" && !safeCodePattern.MatchString(result.FailureCode) {
		return State{}, errors.New("autopilot failure code is invalid")
	}
	job.CycleCount++
	job.UpdatedAt = s.now()
	if result.Completed {
		job.State = StateCompleted
		job.SafeCode = "completed"
		job.NextCycleAt = time.Time{}
	} else if result.ProviderBlocked {
		job.State = StateBlocked
		job.SafeCode = "provider_blocked"
		job.NextCycleAt = time.Time{}
	} else {
		if result.Progress {
			job.ProgressRevision++
			job.ConsecutiveNoProgress = 0
			job.LastProgressAt = job.UpdatedAt
		} else {
			job.ConsecutiveNoProgress++
		}
		if result.CheckpointChanged {
			job.CheckpointRevision++
		}
		if result.FailureCode != "" {
			if job.LastFailureCode == result.FailureCode {
				job.RepeatedFailureCount++
			} else {
				job.LastFailureCode = result.FailureCode
				job.RepeatedFailureCount = 1
			}
			job.NextCycleAt = job.UpdatedAt.Add(retryBackoff(job.RepeatedFailureCount))
		} else {
			job.LastFailureCode = ""
			job.RepeatedFailureCount = 0
			job.NextCycleAt = time.Time{}
		}
		if result.ActionDigest != "" {
			if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(result.ActionDigest) {
				return State{}, errors.New("autopilot action digest is invalid")
			}
			if job.LastActionDigest == result.ActionDigest && !result.Progress && !result.CheckpointChanged {
				job.RepeatedActionCount++
			} else {
				job.LastActionDigest = result.ActionDigest
				job.RepeatedActionCount = 1
			}
		} else {
			job.LastActionDigest = ""
			job.RepeatedActionCount = 0
		}
		if job.ConsecutiveNoProgress >= 2 {
			job.State = StateBlocked
			job.SafeCode = "no_progress"
			job.NextCycleAt = time.Time{}
		}
		if job.RepeatedFailureCount >= 3 {
			job.State = StateBlocked
			job.SafeCode = "repeated_failure"
			job.NextCycleAt = time.Time{}
		}
		if job.RepeatedActionCount >= 2 {
			job.State = StateBlocked
			job.SafeCode = "repeated_action"
			job.NextCycleAt = time.Time{}
		}
	}
	return job, s.write(job)
}

func retryBackoff(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	delay := 2 * time.Second
	for i := 1; i < failures && delay < 2*time.Minute; i++ {
		delay *= 2
	}
	if delay > 2*time.Minute {
		return 2 * time.Minute
	}
	return delay
}

func (s Store) path() (string, error) {
	workspace := filepath.Clean(strings.TrimSpace(s.Workspace))
	if !filepath.IsAbs(workspace) {
		return "", errors.New("autopilot workspace is invalid")
	}
	directory := filepath.Join(workspace, ".mcp-devbox")
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return "", errors.New("autopilot state directory is unsafe")
	}
	return filepath.Join(directory, StateFile), nil
}
func (s Store) write(job State) error {
	if !validState(job) {
		return errors.New("autopilot state is invalid")
	}
	path, err := s.path()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return errors.New("autopilot state encoding failed")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".autopilot-state-")
	if err != nil {
		return errors.New("autopilot state staging failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if temporary.Chmod(0o600) != nil {
		return errors.New("autopilot state staging failed")
	}
	if _, err = temporary.Write(append(body, '\n')); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("autopilot state staging failed")
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return errors.New("autopilot state activation failed")
	}
	return nil
}
func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func validState(job State) bool {
	return job.Version == 1 && strings.HasPrefix(job.JobID, "aj_") && len(job.JobID) == 35 && workspacePattern.MatchString(job.WorkspaceID) && job.RunUntil == RunUntilCompletedOrCancelled && (job.State == StateRunning || job.State == StatePaused || job.State == StateBlocked || job.State == StateCompleted || job.State == StateCancelled) && !job.CreatedAt.IsZero() && !job.UpdatedAt.IsZero() && (job.SafeCode == "" || safeCodePattern.MatchString(job.SafeCode)) && (job.LastFailureCode == "" || safeCodePattern.MatchString(job.LastFailureCode)) && (job.LastActionDigest == "" || regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(job.LastActionDigest))
}
