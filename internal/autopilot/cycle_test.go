package autopilot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fixedModel struct {
	response LocalAgentResponse
	err      error
}

func (m fixedModel) NextAction(context.Context, LocalAgentRequest) (LocalAgentResponse, error) {
	return m.response, m.err
}

type fixedExecutor struct {
	observation ActionObservation
	calls       int
}

func (e *fixedExecutor) Execute(context.Context, LocalAgentResponse) (ActionObservation, error) {
	e.calls++
	return e.observation, nil
}

type fixedAuthorization struct{ err error }

func (a fixedAuthorization) Validate(context.Context, string) error { return a.err }

func TestCycleRunnerExecutesOnlyStructuredLocalActionAndPersistsProgress(t *testing.T) {
	workspace := cycleWorkspace(t)
	store := Store{Workspace: workspace}
	_, _, _ = store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
	executor := &fixedExecutor{observation: ActionObservation{Progress: true, CheckpointChanged: true}}
	runner := CycleRunner{Store: store, Model: fixedModel{response: LocalAgentResponse{Action: ActionStatus, Arguments: json.RawMessage(`{"workspace_id":"ws_0123456789abcdef0123456789abcdef"}`)}}, Executor: executor, Authorization: fixedAuthorization{}}
	job, err := runner.Run(context.Background())
	if err != nil || job.ProgressRevision != 1 || job.CheckpointRevision != 1 || executor.calls != 1 {
		t.Fatalf("job=%+v calls=%d err=%v", job, executor.calls, err)
	}
}

func TestCycleRunnerBlocksOnAuthorizationProviderAndSTOP(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(string)
		auth    error
		model   error
		want    string
	}{{name: "authorization", auth: errors.New("changed"), want: "authorization_lost"}, {name: "provider", model: ErrProviderBlocked, want: "provider_blocked"}, {name: "stop", prepare: func(workspace string) {
		_ = os.WriteFile(filepath.Join(workspace, ".mcp-devbox", "STOP"), []byte("stop\n"), 0o600)
	}, want: "stop"}} {
		t.Run(test.name, func(t *testing.T) {
			workspace := cycleWorkspace(t)
			if test.prepare != nil {
				test.prepare(workspace)
			}
			store := Store{Workspace: workspace}
			_, _, _ = store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
			runner := CycleRunner{Store: store, Model: fixedModel{err: test.model}, Executor: &fixedExecutor{}, Authorization: fixedAuthorization{err: test.auth}}
			job, err := runner.Run(context.Background())
			if err != nil || job.State != StateBlocked || job.SafeCode != test.want {
				t.Fatalf("job=%+v err=%v", job, err)
			}
		})
	}
}

func TestCycleRunnerBlocksRepeatedActionWithoutNewEvidence(t *testing.T) {
	workspace := cycleWorkspace(t)
	store := Store{Workspace: workspace}
	_, _, _ = store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
	executor := &fixedExecutor{observation: ActionObservation{Progress: true, ModelObservation: json.RawMessage(`{"status":"same"}`)}}
	runner := CycleRunner{Store: store, Model: fixedModel{response: LocalAgentResponse{Action: ActionStatus, Arguments: json.RawMessage(`{"workspace_id":"ws_0123456789abcdef0123456789abcdef"}`)}}, Executor: executor, Authorization: fixedAuthorization{}}
	first, err := runner.Run(context.Background())
	if err != nil || first.State != StateRunning {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := runner.Run(context.Background())
	if err != nil || second.State != StateBlocked || second.SafeCode != "repeated_action" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func cycleWorkspace(t *testing.T) string {
	workspace := privateWorkspace(t)
	contract := []byte(`{"version":1,"platform":"htb","machine":"Cap","target":"10.129.1.2","lhost":"10.10.1.2","vpn_interface":"tun0","authorization_revision":1}`)
	if err := os.WriteFile(filepath.Join(workspace, ".mcp-devbox", "lab-contract.json"), contract, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}
