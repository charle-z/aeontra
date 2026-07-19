package edgeclient

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	HTBLabStatusURL                 = "http://unix/v1/status"
	HTBLabAuthValidateURL           = "http://unix/v1/auth-validate"
	HTBLabCommandURL                = "http://unix/v1/command"
	HTBLabCommandSaveURL            = "http://unix/v1/command-save"
	HTBLabCommandCredentialStdinURL = "http://unix/v1/command-credential-stdin"
	HTBLabSessionCloseURL           = "http://unix/v1/session-close"
)

var (
	htbLabSessionPattern = regexp.MustCompile(`^hs_[a-f0-9]{32}$`)
	htbLabFlagPattern    = regexp.MustCompile(`(?i)\b[a-f0-9]{32}\b`)
)

type HTBCredentialReference struct {
	Source       string `json:"source"`
	ExtractAfter string `json:"extract_after"`
}

type HTBWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type HTBAuthValidateRequest struct {
	WorkspaceID    string                 `json:"workspace_id"`
	Username       string                 `json:"username"`
	Credential     HTBCredentialReference `json:"credential"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
}

type HTBSessionCommandRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	SessionID      string `json:"session_id"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type HTBSessionCommandSaveRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	SessionID      string `json:"session_id"`
	Command        string `json:"command"`
	SaveOutput     string `json:"save_output"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type HTBSessionCloseRequest struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}

type HTBArtifactStatus struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	NonEmpty bool   `json:"non_empty"`
	Bytes    int64  `json:"bytes,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type HTBSessionHandle struct {
	SessionID string    `json:"session_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type HTBWorkspaceStatusResponse struct {
	Status          string             `json:"status"`
	WorkspaceID     string             `json:"workspace_id"`
	Mode            string             `json:"mode"`
	Authorized      bool               `json:"authorized"`
	VPNStatus       string             `json:"vpn_status"`
	BrokerStatus    string             `json:"broker_status"`
	RemoteUser      string             `json:"remote_user,omitempty"`
	Handles         []HTBSessionHandle `json:"handles"`
	UserArtifact    HTBArtifactStatus  `json:"user_txt"`
	RootArtifact    HTBArtifactStatus  `json:"root_txt"`
	NextPhase       string             `json:"next_phase"`
	FailureCategory string             `json:"failure_category,omitempty"`
}

type HTBAuthValidateResponse struct {
	Status          string    `json:"status"`
	SessionID       string    `json:"session_id"`
	WorkspaceID     string    `json:"workspace_id"`
	Username        string    `json:"username"`
	UID             int64     `json:"uid"`
	GID             int64     `json:"gid"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	FailureCategory string    `json:"failure_category,omitempty"`
}

type HTBCommandResponse struct {
	Status          string `json:"status"`
	WorkspaceID     string `json:"workspace_id"`
	SessionID       string `json:"session_id"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	ExitCode        int    `json:"exit_code"`
	Truncated       bool   `json:"truncated"`
	FailureCategory string `json:"failure_category,omitempty"`
}

type HTBCommandSaveResponse struct {
	Status          string `json:"status"`
	WorkspaceID     string `json:"workspace_id"`
	SessionID       string `json:"session_id"`
	Path            string `json:"path"`
	Bytes           int64  `json:"bytes"`
	SHA256          string `json:"sha256"`
	Mode            string `json:"mode"`
	NonEmpty        bool   `json:"non_empty"`
	FailureCategory string `json:"failure_category,omitempty"`
}

type HTBSessionCloseResponse struct {
	Status      string `json:"status"`
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
	Closed      bool   `json:"closed"`
}

type htbLabSession struct {
	ID          string
	WorkspaceID string
	RuntimeID   string
	Username    string
	Credential  HTBCredentialReference
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

func decodeHTBActionRequest(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, htbLabBrokerRequestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("HTB action request is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("HTB action request has trailing data")
	}
	return nil
}

func validateHTBWorkspaceID(got, expected string) error {
	if !workspaceIDPattern.MatchString(got) || got != expected {
		return errors.New("HTB workspace is not authorized for this runtime")
	}
	return nil
}

func validateHTBCredentialReference(reference HTBCredentialReference) (HTBCredentialReference, error) {
	reference.Source = filepath.Clean(strings.TrimSpace(reference.Source))
	reference.ExtractAfter = strings.Trim(reference.ExtractAfter, "\r\n")
	request := HTBLabSSHRequest{Username: "placeholder", Source: reference.Source, ExtractAfter: reference.ExtractAfter, Command: "id", TimeoutSeconds: 120}
	validated, err := validateHTBLabSSHRequest(request)
	if err != nil {
		return HTBCredentialReference{}, err
	}
	reference.Source = validated.Source
	reference.ExtractAfter = validated.ExtractAfter
	return reference, nil
}

func validateHTBSessionCommand(request HTBSessionCommandRequest, workspaceID string) (HTBSessionCommandRequest, error) {
	if err := validateHTBWorkspaceID(request.WorkspaceID, workspaceID); err != nil {
		return HTBSessionCommandRequest{}, err
	}
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Command = strings.TrimSpace(request.Command)
	if !htbLabSessionPattern.MatchString(request.SessionID) {
		return HTBSessionCommandRequest{}, errors.New("HTB session id is invalid")
	}
	validated, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: "placeholder", Source: "loot/placeholder", ExtractAfter: "PASS", Command: request.Command, TimeoutSeconds: request.TimeoutSeconds,
	})
	if err != nil {
		return HTBSessionCommandRequest{}, err
	}
	request.Command = validated.Command
	request.TimeoutSeconds = validated.TimeoutSeconds
	return request, nil
}

func validateHTBSavePath(path string) (string, error) {
	validated, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: "placeholder", Source: "loot/placeholder", ExtractAfter: "PASS", Command: "id", SaveOutput: path, TimeoutSeconds: 120,
	})
	if err != nil || validated.SaveOutput == "" {
		return "", errors.New("HTB output path is invalid")
	}
	return validated.SaveOutput, nil
}

func newHTBSessionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("HTB session id generation failed")
	}
	return "hs_" + hex.EncodeToString(value), nil
}

func redactHTBCommandOutput(value []byte, credential []byte) string {
	copyValue := append([]byte(nil), value...)
	defer zeroHTBBytes(copyValue)
	if len(credential) > 0 {
		copyValue = bytes.ReplaceAll(copyValue, credential, []byte("[REDACTED-CREDENTIAL]"))
	}
	return htbLabFlagPattern.ReplaceAllString(string(copyValue), "[REDACTED-HTB-FLAG]")
}
