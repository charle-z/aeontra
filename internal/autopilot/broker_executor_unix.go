//go:build !windows

package autopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type BrokerExecutor struct {
	SocketPath  string
	Workspace   string
	WorkspaceID string
}

func (executor BrokerExecutor) Validate(ctx context.Context, workspaceID string) error {
	if workspaceID != executor.WorkspaceID {
		return errors.New("workspace authorization changed")
	}
	_, err := executor.call(ctx, "/v1/status", json.RawMessage(`{"workspace_id":"`+workspaceID+`"}`))
	return err
}

func (executor BrokerExecutor) Execute(ctx context.Context, action LocalAgentResponse) (ActionObservation, error) {
	switch action.Action {
	case ActionFinish:
		return ActionObservation{Completed: true, Progress: true}, nil
	case ActionBlock:
		return ActionObservation{ProviderBlocked: true}, nil
	case ActionCheckpointUpdate:
		var request struct {
			Content string `json:"content"`
		}
		if decodeClosedAction(action.Arguments, &request) != nil || request.Content == "" || len(request.Content) > 1<<20 {
			return ActionObservation{FailureCode: "checkpoint_invalid"}, errors.New("checkpoint invalid")
		}
		path := filepath.Join(executor.Workspace, ".mcp-devbox", "CURRENT.md")
		old, _ := os.ReadFile(path)
		if bytes.Equal(old, []byte(request.Content)) {
			return ActionObservation{}, nil
		}
		if err := writePrivateLocalFile(path, []byte(request.Content)); err != nil {
			return ActionObservation{FailureCode: "checkpoint_failed"}, err
		}
		return ActionObservation{Progress: true, CheckpointChanged: true}, nil
	}
	endpoint := ""
	switch action.Action {
	case ActionStatus, ActionArtifactMetadata:
		endpoint = "/v1/status"
	case ActionAuthValidate:
		endpoint = "/v1/auth-validate"
	case ActionCommand:
		endpoint = "/v1/command"
	case ActionCommandSave:
		endpoint = "/v1/command-save"
	case ActionCommandCredentialStdin:
		endpoint = "/v1/command-credential-stdin"
	case ActionSessionClose:
		endpoint = "/v1/session-close"
	default:
		return ActionObservation{FailureCode: "action_invalid"}, errors.New("action invalid")
	}
	body, err := executor.call(ctx, endpoint, action.Arguments)
	if err != nil {
		return ActionObservation{FailureCode: "action_failed", ModelObservation: body}, err
	}
	progress := action.Action != ActionStatus && action.Action != ActionArtifactMetadata
	return ActionObservation{Progress: progress, ModelObservation: body}, nil
}

func (executor BrokerExecutor) call(ctx context.Context, endpoint string, body json.RawMessage) (json.RawMessage, error) {
	if !filepath.IsAbs(executor.SocketPath) || !json.Valid(body) || len(body) > 64<<10 {
		return nil, errors.New("broker request invalid")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", executor.SocketPath)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 10 * time.Minute}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("local broker unavailable")
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(content) > 4<<20 || !json.Valid(content) {
		return nil, errors.New("local broker response invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return content, errors.New("local broker action rejected")
	}
	return content, nil
}

func decodeClosedAction(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing action data")
	}
	return nil
}
func writePrivateLocalFile(path string, body []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".autopilot-local-")
	if err != nil {
		return err
	}
	staged := temporary.Name()
	defer os.Remove(staged)
	if temporary.Chmod(0o600) != nil {
		return errors.New("local file unsafe")
	}
	if _, err = temporary.Write(body); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("local file unavailable")
	}
	return os.Rename(staged, path)
}
