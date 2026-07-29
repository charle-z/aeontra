package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type recordingEdgeControlStore struct {
	active      bool
	binding     edge.WorkspaceBinding
	createdKind edge.OperationKind
	request     edge.OperationRequest
	status      edge.OperationResult
	pending     edge.Operation
}

func (s *recordingEdgeControlStore) DeviceActive(string) bool { return s.active }

func (s *recordingEdgeControlStore) ResolveWorkspace(workspaceID string) (edge.WorkspaceBinding, error) {
	if s.binding.WorkspaceID != workspaceID {
		return edge.WorkspaceBinding{}, errors.New("registered active workspace not found")
	}
	return s.binding, nil
}

func (s *recordingEdgeControlStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	s.createdKind = kind
	s.request = request
	s.pending = edge.Operation{ID: "eo_33333333333333333333333333333333", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}
	return s.pending, true, nil
}

func (s *recordingEdgeControlStore) OperationStatus(string) (edge.Operation, error) {
	return s.pending, nil
}

func (s *recordingEdgeControlStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return s.status, nil
}

func (s *recordingEdgeControlStore) WaitOperation(_ context.Context, _ string, _ time.Duration) (edge.Operation, error) {
	s.pending.State = edge.OperationSucceeded
	s.pending.Result = s.status
	return s.pending, nil
}

func TestEdgeControlHandlersQueueClosedOperationsAndReturnPublicState(t *testing.T) {
	store := &recordingEdgeControlStore{
		active: true,
		binding: edge.WorkspaceBinding{
			WorkspaceID: testWorkspaceID, DeviceID: testEdgeDeviceID, Profile: "linux-workcell", Mode: "htb-linux",
		},
		status: edge.OperationResult{
			WorkspaceID: testWorkspaceID, AuthorizationRevision: 7, JobID: "job-safe", JobState: "running",
			ProgressRevision: 9, CycleCount: 3, JobSafeCode: "cycle_complete", Release: "p15.0.0",
			Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ManifestStatus: "valid", ComponentsCompatible: true,
			ServiceActive: true, UpdateAvailable: true, Paired: true, BubblewrapValid: true, RootlessValid: true,
			ServiceState: "active", ProcessState: "single", LockState: "held", Coherence: "managed",
			ProcessRelease: "p15.0.0", ProcessCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			WorkspaceCount: 1, ProviderValid: true, DriverValid: true, Blockers: []string{"none"},
		},
	}
	server := stampServer(t).WithEdgeStore(store)

	assertOperation := func(body string, err error, kind edge.OperationKind) {
		t.Helper()
		if err != nil || store.createdKind != kind || !strings.Contains(body, `"state":"succeeded"`) {
			t.Fatalf("kind=%s created=%s body=%s err=%v", kind, store.createdKind, body, err)
		}
	}

	body, err := server.handleDeviceOperation(json.RawMessage(`{"device_id":"`+testEdgeDeviceID+`","release":"stable"}`), edge.OperationBundleUpdate, true)
	assertOperation(body, err, edge.OperationBundleUpdate)
	if store.request.Release != "stable" {
		t.Fatalf("bundle request=%+v", store.request)
	}
	if !strings.Contains(body, `"process_state":"single"`) || !strings.Contains(body, `"lock_state":"held"`) || !strings.Contains(body, `"coherence":"managed"`) {
		t.Fatalf("authoritative runtime fields missing: %s", body)
	}

	body, err = server.handleLabPrepare(json.RawMessage(`{"device_id":"` + testEdgeDeviceID + `","platform":"htb","machine":"cap","target":"10.10.10.10","difficulty":"easy","operating_system":"linux"}`))
	assertOperation(body, err, edge.OperationLabPrepare)
	if store.request.Machine != "cap" || store.request.Target != "10.10.10.10" {
		t.Fatalf("prepare request=%+v", store.request)
	}

	body, err = server.handleLabRetarget(json.RawMessage(`{"workspace_id":"` + testWorkspaceID + `","target":"10.10.10.11"}`))
	assertOperation(body, err, edge.OperationLabRetarget)
	if store.request.WorkspaceID != testWorkspaceID || store.request.Target != "10.10.10.11" {
		t.Fatalf("retarget request=%+v", store.request)
	}

	body, err = server.handleAutopilotControl(json.RawMessage(`{"workspace_id":"`+testWorkspaceID+`","run_until":"completed_or_cancelled"}`), edge.OperationAutopilotStart)
	assertOperation(body, err, edge.OperationAutopilotStart)
	if store.request.WorkspaceID != testWorkspaceID || store.request.RunUntil != "completed_or_cancelled" {
		t.Fatalf("autopilot request=%+v", store.request)
	}

	body, err = server.handleAutopilotStatus(json.RawMessage(`{"workspace_id":"` + testWorkspaceID + `"}`))
	if err != nil || !strings.Contains(body, `"job_id":"job-safe"`) || !strings.Contains(body, `"cycle_count":3`) || strings.Contains(body, "10.10.10.10") {
		t.Fatalf("autopilot status=%s err=%v", body, err)
	}
}

func TestEdgeControlHandlersRejectUnavailableOrUnauthorizedRequests(t *testing.T) {
	server := stampServer(t)
	if _, err := server.handleDeviceOperation(json.RawMessage(`{}`), edge.OperationBundleStatus, false); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("device store error=%v", err)
	}
	if _, err := server.handleAutopilotControl(json.RawMessage(`{}`), edge.OperationAutopilotStart); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("autopilot store error=%v", err)
	}
	if _, err := server.handleAutopilotStatus(json.RawMessage(`{}`)); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("status store error=%v", err)
	}
	if _, err := server.handleLabPrepare(json.RawMessage(`{}`)); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("prepare store error=%v", err)
	}
	if _, err := server.handleLabRetarget(json.RawMessage(`{}`)); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("retarget store error=%v", err)
	}

	store := &recordingEdgeControlStore{
		active:  true,
		binding: edge.WorkspaceBinding{WorkspaceID: testWorkspaceID, DeviceID: testEdgeDeviceID, Profile: "sandbox", Mode: "dev"},
	}
	server.WithEdgeStore(store)
	if _, err := server.handleDeviceOperation(json.RawMessage(`{"device_id":"`+testEdgeDeviceID+`","release":"stable"}`), edge.OperationBundleStatus, false); err == nil || err.Error() != "release is not accepted" {
		t.Fatalf("release error=%v", err)
	}
	if _, err := server.handleAutopilotControl(json.RawMessage(`{"workspace_id":"`+testWorkspaceID+`"}`), edge.OperationAutopilotPause); err == nil || !strings.Contains(err.Error(), "authorized htb-linux") {
		t.Fatalf("autopilot authorization error=%v", err)
	}
	if _, err := server.handleLabRetarget(json.RawMessage(`{"workspace_id":"` + testWorkspaceID + `","target":"10.10.10.11"}`)); err == nil || !strings.Contains(err.Error(), "authorized htb-linux") {
		t.Fatalf("retarget authorization error=%v", err)
	}
}
