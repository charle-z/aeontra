package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectToolEdgeStore struct {
	resolvedTarget string
	createdKind    edge.OperationKind
	createdRequest edge.OperationRequest
	statusResult   *edge.OperationResult
}

func (store *projectToolEdgeStore) DeviceActive(string) bool { return true }

func (store *projectToolEdgeStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	store.resolvedTarget = name
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}

func (store *projectToolEdgeStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	store.createdKind = kind
	store.createdRequest = request
	return edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}, true, nil
}

func (store *projectToolEdgeStore) OperationStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}

func (store *projectToolEdgeStore) ActiveOperations(string, int) ([]edge.Operation, error) {
	return nil, nil
}

func (store *projectToolEdgeStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}

func (store *projectToolEdgeStore) RequestOperationCancel(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}

func (store *projectToolEdgeStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}

func (store *projectToolEdgeStore) WaitOperation(_ context.Context, operationID string, _ time.Duration) (edge.Operation, error) {
	operation := edge.Operation{
		ID: operationID, DeviceID: "ed_11111111111111111111111111111111", Kind: store.createdKind,
		State: edge.OperationSucceeded,
		Result: edge.OperationResult{
			WorkspaceID:  "ws_33333333333333333333333333333333",
			ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
			ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		},
	}
	if store.createdKind == edge.OperationProjectSnapshot {
		operation.Result.SnapshotBranch = "main"
		operation.Result.SnapshotHead = "0123456789abcdef0123456789abcdef01234567"
		operation.Result.SnapshotClean = true
	}
	if store.createdKind == edge.OperationProjectStatus {
		if store.statusResult != nil {
			operation.Result = *store.statusResult
			return operation, nil
		}
		operation.Result.ProjectToolchainState = "edge-required"
		operation.Result.ProjectToolchainRoute = "edge-toolbox"
		operation.Result.ProjectToolchainManifests = []string{"rust-toolchain.toml", "package.json"}
	}
	if store.createdKind == edge.OperationProjectRegistryList {
		operation.Result = edge.OperationResult{ProjectRegistryAction: "listed", ProjectClaims: []edge.ProjectClaimSummary{{
			Alias: "project", Owner: "charle-z", Repository: "repo", Target: "parrot", WorkspaceID: "ws_33333333333333333333333333333333", Generation: 7, State: "repairable", Reason: "workspace_missing", Repairable: true,
		}}}
	}
	if store.createdKind == edge.OperationProjectReconcile {
		operation.Result = edge.OperationResult{ProjectRegistryAction: "reconciled", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot", ProjectClaimGeneration: 7}
	}
	if store.createdKind == edge.OperationProjectRelease {
		operation.Result = edge.OperationResult{ProjectRegistryAction: "released", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot", ProjectClaimGeneration: 7}
	}
	return operation, nil
}

func TestProjectStatusReturnsActionableRedactedDiagnostic(t *testing.T) {
	store := &projectToolEdgeStore{statusResult: &edge.OperationResult{
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		ProjectState: "identity_mismatch", ProjectReason: "checkout_identity_mismatch",
		ProjectDiagnosticReason: "workspace_identity_mismatch", ProjectRepairable: true, ProjectRecommendedAction: "project_reconcile",
	}}
	server := New(nil).WithEdgeStore(store)
	output, err := server.table["project_status"].handler(json.RawMessage(`{"alias":"project","target":"parrot"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"state":"identity_mismatch"`, `"reason":"checkout_identity_mismatch"`, `"diagnostic_reason":"workspace_identity_mismatch"`, `"repairable":true`, `"recommended_action":"project_reconcile"`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("status output missing %s: %s", expected, output)
		}
	}
	for _, forbidden := range []string{`path`, `expected`, `observed`, `C:\\`, `/home/`} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("status output exposed %q: %s", forbidden, output)
		}
	}
}

func TestProjectToolsUseHumanAliasesAndHideOpaqueIDs(t *testing.T) {
	store := &projectToolEdgeStore{}
	server := New(nil).WithEdgeStore(store)
	for _, name := range []string{"project_prepare", "project_status"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		schema, err := json.Marshal(entry.def.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"device_id", "workspace_id", "operation_id", "plan_id", "path"} {
			if strings.Contains(string(schema), forbidden) {
				t.Fatalf("%s schema exposed %q: %s", name, forbidden, schema)
			}
		}
	}
	if server.table["project_prepare"].def.Annotations["openWorldHint"] != true {
		t.Fatal("project_prepare must disclose its GitHub network effect")
	}

	output, err := server.table["project_prepare"].handler(json.RawMessage(`{"alias":"project","repository":"repo","target":"parrot"}`))
	if err != nil {
		t.Fatal(err)
	}
	if store.resolvedTarget != "parrot" || store.createdKind != edge.OperationProjectPrepare {
		t.Fatalf("target=%q kind=%q", store.resolvedTarget, store.createdKind)
	}
	if !reflect.DeepEqual(store.createdRequest, edge.OperationRequest{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: "linux-workcell"}) {
		t.Fatalf("request=%+v", store.createdRequest)
	}
	for _, forbidden := range []string{"ed_111", "ws_333", "eo_222", "device_id", "workspace_id", "operation_id"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("prepare output exposed %q: %s", forbidden, output)
		}
	}
	for _, required := range []string{`"alias":"project"`, `"repository":"charle-z/repo"`, `"target":"parrot"`, `"state":"ready"`} {
		if !strings.Contains(output, required) {
			t.Fatalf("prepare output missing %q: %s", required, output)
		}
	}

	output, err = server.table["project_status"].handler(json.RawMessage(`{"alias":"project","target":"parrot"}`))
	if err != nil {
		t.Fatal(err)
	}
	if store.createdKind != edge.OperationProjectStatus || store.createdRequest.Repository != "" {
		t.Fatalf("status kind=%q request=%+v", store.createdKind, store.createdRequest)
	}
	if strings.Contains(output, "ws_333") || !strings.Contains(output, `"state":"ready"`) ||
		!strings.Contains(output, `"toolchain_state":"edge-required"`) ||
		!strings.Contains(output, `"toolchain_route":"edge-toolbox"`) ||
		!strings.Contains(output, `"toolchain_manifests":["rust-toolchain.toml","package.json"]`) {
		t.Fatalf("status output=%s", output)
	}
}

func TestProjectStatusRejectsRepositoryAndUnknownFields(t *testing.T) {
	server := New(nil).WithEdgeStore(&projectToolEdgeStore{})
	for _, input := range []string{
		`{"alias":"project","target":"parrot","repository":"repo"}`,
		`{"alias":"project","target":"parrot","device_id":"ed_11111111111111111111111111111111"}`,
	} {
		if _, err := server.table["project_status"].handler(json.RawMessage(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestProjectRegistryRecoveryToolsAreTargetScopedAndRedacted(t *testing.T) {
	store := &projectToolEdgeStore{}
	server := New(nil).WithEdgeStore(store)
	for _, name := range []string{"project_registry_list", "project_reconcile", "project_release"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		schema, err := json.Marshal(entry.def.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(schema), `"target"`) || strings.Contains(string(schema), "device_id") || strings.Contains(string(schema), "workspace_id") {
			t.Fatalf("%s schema=%s", name, schema)
		}
	}
	if server.table["project_registry_list"].def.Annotations["readOnlyHint"] != true || server.table["project_release"].def.Annotations["destructiveHint"] != true {
		t.Fatal("registry recovery annotations are incorrect")
	}

	output, err := server.table["project_registry_list"].handler(json.RawMessage(`{"target":"parrot"}`))
	if err != nil || store.createdKind != edge.OperationProjectRegistryList || store.createdRequest.TargetAlias != "parrot" {
		t.Fatalf("list output=%s request=%+v err=%v", output, store.createdRequest, err)
	}
	for _, expected := range []string{`"action":"listed"`, `"repository":"repo"`, `"generation":7`, `"repairable":true`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("list output missing %s: %s", expected, output)
		}
	}
	if strings.Contains(output, "workspace_id") || strings.Contains(output, "ws_333") {
		t.Fatalf("list output exposed private workspace identity: %s", output)
	}

	output, err = server.table["project_reconcile"].handler(json.RawMessage(`{"alias":"project","target":"parrot"}`))
	if err != nil || store.createdKind != edge.OperationProjectReconcile || !strings.Contains(output, `"action":"reconciled"`) {
		t.Fatalf("reconcile output=%s request=%+v err=%v", output, store.createdRequest, err)
	}
	output, err = server.table["project_release"].handler(json.RawMessage(`{"alias":"project","repository":"repo","target":"parrot","generation":7}`))
	if err != nil || store.createdKind != edge.OperationProjectRelease || store.createdRequest.ProjectClaimGeneration != 7 || !strings.Contains(output, `"action":"released"`) {
		t.Fatalf("release output=%s request=%+v err=%v", output, store.createdRequest, err)
	}
}

func TestProjectSnapshotUsesOneDurableIdempotentEdgeOperation(t *testing.T) {
	store := &projectToolEdgeStore{}
	server := New(nil).WithEdgeStore(store)
	entry, ok := server.table["project_snapshot"]
	if !ok {
		t.Fatal("missing project_snapshot")
	}
	if entry.def.Annotations["readOnlyHint"] != true || entry.def.Annotations["idempotentHint"] != true {
		t.Fatalf("annotations=%+v", entry.def.Annotations)
	}
	output, err := entry.handler(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"chat-vertical-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if store.resolvedTarget != "parrot" || store.createdKind != edge.OperationProjectSnapshot {
		t.Fatalf("target=%q kind=%q", store.resolvedTarget, store.createdKind)
	}
	expected := edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "chat-vertical-1"}
	if !reflect.DeepEqual(store.createdRequest, expected) {
		t.Fatalf("request=%+v", store.createdRequest)
	}
	for _, required := range []string{
		`"operation_id":"eo_22222222222222222222222222222222"`,
		`"alias":"project"`, `"repository":"charle-z/repo"`, `"target":"parrot"`,
		`"branch":"main"`, `"head":"0123456789abcdef0123456789abcdef01234567"`, `"clean":true`,
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("snapshot output missing %q: %s", required, output)
		}
	}
	for _, forbidden := range []string{"device_id", "workspace_id", "ed_111", "ws_333"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("snapshot output exposed %q: %s", forbidden, output)
		}
	}
}
