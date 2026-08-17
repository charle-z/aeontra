package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestResolveProjectWorkspaceRequiresLatestRegisteredDevelopmentBinding(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-trusted-linux", publicKey)
	if err != nil {
		t.Fatal(err)
	}

	const (
		alias       = "codex-36010"
		target      = "parrot-trusted-linux"
		workspaceID = "ws_0123456789abcdef0123456789abcdef"
		newerID     = "ws_fedcba9876543210fedcba9876543210"
	)
	completeProjectBinding(t, store, device.ID, OperationProjectPrepare, OperationRequest{
		Alias: alias, Repository: "codex", TargetAlias: target, Profile: "linux-workcell",
	}, OperationResult{
		WorkspaceID: workspaceID, ProjectAlias: alias, ProjectOwner: "charle-z", ProjectRepository: "codex",
		ProjectTarget: target, ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	})
	if _, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{{
		WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "dev",
	}}); err != nil {
		t.Fatal(err)
	}

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`
INSERT INTO edge_operations(
    operation_id, device_id, kind, request_json, request_digest, state,
    result_json, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 300; index++ {
		otherAlias := fmt.Sprintf("other-project-%03d", index)
		requestJSON, err := json.Marshal(OperationRequest{
			Alias: otherAlias, TargetAlias: target, Profile: "linux-workcell",
		})
		if err != nil {
			t.Fatal(err)
		}
		resultJSON, err := json.Marshal(OperationResult{
			WorkspaceID: workspaceID, ProjectAlias: otherAlias, ProjectOwner: "charle-z", ProjectRepository: "other",
			ProjectTarget: target, ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := statement.Exec(
			fmt.Sprintf("eo_%032x", index+1),
			device.ID,
			OperationProjectStatus,
			requestJSON,
			fmt.Sprintf("%064x", index+1),
			OperationSucceeded,
			resultJSON,
			int64(index+1),
			int64(index+1),
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	binding, err := store.ResolveProjectWorkspace(device.ID, " CODEX-36010 ", " PARROT-TRUSTED-LINUX ")
	if err != nil || binding.WorkspaceID != workspaceID || binding.DeviceID != device.ID || binding.Profile != "linux-workcell" || binding.Mode != "dev" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	if _, err := store.ResolveProjectWorkspace(device.ID, "other-project", target); err == nil {
		t.Fatal("wrong alias resolved a project workspace")
	}
	if _, err := store.ResolveProjectWorkspace(device.ID, alias, "other-edge"); err == nil {
		t.Fatal("wrong target resolved a project workspace")
	}

	// A newer successful project binding is authoritative. If it is not in the
	// current workspace registration snapshot, fail closed instead of reviving
	// an older still-registered workspace for the same alias.
	completeProjectBinding(t, store, device.ID, OperationProjectStatus, OperationRequest{
		Alias: alias, TargetAlias: target, Profile: "linux-workcell",
	}, OperationResult{
		WorkspaceID: newerID, ProjectAlias: alias, ProjectOwner: "charle-z", ProjectRepository: "codex",
		ProjectTarget: target, ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	})
	if _, err := store.ResolveProjectWorkspace(device.ID, alias, target); err == nil {
		t.Fatal("stale older workspace was revived after a newer unregistered binding")
	}
}

func TestResolveProjectWorkspaceRejectsInactiveOrNonDevelopmentRegistration(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-trusted-linux", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	const workspaceID = "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	completeProjectBinding(t, store, device.ID, OperationProjectPrepare, OperationRequest{
		Alias: "codex", Repository: "codex", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell",
	}, OperationResult{
		WorkspaceID: workspaceID, ProjectAlias: "codex", ProjectOwner: "charle-z", ProjectRepository: "codex",
		ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	})

	if _, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{{
		WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "htb-linux",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveProjectWorkspace(device.ID, "codex", "parrot-trusted-linux"); err == nil {
		t.Fatal("non-development registration resolved as a project workspace")
	}

	if _, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{{
		WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "dev",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveProjectWorkspace(device.ID, "codex", "parrot-trusted-linux"); err == nil {
		t.Fatal("workspace on an inactive Edge resolved")
	}
}

func completeProjectBinding(t *testing.T, store *Store, deviceID string, kind OperationKind, request OperationRequest, result OperationResult) {
	t.Helper()
	operation, _, err := store.CreateOperation(deviceID, kind, request)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(deviceID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if _, err := store.CompleteOperation(deviceID, operation.ID, lease.LeaseID, result, ""); err != nil {
		t.Fatal(err)
	}
}
