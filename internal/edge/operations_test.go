package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLabOperationIsClosedIdempotentAndDurable(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{Platform: "htb", Machine: "Cap", Target: "10.129.63.164", Difficulty: "easy", OperatingSystem: "linux"}
	created, fresh, err := store.CreateOperation(device.ID, OperationLabPrepare, request)
	if err != nil || !fresh || created.State != OperationQueued {
		t.Fatalf("created=%+v fresh=%t err=%v", created, fresh, err)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationLabPrepare, request)
	if err != nil || fresh || reused.ID != created.ID {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != created.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{WorkspaceID: "ws_0123456789abcdef0123456789abcdef", AuthorizationRevision: 1}
	completed, err := store.CompleteOperation(device.ID, created.ID, lease.LeaseID, result, "")
	if err != nil || completed.State != OperationSucceeded || !reflect.DeepEqual(completed.Result, result) {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	after, err := store.OperationStatus(created.ID)
	if err != nil || !reflect.DeepEqual(after.Result, result) {
		t.Fatalf("after=%+v err=%v", after, err)
	}
}

func TestLabOperationsRejectPublicTargetsAndUnsafeCompletion(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	if _, _, err := store.CreateOperation(device.ID, OperationLabPrepare, OperationRequest{Platform: "htb", Machine: "Cap", Target: "8.8.8.8", Difficulty: "easy", OperatingSystem: "linux"}); err == nil {
		t.Fatal("public target accepted")
	}
	if validOperationCompletion(OperationResult{}, "contains-target-10.10.10.10") {
		t.Fatal("unsafe failure code accepted")
	}
}

func TestAutopilotControlOperationsPublishOnlySafeDurableMetadata(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	workspaceID := "ws_0123456789abcdef0123456789abcdef"
	if _, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{{WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "htb-linux"}}); err != nil {
		t.Fatal(err)
	}
	operation, _, err := store.CreateOperation(device.ID, OperationAutopilotStart, OperationRequest{WorkspaceID: workspaceID, RunUntil: "completed_or_cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{WorkspaceID: workspaceID, JobID: "aj_0123456789abcdef0123456789abcdef", JobState: "running", ProgressRevision: 3, CycleCount: 7}
	if _, err = store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.ReportAutopilot(device.ID, result); err != nil {
		t.Fatal(err)
	}
	status, err := store.AutopilotStatus(workspaceID)
	if err != nil || !reflect.DeepEqual(status, result) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	second, fresh, err := store.CreateOperation(device.ID, OperationAutopilotStart, OperationRequest{WorkspaceID: workspaceID, RunUntil: "completed_or_cancelled"})
	if err != nil || !fresh || second.ID == operation.ID {
		t.Fatalf("second=%+v fresh=%t err=%v", second, fresh, err)
	}
}

func TestBundleOperationsAcceptOnlyOfficialClosedRequests(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	if _, _, err := store.CreateOperation(device.ID, OperationBundleUpdate, OperationRequest{Release: "stable"}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []OperationRequest{{Release: "https://evil.example/bundle"}, {Release: "stable", Target: "10.0.0.1"}, {Machine: "command"}} {
		if _, _, err := store.CreateOperation(device.ID, OperationBundleUpdate, request); err == nil {
			t.Fatalf("unsafe update accepted: %+v", request)
		}
	}
	if _, _, err := store.CreateOperation(device.ID, OperationEdgeRepair, OperationRequest{Release: "stable"}); err == nil {
		t.Fatal("repair accepted caller options")
	}
}

func TestOperationLeaseRejectsVersionSkewWithoutBlockingRecovery(t *testing.T) {
	const protocol = "mcp-devbox.edge-bundle.v1"
	catalog := "sha256:" + strings.Repeat("a", 64)
	otherCatalog := "sha256:" + strings.Repeat("b", 64)

	newFixture := func(t *testing.T, kind OperationKind, request OperationRequest) (*Store, Device) {
		t.Helper()
		store := openHTTPTestStore(t)
		code, _ := store.CreatePairing(time.Minute)
		publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		device, err := store.Pair(code, "parrot-edge", publicKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetExpectedOperationCompatibility(protocol, catalog); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CreateOperation(device.ID, kind, request); err != nil {
			t.Fatal(err)
		}
		return store, device
	}

	t.Run("ordinary work fails with an actionable compatibility error", func(t *testing.T) {
		store, device := newFixture(t, OperationProjectStatus, OperationRequest{Alias: "codex", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell"})
		_, err := store.LeaseOperationCompatible(device.ID, time.Minute, protocol, otherCatalog)
		var compatibilityErr *OperationCompatibilityError
		if !errors.As(err, &compatibilityErr) || compatibilityErr.ExpectedCatalog != catalog || compatibilityErr.ObservedCatalog != otherCatalog {
			t.Fatalf("compatibility error=%#v", err)
		}
	})

	t.Run("recovery work remains leaseable", func(t *testing.T) {
		store, device := newFixture(t, OperationBundleStatus, OperationRequest{})
		lease, err := store.LeaseOperationCompatible(device.ID, time.Minute, protocol, otherCatalog)
		if err != nil || lease.Operation.Kind != OperationBundleStatus {
			t.Fatalf("lease=%+v err=%v", lease, err)
		}
	})

	t.Run("matching bundle leases ordinary work", func(t *testing.T) {
		store, device := newFixture(t, OperationProjectStatus, OperationRequest{Alias: "codex", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell"})
		lease, err := store.LeaseOperationCompatible(device.ID, time.Minute, protocol, catalog)
		if err != nil || lease.Operation.Kind != OperationProjectStatus {
			t.Fatalf("lease=%+v err=%v", lease, err)
		}
	})
}

func TestBundleDiagnosticCompletionAcceptsOnlySafePreciseStates(t *testing.T) {
	valid := OperationResult{Release: "p15.1.0", Commit: "0123456789abcdef0123456789abcdef01234567", ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true}
	if !validOperationCompletion(valid, "") {
		t.Fatal("valid signed bundle diagnostic rejected")
	}
	withIdentity := valid
	withIdentity.EdgeProtocolVersion = "mcp-devbox.edge-bundle.v1"
	withIdentity.EdgeCatalogHash = "sha256:" + strings.Repeat("a", 64)
	if !validOperationCompletion(withIdentity, "") {
		t.Fatal("authenticated bundle identity rejected")
	}
	authoritative := valid
	authoritative.ServiceActive = true
	authoritative.ServiceState = "active"
	authoritative.ProcessState = "single"
	authoritative.LockState = "held"
	authoritative.Coherence = "managed"
	authoritative.ProcessRelease = valid.Release
	authoritative.ProcessCommit = valid.Commit
	if !validOperationCompletion(authoritative, "") {
		t.Fatal("authoritative runtime diagnostic rejected")
	}
	partial := OperationResult{ManifestStatus: "provider_outdated", DriverValid: true, Blockers: []string{"provider_outdated", "edge_service_inactive"}}
	if !validOperationCompletion(partial, "") {
		t.Fatal("safe partial-install diagnostic rejected")
	}
	for _, unsafe := range []OperationResult{
		{ManifestStatus: "arbitrary"},
		{ManifestStatus: "provider_outdated", ProviderValid: true},
		{ManifestStatus: "manifest_invalid", Blockers: []string{"contains-target-10.0.0.1"}},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, ServiceActive: true, ServiceState: "inactive", ProcessState: "single", LockState: "held", Coherence: "managed"},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, ServiceState: "inactive", ProcessState: "duplicate", LockState: "held", Coherence: "duplicate", ProcessRelease: "arbitrary", ProcessCommit: valid.Commit},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, ServiceState: "inactive", ProcessState: "single", LockState: "held", Coherence: "manual"},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, ServiceState: "inactive", ProcessState: "inactive", LockState: "held", Coherence: "stopped"},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, EdgeProtocolVersion: "other-bundle.v1", EdgeCatalogHash: "sha256:" + strings.Repeat("a", 64)},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, EdgeProtocolVersion: "mcp-devbox.edge-bundle.v1", EdgeCatalogHash: "sha256:invalid"},
		{Release: valid.Release, Commit: valid.Commit, ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true, EdgeProtocolVersion: "mcp-devbox.edge-bundle.v1"},
	} {
		if validOperationCompletion(unsafe, "") {
			t.Fatalf("unsafe diagnostic accepted: %+v", unsafe)
		}
	}
}
