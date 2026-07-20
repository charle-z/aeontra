package autopilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type ActionKind string

const (
	ActionStatus                 ActionKind = "status"
	ActionAuthValidate           ActionKind = "auth_validate"
	ActionCommand                ActionKind = "command"
	ActionCommandSave            ActionKind = "command_save"
	ActionCommandCredentialStdin ActionKind = "command_with_credential_stdin"
	ActionSessionClose           ActionKind = "session_close"
	ActionArtifactMetadata       ActionKind = "artifact_metadata"
	ActionCheckpointUpdate       ActionKind = "checkpoint_update"
	ActionFinish                 ActionKind = "finish"
	ActionBlock                  ActionKind = "block"
)

type LocalAgentRequest struct {
	WorkspaceID      string          `json:"workspace_id"`
	Contract         json.RawMessage `json:"contract"`
	Checkpoint       string          `json:"checkpoint"`
	LastObservation  json.RawMessage `json:"last_observation,omitempty"`
	ProgressRevision uint64          `json:"progress_revision"`
}
type LocalAgentResponse struct {
	Action    ActionKind      `json:"action"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
type LocalAgentModel interface {
	NextAction(context.Context, LocalAgentRequest) (LocalAgentResponse, error)
}
type LocalActionExecutor interface {
	Execute(context.Context, LocalAgentResponse) (ActionObservation, error)
}
type AuthorizationValidator interface {
	Validate(context.Context, string) error
}
type ActionObservation struct {
	Progress          bool
	CheckpointChanged bool
	Completed         bool
	FailureCode       string
	ProviderBlocked   bool
	ModelObservation  json.RawMessage
}

var ErrProviderBlocked = errors.New("local model provider blocked")

type CycleRunner struct {
	Store         Store
	Model         LocalAgentModel
	Executor      LocalActionExecutor
	Authorization AuthorizationValidator
	ModelTimeout  time.Duration
	ActionTimeout time.Duration
	StorageLimit  int64
}

func (r CycleRunner) Run(ctx context.Context) (State, error) {
	job, err := r.Store.Load()
	if err != nil {
		return State{}, err
	}
	if job.State != StateRunning {
		return job, nil
	}
	if !job.NextCycleAt.IsZero() && r.Store.now().Before(job.NextCycleAt) {
		return job, nil
	}
	if _, err := os.Stat(filepath.Join(r.Store.Workspace, ".mcp-devbox", "STOP")); err == nil {
		return r.Store.transition(StateBlocked, "stop")
	}
	if r.Authorization == nil || r.Model == nil || r.Executor == nil {
		return r.Store.transition(StateBlocked, "configuration_invalid")
	}
	if err := r.Authorization.Validate(ctx, job.WorkspaceID); err != nil {
		return r.Store.transition(StateBlocked, "authorization_lost")
	}
	if r.StorageLimit <= 0 {
		r.StorageLimit = 1 << 30
	}
	if exceeded, err := workspaceStorageExceeds(r.Store.Workspace, r.StorageLimit); err != nil || exceeded {
		return r.Store.transition(StateBlocked, "storage_limit")
	}
	contract, err := os.ReadFile(filepath.Join(r.Store.Workspace, ".mcp-devbox", "lab-contract.json"))
	if err != nil || len(contract) > 64<<10 {
		return r.Store.transition(StateBlocked, "contract_invalid")
	}
	modelTimeout := r.ModelTimeout
	if modelTimeout <= 0 {
		modelTimeout = 2 * time.Minute
	}
	modelCtx, cancel := context.WithTimeout(ctx, modelTimeout)
	response, err := r.Model.NextAction(modelCtx, LocalAgentRequest{WorkspaceID: job.WorkspaceID, Contract: contract, Checkpoint: readBoundedCheckpoint(r.Store.Workspace), LastObservation: readObservation(r.Store.Workspace), ProgressRevision: job.ProgressRevision})
	cancel()
	if errors.Is(err, ErrProviderBlocked) {
		return r.Store.RecordCycle(CycleResult{ProviderBlocked: true})
	}
	if err != nil {
		return r.Store.RecordCycle(CycleResult{FailureCode: "provider_transient"})
	}
	if !validAction(response) {
		return r.Store.RecordCycle(CycleResult{FailureCode: "provider_invalid"})
	}
	actionTimeout := r.ActionTimeout
	if actionTimeout <= 0 {
		actionTimeout = 5 * time.Minute
	}
	actionCtx, actionCancel := context.WithTimeout(ctx, actionTimeout)
	observation, executeErr := r.Executor.Execute(actionCtx, response)
	actionCancel()
	if executeErr != nil && observation.FailureCode == "" {
		observation.FailureCode = "action_failed"
	}
	if observation.ProviderBlocked {
		return r.Store.RecordCycle(CycleResult{ProviderBlocked: true, ActionDigest: actionDigest(response)})
	}
	if len(observation.ModelObservation) > 0 {
		previous := readObservation(r.Store.Workspace)
		if bytes.Equal(bytes.TrimSpace(previous), bytes.TrimSpace(observation.ModelObservation)) {
			observation.Progress = false
		}
		if err := writeObservation(r.Store.Workspace, observation.ModelObservation); err != nil {
			return r.Store.transition(StateBlocked, "observation_invalid")
		}
	}
	return r.Store.RecordCycle(CycleResult{Progress: observation.Progress, CheckpointChanged: observation.CheckpointChanged, Completed: observation.Completed, FailureCode: observation.FailureCode, ActionDigest: actionDigest(response)})
}

func validAction(response LocalAgentResponse) bool {
	switch response.Action {
	case ActionStatus, ActionAuthValidate, ActionCommand, ActionCommandSave, ActionCommandCredentialStdin, ActionSessionClose, ActionArtifactMetadata, ActionCheckpointUpdate:
		return len(response.Arguments) > 0 && json.Valid(response.Arguments)
	case ActionFinish, ActionBlock:
		return len(response.Arguments) == 0 || json.Valid(response.Arguments)
	default:
		return false
	}
}
func actionDigest(response LocalAgentResponse) string {
	body, _ := json.Marshal(response)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func readBoundedCheckpoint(workspace string) string {
	for _, name := range []string{"checkpoint.md", "CURRENT.md"} {
		body, err := os.ReadFile(filepath.Join(workspace, ".mcp-devbox", name))
		if err == nil && len(body) <= 1<<20 {
			return string(body)
		}
	}
	return ""
}
func readObservation(workspace string) json.RawMessage {
	body, err := os.ReadFile(filepath.Join(workspace, ".mcp-devbox", "autopilot-observation.json"))
	if err == nil && len(body) <= 1<<20 && json.Valid(body) {
		return body
	}
	return nil
}
func writeObservation(workspace string, body json.RawMessage) error {
	if len(body) == 0 || len(body) > 1<<20 || !json.Valid(body) {
		return errors.New("autopilot observation is invalid")
	}
	directory := filepath.Join(workspace, ".mcp-devbox")
	temporary, err := os.CreateTemp(directory, ".autopilot-observation-")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if temporary.Chmod(0o600) != nil {
		return errors.New("autopilot observation is unsafe")
	}
	if _, err = temporary.Write(body); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("autopilot observation unavailable")
	}
	return os.Rename(path, filepath.Join(directory, "autopilot-observation.json"))
}
func workspaceStorageExceeds(root string, limit int64) (bool, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			if total > limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return total > limit, err
}
