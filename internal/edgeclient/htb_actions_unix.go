//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (broker *htbLabBroker) status(writer http.ResponseWriter, request *http.Request) {
	var input HTBWorkspaceRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	if err := validateHTBWorkspaceID(input.WorkspaceID, broker.config.Workspace.ID); err != nil {
		writeHTBActionError(writer, http.StatusForbidden, "workspace_not_authorized")
		return
	}
	if !broker.config.ExpiresAt.After(broker.now().UTC()) {
		writeHTBActionError(writer, http.StatusGone, "runtime_expired")
		return
	}
	if err := broker.preflight(request.Context()); err != nil {
		writeHTBActionError(writer, http.StatusPreconditionFailed, "vpn_route_invalid")
		return
	}
	userArtifact, err := inspectHTBArtifact(broker.config.Workspace.Path, "loot/user.txt")
	if err != nil {
		writeHTBActionError(writer, http.StatusBadGateway, "artifact_status_failed")
		return
	}
	rootArtifact, err := inspectHTBArtifact(broker.config.Workspace.Path, "loot/root.txt")
	if err != nil {
		writeHTBActionError(writer, http.StatusBadGateway, "artifact_status_failed")
		return
	}
	handles := broker.activeSessionHandles()
	remoteUser := ""
	if len(handles) > 0 {
		remoteUser = handles[len(handles)-1].Username
	}
	next := "validate_credential"
	switch {
	case rootArtifact.NonEmpty:
		next = "cleanup"
	case userArtifact.NonEmpty:
		next = "enumerate_privileges"
	case len(handles) > 0:
		next = "collect_user_artifact"
	}
	writeHTBActionJSON(writer, http.StatusOK, HTBWorkspaceStatusResponse{
		Status: "ok", WorkspaceID: broker.config.Workspace.ID, Mode: string(broker.config.Workspace.Mode),
		Authorized: true, VPNStatus: "route_valid", BrokerStatus: "active", RemoteUser: remoteUser,
		Handles: handles, UserArtifact: userArtifact, RootArtifact: rootArtifact, NextPhase: next,
	})
}

func (broker *htbLabBroker) authValidate(writer http.ResponseWriter, request *http.Request) {
	var input HTBAuthValidateRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	if err := validateHTBWorkspaceID(input.WorkspaceID, broker.config.Workspace.ID); err != nil {
		writeHTBActionError(writer, http.StatusForbidden, "workspace_not_authorized")
		return
	}
	if !broker.config.ExpiresAt.After(broker.now().UTC()) {
		writeHTBActionError(writer, http.StatusGone, "runtime_expired")
		return
	}
	if broker.attempts.Add(1) > 64 {
		writeHTBActionError(writer, http.StatusTooManyRequests, "attempt_limit")
		return
	}
	reference, err := validateHTBCredentialReference(input.Credential)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "credential_handle_invalid")
		return
	}
	sshRequest, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: input.Username, Source: reference.Source, ExtractAfter: reference.ExtractAfter,
		Command: "id -u && id -g && id -un", TimeoutSeconds: input.TimeoutSeconds,
	})
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := broker.preflight(request.Context()); err != nil {
		writeHTBActionError(writer, http.StatusPreconditionFailed, "vpn_route_invalid")
		return
	}
	credential, err := extractHTBLabCredential(broker.config.Workspace.Path, sshRequest)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "credential_extraction_failed")
		return
	}
	defer zeroHTBBytes(credential)
	response, err := broker.executeSSHWithCredential(request.Context(), sshRequest, credential)
	if err != nil || response.Truncated {
		writeHTBActionError(writer, http.StatusBadGateway, "authentication_failed")
		return
	}
	lines := strings.Split(strings.TrimSpace(response.Stdout), "\n")
	if len(lines) != 3 {
		writeHTBActionError(writer, http.StatusBadGateway, "identity_invalid")
		return
	}
	uid, uidErr := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	gid, gidErr := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	remoteUser := strings.TrimSpace(lines[2])
	if uidErr != nil || gidErr != nil || remoteUser != sshRequest.Username {
		writeHTBActionError(writer, http.StatusBadGateway, "identity_invalid")
		return
	}
	now := broker.now().UTC()
	if !broker.config.ExpiresAt.After(now) {
		writeHTBActionError(writer, http.StatusGone, "runtime_expired")
		return
	}
	sessionID, err := newHTBSessionID()
	if err != nil {
		writeHTBActionError(writer, http.StatusInternalServerError, "session_unavailable")
		return
	}
	session := htbLabSession{
		ID: sessionID, WorkspaceID: broker.config.Workspace.ID, RuntimeID: broker.config.RuntimeID,
		Username: sshRequest.Username, Credential: reference, CreatedAt: now, ExpiresAt: broker.config.ExpiresAt.UTC(),
	}
	broker.mu.Lock()
	broker.removeExpiredSessionsLocked(now)
	broker.sessions[session.ID] = session
	broker.mu.Unlock()
	writeHTBActionJSON(writer, http.StatusOK, HTBAuthValidateResponse{
		Status: "ok", SessionID: session.ID, WorkspaceID: session.WorkspaceID, Username: session.Username,
		UID: uid, GID: gid, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt,
	})
}

func (broker *htbLabBroker) command(writer http.ResponseWriter, request *http.Request) {
	var input HTBSessionCommandRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	validated, err := validateHTBSessionCommand(input, broker.config.Workspace.ID)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	response, credential, err := broker.executeSessionCommand(request.Context(), validated, "", false)
	if credential != nil {
		defer zeroHTBBytes(credential)
	}
	if err != nil {
		writeHTBActionError(writer, http.StatusBadGateway, safeHTBActionFailure(err))
		return
	}
	writeHTBActionJSON(writer, http.StatusOK, HTBCommandResponse{
		Status: "ok", WorkspaceID: validated.WorkspaceID, SessionID: validated.SessionID,
		Stdout: redactHTBCommandOutput([]byte(response.Stdout), credential),
		Stderr: redactHTBCommandOutput([]byte(response.Stderr), credential), ExitCode: response.ExitCode, Truncated: response.Truncated,
	})
}

func (broker *htbLabBroker) commandSave(writer http.ResponseWriter, request *http.Request) {
	var input HTBSessionCommandSaveRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	validated, err := validateHTBSessionCommand(HTBSessionCommandRequest{
		WorkspaceID: input.WorkspaceID, SessionID: input.SessionID, Command: input.Command, TimeoutSeconds: input.TimeoutSeconds,
	}, broker.config.Workspace.ID)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	saveOutput, err := validateHTBSavePath(input.SaveOutput)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "output_path_invalid")
		return
	}
	response, credential, err := broker.executeSessionCommand(request.Context(), validated, saveOutput, false)
	if credential != nil {
		defer zeroHTBBytes(credential)
	}
	if err != nil {
		writeHTBActionError(writer, http.StatusBadGateway, safeHTBActionFailure(err))
		return
	}
	status, err := inspectHTBArtifact(broker.config.Workspace.Path, response.SavedPath)
	if err != nil || !status.Exists {
		writeHTBActionError(writer, http.StatusBadGateway, "output_metadata_failed")
		return
	}
	writeHTBActionJSON(writer, http.StatusOK, HTBCommandSaveResponse{
		Status: "ok", WorkspaceID: validated.WorkspaceID, SessionID: validated.SessionID,
		Path: status.Path, Bytes: status.Bytes, SHA256: status.SHA256, Mode: status.Mode, NonEmpty: status.NonEmpty,
	})
}

func (broker *htbLabBroker) commandCredentialStdin(writer http.ResponseWriter, request *http.Request) {
	var input HTBSessionCommandRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	validated, err := validateHTBSessionCommand(input, broker.config.Workspace.ID)
	if err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	response, credential, err := broker.executeSessionCommand(request.Context(), validated, "", true)
	if credential != nil {
		defer zeroHTBBytes(credential)
	}
	if err != nil {
		writeHTBActionError(writer, http.StatusBadGateway, safeHTBActionFailure(err))
		return
	}
	writeHTBActionJSON(writer, http.StatusOK, HTBCommandResponse{
		Status: "ok", WorkspaceID: validated.WorkspaceID, SessionID: validated.SessionID,
		Stdout: redactHTBCommandOutput([]byte(response.Stdout), credential),
		Stderr: redactHTBCommandOutput([]byte(response.Stderr), credential), ExitCode: response.ExitCode, Truncated: response.Truncated,
	})
}

func (broker *htbLabBroker) sessionClose(writer http.ResponseWriter, request *http.Request) {
	var input HTBSessionCloseRequest
	if !broker.beginHTBAction(writer, request, &input) {
		return
	}
	if err := validateHTBWorkspaceID(input.WorkspaceID, broker.config.Workspace.ID); err != nil || !htbLabSessionPattern.MatchString(input.SessionID) {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	broker.mu.Lock()
	session, exists := broker.sessions[input.SessionID]
	if exists && session.WorkspaceID == input.WorkspaceID && session.RuntimeID == broker.config.RuntimeID {
		delete(broker.sessions, input.SessionID)
	} else {
		exists = false
	}
	broker.mu.Unlock()
	if !exists {
		writeHTBActionError(writer, http.StatusNotFound, "session_not_found")
		return
	}
	writeHTBActionJSON(writer, http.StatusOK, HTBSessionCloseResponse{Status: "ok", WorkspaceID: input.WorkspaceID, SessionID: input.SessionID, Closed: true})
}

func (broker *htbLabBroker) executeSessionCommand(ctx context.Context, input HTBSessionCommandRequest, saveOutput string, credentialStdin bool) (HTBLabSSHResponse, []byte, error) {
	session, err := broker.activeSession(input.WorkspaceID, input.SessionID)
	if err != nil {
		return HTBLabSSHResponse{}, nil, err
	}
	if err := broker.preflight(ctx); err != nil {
		return HTBLabSSHResponse{}, nil, errors.New("vpn_route_invalid")
	}
	sshRequest, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: session.Username, Source: session.Credential.Source, ExtractAfter: session.Credential.ExtractAfter,
		Command: input.Command, SaveOutput: saveOutput, PasswordStdin: credentialStdin, TimeoutSeconds: input.TimeoutSeconds,
	})
	if err != nil {
		return HTBLabSSHResponse{}, nil, err
	}
	credential, err := extractHTBLabCredential(broker.config.Workspace.Path, sshRequest)
	if err != nil {
		return HTBLabSSHResponse{}, nil, errors.New("credential_extraction_failed")
	}
	response, err := broker.executeSSHWithCredential(ctx, sshRequest, credential)
	if err != nil {
		zeroHTBBytes(credential)
		return HTBLabSSHResponse{}, nil, errors.New("ssh_failed")
	}
	return response, credential, nil
}

func (broker *htbLabBroker) activeSession(workspaceID, sessionID string) (htbLabSession, error) {
	now := broker.now().UTC()
	broker.mu.Lock()
	defer broker.mu.Unlock()
	session, exists := broker.sessions[sessionID]
	if !exists || session.WorkspaceID != workspaceID || session.RuntimeID != broker.config.RuntimeID {
		broker.removeExpiredSessionsLocked(now)
		return htbLabSession{}, errors.New("session_not_found")
	}
	if !session.ExpiresAt.After(now) || !broker.config.ExpiresAt.After(now) {
		delete(broker.sessions, sessionID)
		broker.removeExpiredSessionsLocked(now)
		return htbLabSession{}, errors.New("session_expired")
	}
	broker.removeExpiredSessionsLocked(now)
	return session, nil
}

func (broker *htbLabBroker) activeSessionHandles() []HTBSessionHandle {
	now := broker.now().UTC()
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.removeExpiredSessionsLocked(now)
	handles := make([]HTBSessionHandle, 0, len(broker.sessions))
	for _, session := range broker.sessions {
		handles = append(handles, HTBSessionHandle{SessionID: session.ID, Username: session.Username, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt})
	}
	sort.Slice(handles, func(left, right int) bool {
		if handles[left].CreatedAt.Equal(handles[right].CreatedAt) {
			return handles[left].SessionID < handles[right].SessionID
		}
		return handles[left].CreatedAt.Before(handles[right].CreatedAt)
	})
	return handles
}

func (broker *htbLabBroker) removeExpiredSessionsLocked(now time.Time) {
	for id, session := range broker.sessions {
		if !session.ExpiresAt.After(now) || !broker.config.ExpiresAt.After(now) {
			delete(broker.sessions, id)
		}
	}
}

func (broker *htbLabBroker) closeAllSessions() {
	broker.mu.Lock()
	clear(broker.sessions)
	broker.mu.Unlock()
}

func (broker *htbLabBroker) preflight(ctx context.Context) error {
	probe := broker.config.Probe
	if probe == nil {
		probe = systemLinuxNetworkProbe{}
	}
	_, err := preflightHTBLinux(ctx, broker.config.Workspace, probe)
	return err
}

func (broker *htbLabBroker) beginHTBAction(writer http.ResponseWriter, request *http.Request, target any) bool {
	writer.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		writeHTBActionError(writer, http.StatusUnsupportedMediaType, "invalid_content_type")
		return false
	}
	if err := decodeHTBActionRequest(request.Body, target); err != nil {
		writeHTBActionError(writer, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func writeHTBActionJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeHTBActionError(writer http.ResponseWriter, status int, category string) {
	writeHTBActionJSON(writer, status, map[string]any{"status": "error", "failure_category": category})
}

func safeHTBActionFailure(err error) string {
	if err == nil {
		return "none"
	}
	switch err.Error() {
	case "vpn_route_invalid", "credential_extraction_failed", "ssh_failed", "session_not_found", "session_expired":
		return err.Error()
	default:
		return "operation_failed"
	}
}
