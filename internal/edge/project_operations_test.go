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
	windowsResult := result
	windowsResult.ProjectProfile = "windows-workcell"
	if !validOperationCompletion(windowsResult, "") {
		t.Fatal("Windows development project result rejected")
	}
	windowsResult.ProjectMode = "htb-linux"
	if validOperationCompletion(windowsResult, "") {
		t.Fatal("Windows HTB project result accepted")
	}
	toolchainResult := result
	toolchainResult.ProjectToolchainState = "edge-required"
	toolchainResult.ProjectToolchainRoute = "edge-toolbox"
	toolchainResult.ProjectToolchainManifests = []string{"package.json", "pom.xml"}
	if !validOperationCompletionForKind(OperationProjectStatus, toolchainResult, "") {
		t.Fatal("valid toolchain summary rejected")
	}
	if validOperationCompletionForKind(OperationProjectPrepare, toolchainResult, "") {
		t.Fatal("toolchain summary accepted for project preparation")
	}
	toolchainResult.ProjectToolchainRoute = "l3"
	if validOperationCompletionForKind(OperationProjectStatus, toolchainResult, "") {
		t.Fatal("inconsistent toolchain route accepted")
	}
	diagnosticResult := OperationResult{
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "Repo", ProjectTarget: "parrot",
		ProjectState: "timeout", ProjectReason: "checkout_timeout", ProjectDiagnosticReason: "git_status_timeout",
		ProjectRepairable: true, ProjectRecommendedAction: "project_reconcile",
	}
	if !validOperationCompletionForKind(OperationProjectStatus, diagnosticResult, "") {
		t.Fatal("actionable project status diagnostic rejected")
	}
	if validOperationCompletionForKind(OperationProjectPrepare, diagnosticResult, "") {
		t.Fatal("project status diagnostic accepted for preparation")
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

func TestProjectRegistryRecoveryOperationContracts(t *testing.T) {
	list, err := validateOperationRequest(OperationProjectRegistryList, OperationRequest{TargetAlias: "Parrot", Profile: "linux-workcell"})
	if err != nil || list.TargetAlias != "parrot" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	reconcile, err := validateOperationRequest(OperationProjectReconcile, OperationRequest{Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell"})
	if err != nil || reconcile.Alias != "project" || reconcile.TargetAlias != "parrot" {
		t.Fatalf("reconcile=%+v err=%v", reconcile, err)
	}
	release, err := validateOperationRequest(OperationProjectRelease, OperationRequest{Alias: "Project", Repository: "Repo", TargetAlias: "Parrot", Profile: "linux-workcell", ProjectClaimGeneration: 3})
	if err != nil || release.Alias != "project" || release.Repository != "repo" || release.TargetAlias != "parrot" || release.ProjectClaimGeneration != 3 {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	if _, err := validateOperationRequest(OperationProjectRelease, OperationRequest{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: "linux-workcell", ProjectClaimGeneration: uint64(1 << 63)}); err == nil {
		t.Fatal("release generation above signed SQLite range accepted")
	}
	for kind, invalids := range map[OperationKind][]OperationRequest{
		OperationProjectRegistryList: {
			{TargetAlias: "parrot", Profile: "linux-workcell", Alias: "project"},
			{TargetAlias: "parrot", Profile: "linux-workcell", ProjectClaimGeneration: 1},
		},
		OperationProjectReconcile: {
			{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", Repository: "repo"},
			{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", ProjectClaimGeneration: 1},
		},
		OperationProjectRelease: {
			{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: "linux-workcell"},
			{Alias: "project", Repository: "owner/repo", TargetAlias: "parrot", Profile: "linux-workcell", ProjectClaimGeneration: 1},
		},
	} {
		for _, invalid := range invalids {
			if _, err := validateOperationRequest(kind, invalid); err == nil {
				t.Fatalf("invalid registry request accepted kind=%s request=%+v", kind, invalid)
			}
		}
	}
	claims := OperationResult{ProjectRegistryAction: "listed", ProjectClaims: []ProjectClaimSummary{{Alias: "project", Owner: "charle-z", Repository: "repo", Target: "parrot", Generation: 1, State: "stale", Reason: "workspace_unavailable", Repairable: true}}}
	if !validOperationCompletionForKind(OperationProjectRegistryList, claims, "") || validOperationCompletionForKind(OperationProjectReconcile, OperationResult{ProjectRegistryAction: "listed", ProjectClaims: claims.ProjectClaims}, "") {
		t.Fatal("registry list/reconcile result contract is not kind-bound")
	}
	released := OperationResult{ProjectRegistryAction: "released", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot", ProjectState: "released", ProjectClaimGeneration: 1}
	if !validOperationCompletionForKind(OperationProjectRelease, released, "") || validOperationCompletionForKind(OperationProjectRelease, released, "unsafe") {
		t.Fatal("release result contract is invalid")
	}
	released.ProjectClaimGeneration = uint64(1 << 63)
	if validOperationCompletionForKind(OperationProjectRelease, released, "") {
		t.Fatal("release result generation above signed SQLite range accepted")
	}
}

func TestProjectSnapshotOperationIsDurablyIdempotent(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{
		Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell",
		IdempotencyKey: "chat-vertical-1",
	}
	operation, fresh, err := store.CreateOperation(device.ID, OperationProjectSnapshot, request)
	if err != nil || !fresh || operation.State != OperationQueued {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		SnapshotBranch: "main", SnapshotHead: "0123456789abcdef0123456789abcdef01234567", SnapshotClean: true,
	}
	completed, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, "")
	if err != nil || completed.State != OperationSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationProjectSnapshot, request)
	if err != nil || fresh || reused.ID != operation.ID || reused.State != OperationSucceeded {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	conflict := request
	conflict.Alias = "other-project"
	if _, _, err := store.CreateOperation(device.ID, OperationProjectSnapshot, conflict); err == nil {
		t.Fatal("idempotency key accepted different snapshot parameters")
	}
	request.IdempotencyKey = "chat-vertical-2"
	second, fresh, err := store.CreateOperation(device.ID, OperationProjectSnapshot, request)
	if err != nil || !fresh || second.ID == operation.ID {
		t.Fatalf("second=%+v fresh=%t err=%v", second, fresh, err)
	}
	request.IdempotencyKey = "bad key"
	if _, _, err := store.CreateOperation(device.ID, OperationProjectSnapshot, request); err == nil {
		t.Fatal("unsafe idempotency key accepted")
	}
	unsafe := result
	unsafe.SnapshotHead = "not-a-commit"
	if validOperationCompletion(unsafe, "") {
		t.Fatal("unsafe project snapshot result accepted")
	}
	dirty := result
	dirty.ProjectState = "dirty"
	dirty.SnapshotClean = false
	if !validOperationCompletion(dirty, "") {
		t.Fatal("dirty project snapshot result rejected")
	}
	inconsistent := dirty
	inconsistent.SnapshotClean = true
	if validOperationCompletion(inconsistent, "") {
		t.Fatal("inconsistent dirty project snapshot result accepted")
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
