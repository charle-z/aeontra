package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectGitSyncToolStore struct {
	resolveErr     error
	createErr      error
	waitErr        error
	reused         bool
	resolvedTarget string
	createdKind    edge.OperationKind
	createdRequest edge.OperationRequest
	waitResult     edge.Operation
}

func (*projectGitSyncToolStore) DeviceActive(string) bool { return true }

func (store *projectGitSyncToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	store.resolvedTarget = name
	if store.resolveErr != nil {
		return edge.Device{}, store.resolveErr
	}
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}

func (store *projectGitSyncToolStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	store.createdKind = kind
	store.createdRequest = request
	if store.createErr != nil {
		return edge.Operation{}, false, store.createErr
	}
	return edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}, !store.reused, nil
}

func (*projectGitSyncToolStore) OperationStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectGitSyncToolStore) ActiveOperations(string, int) ([]edge.Operation, error) {
	return nil, nil
}
func (*projectGitSyncToolStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectGitSyncToolStore) RequestOperationCancel(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectGitSyncToolStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}

func (store *projectGitSyncToolStore) WaitOperation(_ context.Context, operationID string, _ time.Duration) (edge.Operation, error) {
	if store.waitErr != nil {
		return edge.Operation{ID: operationID, Kind: store.createdKind, State: edge.OperationQueued}, store.waitErr
	}
	result := store.waitResult
	result.ID = operationID
	result.Kind = store.createdKind
	return result, nil
}

func TestProjectGitSyncToolsExposeOnlyClosedProjectScopedInputs(t *testing.T) {
	server := stampServer(t)
	for _, name := range []string{"project_git_status", "project_git_fetch", "project_git_fast_forward_preview", "project_git_fast_forward"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		encoded, err := json.Marshal(entry.def.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		if !containsAll(text, `"additionalProperties":false`, `"alias"`, `"target"`) {
			t.Fatalf("%s schema=%s", name, text)
		}
		for _, forbidden := range []string{`"url"`, `"remote"`, `"refspec"`, `"force"`, `"tags"`, `"path"`} {
			if containsAll(text, forbidden) {
				t.Fatalf("%s exposed %s", name, forbidden)
			}
		}
	}
	status := server.table["project_git_status"].def.Annotations
	if status["readOnlyHint"] != true || status["destructiveHint"] != false {
		t.Fatalf("status annotations=%v", status)
	}
}

func TestProjectGitSyncHandlersQueueBoundOperationsAndReturnOnlyPublicMetadata(t *testing.T) {
	tests := []struct {
		name       string
		arguments  string
		kind       edge.OperationKind
		planID     string
		idempotent string
	}{
		{name: "status", arguments: `{"alias":"project","target":"parrot"}`, kind: edge.OperationProjectGitStatus},
		{name: "fetch", arguments: `{"alias":"project","target":"parrot","idempotency_key":"fetch-1"}`, kind: edge.OperationProjectGitFetch, idempotent: "fetch-1"},
		{name: "preview", arguments: `{"alias":"project","target":"parrot","idempotency_key":"preview-1"}`, kind: edge.OperationProjectGitFastForwardPreview, idempotent: "preview-1"},
		{name: "fast_forward", arguments: `{"alias":"project","target":"parrot","idempotency_key":"apply-1","plan_id":"gp_11111111111111111111111111111111"}`, kind: edge.OperationProjectGitFastForward, idempotent: "apply-1", planID: "gp_11111111111111111111111111111111"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &projectGitSyncToolStore{waitResult: edge.Operation{
				State: edge.OperationSucceeded,
				Result: edge.OperationResult{
					ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
					GitBranch: "main", GitHead: strings.Repeat("a", 40), GitRemoteHead: strings.Repeat("b", 40),
					GitAhead: 1, GitBehind: 2, GitDiverged: true, GitDirty: true, GitFetched: true,
					GitFastForwarded: test.kind == edge.OperationProjectGitFastForward,
					GitPlanID:        test.planID, GitPlanExpiresAt: "2026-08-02T12:00:00Z",
				},
			}}
			server := New(nil).WithEdgeStore(store)
			output, err := server.handleProjectGitSync(json.RawMessage(test.arguments), test.kind)
			if err != nil {
				t.Fatal(err)
			}
			if store.resolvedTarget != "parrot" || store.createdKind != test.kind {
				t.Fatalf("target=%q kind=%q", store.resolvedTarget, store.createdKind)
			}
			expectedRequest := edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: test.idempotent, GitPlanID: test.planID}
			if !reflect.DeepEqual(store.createdRequest, expectedRequest) {
				t.Fatalf("request=%+v want=%+v", store.createdRequest, expectedRequest)
			}
			for _, required := range []string{`"operation_state":"succeeded"`, `"alias":"project"`, `"repository":"charle-z/repo"`, `"target":"parrot"`, `"branch":"main"`, `"ahead":1`, `"behind":2`, `"diverged":true`, `"dirty":true`, `"remote_tracking_current":true`} {
				if !strings.Contains(output, required) {
					t.Fatalf("output missing %q: %s", required, output)
				}
			}
			for _, forbidden := range []string{"device_id", "workspace_id", "credential", "remote_url", "ed_111"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("output exposed %q: %s", forbidden, output)
				}
			}
		})
	}
}

func TestProjectGitSyncHandlerReportsReuseAndClosedFailureReason(t *testing.T) {
	store := &projectGitSyncToolStore{reused: true, waitResult: edge.Operation{State: edge.OperationFailed, SafeCode: "git_dirty"}}
	server := New(nil).WithEdgeStore(store)
	output, err := server.handleProjectGitSync(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"fetch-2"}`), edge.OperationProjectGitFetch)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"operation_state":"failed"`, `"reused":true`, `"reason":"git_dirty"`} {
		if !strings.Contains(output, required) {
			t.Fatalf("output missing %q: %s", required, output)
		}
	}
}

func TestProjectGitSyncHandlerRejectsInvalidDispatchBeforeQueueing(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments string
		kind      edge.OperationKind
		want      string
	}{
		{name: "unknown field", arguments: `{"alias":"project","target":"parrot","url":"https://example.invalid"}`, kind: edge.OperationProjectGitStatus, want: "unknown"},
		{name: "status idempotency", arguments: `{"alias":"project","target":"parrot","idempotency_key":"not-allowed"}`, kind: edge.OperationProjectGitStatus, want: "status accepts no plan"},
		{name: "plan on fetch", arguments: `{"alias":"project","target":"parrot","plan_id":"gp_11111111111111111111111111111111"}`, kind: edge.OperationProjectGitFetch, want: "plan is accepted only"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &projectGitSyncToolStore{}
			server := New(nil).WithEdgeStore(store)
			_, err := server.handleProjectGitSync(json.RawMessage(test.arguments), test.kind)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want substring %q", err, test.want)
			}
			if store.createdKind != "" {
				t.Fatalf("queued kind=%q", store.createdKind)
			}
		})
	}
}

func TestProjectGitSyncHandlerPropagatesResolverAndOperationErrors(t *testing.T) {
	for _, test := range []struct {
		name  string
		store *projectGitSyncToolStore
		want  string
	}{
		{name: "resolve", store: &projectGitSyncToolStore{resolveErr: errors.New("target unavailable")}, want: "target unavailable"},
		{name: "create", store: &projectGitSyncToolStore{createErr: errors.New("queue unavailable")}, want: "queue unavailable"},
		{name: "wait", store: &projectGitSyncToolStore{waitErr: errors.New("wait unavailable")}, want: "wait unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := New(nil).WithEdgeStore(test.store)
			_, err := server.handleProjectGitSync(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"fetch-errors"}`), edge.OperationProjectGitFetch)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want substring %q", err, test.want)
			}
		})
	}
}

type deviceWithoutAliasResolver struct{}

func (deviceWithoutAliasResolver) DeviceActive(string) bool { return true }

func TestProjectGitSyncHandlerRequiresConfiguredStoresAndAliasResolver(t *testing.T) {
	server := New(nil)
	if _, err := server.handleProjectGitSync(json.RawMessage(`{}`), edge.OperationProjectGitStatus); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("unconfigured err=%v", err)
	}
	server.edgeDevices = deviceWithoutAliasResolver{}
	server.edgeOperations = &projectGitSyncToolStore{}
	if _, err := server.handleProjectGitSync(json.RawMessage(`{"alias":"project","target":"parrot"}`), edge.OperationProjectGitStatus); err == nil || !strings.Contains(err.Error(), "alias resolution") {
		t.Fatalf("resolver err=%v", err)
	}
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}
