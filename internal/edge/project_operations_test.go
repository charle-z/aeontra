package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestProjectOperationsUseHumanAliasesAndSafeResults(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	prepare := OperationRequest{Alias: "Project", Repository: "Repo", TargetAlias: "Parrot", Profile: "linux-workcell"}
	op, _, err := store.CreateOperation(device.ID, OperationProjectPrepare, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if op.Request.Alias != "project" || op.Request.Repository != "Repo" || op.Request.TargetAlias != "parrot" {
		t.Fatalf("normalized request=%+v", op.Request)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "Repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	}
	if _, err := store.CompleteOperation(device.ID, op.ID, lease.LeaseID, result, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateOperation(device.ID, OperationProjectStatus, OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []OperationRequest{
		{Alias: "project", Repository: "owner/repo", TargetAlias: "parrot", Profile: "linux-workcell"},
		{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: "linux-workcell", WorkspaceID: "ws_0123456789abcdef0123456789abcdef"},
		{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: "sandbox"},
	} {
		if _, _, err := store.CreateOperation(device.ID, OperationProjectPrepare, invalid); err == nil {
			t.Fatalf("unsafe project request accepted: %+v", invalid)
		}
	}
	if validOperationCompletion(OperationResult{WorkspaceID: result.WorkspaceID, ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "htb-linux"}, "") {
		t.Fatal("non-development project result accepted")
	}
	mixed := result
	mixed.Commit = "0123456789abcdef0123456789abcdef01234567"
	if validOperationCompletion(mixed, "") {
		t.Fatal("project result accepted unrelated operation metadata")
	}
}

func TestResolveActiveDeviceNameRequiresUniqueActiveAlias(t *testing.T) {
	store := openHTTPTestStore(t)
	pair := func(name string) Device {
		code, _ := store.CreatePairing(time.Minute)
		publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		device, err := store.Pair(code, name, publicKey)
		if err != nil {
			t.Fatal(err)
		}
		return device
	}
	first := pair("parrot")
	resolved, err := store.ResolveActiveDeviceName(" PARROT ")
	if err != nil || resolved.ID != first.ID {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	_ = pair("parrot")
	if _, err := store.ResolveActiveDeviceName("parrot"); err == nil {
		t.Fatal("ambiguous target alias accepted")
	}
	if err := store.Revoke(first.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err = store.ResolveActiveDeviceName("parrot")
	if err != nil || resolved.ID == first.ID {
		t.Fatalf("resolved after revoke=%+v err=%v", resolved, err)
	}
}
