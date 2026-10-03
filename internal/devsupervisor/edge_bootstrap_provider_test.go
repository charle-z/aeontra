package devsupervisor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type bootstrapJournal struct {
	*edge.Store
	loseNextResolveACK bool
	substituteResolve  bool
	createCalls        int
}

var errBootstrapLostACK = errors.New("test: Edge journal acknowledgement lost")

func (journal *bootstrapJournal) CreateOperation(device string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	journal.createCalls++
	operation, created, err := journal.Store.CreateOperation(device, kind, request)
	if err == nil && created && kind == edge.OperationProjectDevelopmentBootstrapResolve && journal.loseNextResolveACK {
		journal.loseNextResolveACK = false
		return edge.Operation{}, false, errBootstrapLostACK
	}
	return operation, created, err
}

func TestEdgeBootstrapPlansAreBoundAndSupportOnlyMissingOfficialSelectors(t *testing.T) {
	anchor := development.WorkspaceAnchor{DeviceID: "ed_" + strings.Repeat("a", 32), WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 7, Owner: "charle-z", Repository: "repo"}
	scope, err := development.NewObjectiveScope("project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	scope.Anchor = anchor
	objective, step := bootstrapPlanObjective(t, "objective-bootstrap-plans", scope,
		"toolchain.go.v1-26", "toolchain.go.v1-26-6", "toolchain.rust.v1-95-0", "toolchain.cargo.v1-95-0")
	provider := &EdgeBootstrapProvider{}
	workcell := supervisorEnvironment(t, "workcell:"+anchor.WorkspaceID, development.ClassWorkcell, anchor.Generation, "toolchain.go", "toolchain.go.v1-26")
	plans, err := provider.Plans(context.Background(), objective, step, supervisorCatalog(t, workcell))
	if err != nil || len(plans) != 1 || plans[0].Provider != edgeBootstrapProvider || plans[0].Pool != edgeBootstrapPool ||
		plans[0].Profile != edgeBootstrapProfile || plans[0].OutputClass != development.ClassWorkcell || plans[0].BaseEnvironmentDigest != workcell.Digest ||
		!plans[0].Covers(requirementIDs(step.Requirements)) {
		t.Fatalf("plans=%+v err=%v", plans, err)
	}
	selectors, err := selectorsForPlan(plans[0])
	if err != nil || len(selectors) != 2 || selectors[0].toolchain != "go" || selectors[0].capability != "toolchain.go.v1-26-6" || selectors[1].toolchain != "rust" || selectors[1].capability != "toolchain.rust.v1-95-0" {
		t.Fatalf("selectors=%+v err=%v", selectors, err)
	}
	if _, err := provider.Plans(context.Background(), objective, step, supervisorCatalog(t, supervisorEnvironment(t, "other", development.ClassWorkcell, anchor.Generation, "toolchain.go"))); err == nil {
		t.Fatal("unregistered workcell identity accepted")
	}

	for name, caps := range map[string][]string{
		"missing-host-capability": {"build.docker"},
		"conflicting-go-pins":     {"toolchain.go.v1-26", "toolchain.go.v1-27-1"},
		"unsupported-go-version":  {"toolchain.go.v2-26"},
	} {
		t.Run(name, func(t *testing.T) {
			badObjective, badStep := bootstrapPlanObjective(t, "objective-bootstrap-invalid", scope, caps...)
			_, err := provider.Plans(context.Background(), badObjective, badStep, supervisorCatalog(t, supervisorEnvironment(t, "workcell:"+anchor.WorkspaceID, development.ClassWorkcell, anchor.Generation)))
			if err == nil {
				t.Fatal("unsupported plan accepted")
			}
		})
	}
	if _, err := provider.Plans(context.Background(), objective, step, supervisorCatalog(t, supervisorEnvironment(t, "workcell:"+anchor.WorkspaceID, development.ClassL3Sandbox, anchor.Generation))); err == nil {
		t.Fatal("non-workcell environment accepted")
	}
	if unbound, unboundStep := bootstrapPlanObjective(t, "objective-bootstrap-unbound", development.ObjectiveScope{Project: "project", Target: "parrot"}, "toolchain.go.v1-26"); true {
		if _, err := provider.Plans(context.Background(), unbound, unboundStep, supervisorCatalog(t, workcell)); err == nil {
			t.Fatal("unbound scope accepted")
		}
	}
}

func TestEdgeBootstrapResolveLostACKAndRestartReuseExactJournalRecord(t *testing.T) {
	fixture := newBootstrapFixture(t, true)
	provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
	effect, err := provider.Reconcile(context.Background(), fixture.request)
	if err != nil || !effect.Pending || effect.ResultRef != "" {
		t.Fatalf("first reconcile=%+v err=%v", effect, err)
	}
	key := bootstrapOperationKey("resolve", fixture.request.Provision, bootstrapSelector{toolchain: "go", capability: "toolchain.go.v1-26", version: []string{"1", "26"}})
	first, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, key)
	if err != nil || !found || first.State != edge.OperationQueued {
		t.Fatalf("first operation=%+v found=%v err=%v", first, found, err)
	}
	if fixture.journal.createCalls != 1 {
		t.Fatalf("create calls after lost ACK=%d", fixture.journal.createCalls)
	}
	if err := fixture.journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := edge.Open(edge.Config{Root: fixture.edgeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewEdgeBootstrapProvider(reopened, fixture.queue)
	effect, err = restarted.Reconcile(context.Background(), fixture.request)
	if err != nil || !effect.Pending {
		t.Fatalf("restart reconcile=%+v err=%v", effect, err)
	}
	recovered, found, err := reopened.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, key)
	if err != nil || !found || recovered.ID != first.ID || recovered.Request.DevelopmentBootstrap == nil || recovered.Request.DevelopmentBootstrap.Resolution != nil {
		t.Fatalf("recovered=%+v found=%v err=%v", recovered, found, err)
	}
	if fixture.journal.createCalls != 1 {
		t.Fatalf("restart submitted duplicate resolve: create calls=%d", fixture.journal.createCalls)
	}
}

func TestEdgeBootstrapRejectsStaleLeaseAndWrongTargetBeforeOperation(t *testing.T) {
	fixture := newBootstrapFixture(t, false)
	provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
	stale := fixture.request
	stale.Lease.Fence++
	if _, err := provider.Reconcile(context.Background(), stale); !errors.Is(err, ErrProvisionConflict) {
		t.Fatalf("stale lease error=%v", err)
	}
	wrongTarget := fixture.request
	wrongTarget.Scope.Target = "other"
	if _, err := provider.Reconcile(context.Background(), wrongTarget); !errors.Is(err, ErrProvisionConflict) {
		t.Fatalf("wrong target error=%v", err)
	}
	key := bootstrapOperationKey("resolve", fixture.request.Provision, bootstrapSelector{toolchain: "go", capability: "toolchain.go.v1-26", version: []string{"1", "26"}})
	if _, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, key); err != nil || found {
		t.Fatalf("invalid lease staged operation found=%v err=%v", found, err)
	}
}

func TestEdgeBootstrapPinsResolvedMinorAcrossRestartAndRequiresKnownVerifiedExit(t *testing.T) {
	cases := map[string]struct {
		exitKnown bool
		marker    bool
	}{
		"verified":       {exitKnown: true, marker: true},
		"unknown-exit":   {exitKnown: false, marker: true},
		"missing-marker": {exitKnown: true, marker: false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newBootstrapFixture(t, false)
			provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
			effect, err := provider.Reconcile(context.Background(), fixture.request)
			if err != nil || !effect.Pending {
				t.Fatalf("resolve queue=%+v err=%v", effect, err)
			}
			selector := bootstrapSelector{toolchain: "go", capability: "toolchain.go.v1-26", version: []string{"1", "26"}}
			resolveKey := bootstrapOperationKey("resolve", fixture.request.Provision, selector)
			resolve, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveKey)
			if err != nil || !found {
				t.Fatalf("resolve=%+v found=%v err=%v", resolve, found, err)
			}
			binding := *resolve.Request.DevelopmentBootstrap
			resolution := goBootstrapResolution(selector.capability)
			resolutionDigest, err := development.BootstrapResolutionDigest(resolution)
			if err != nil {
				t.Fatal(err)
			}
			binding.Resolution, binding.ResolutionDigest = &resolution, resolutionDigest
			completeBootstrapOperation(t, fixture.journal.Store, resolve, bootstrapProjectResult(fixture.request.Scope, &binding, "registered"), "")

			effect, err = provider.Reconcile(context.Background(), fixture.request)
			if err != nil || !effect.Pending {
				t.Fatalf("start queue=%+v err=%v", effect, err)
			}
			startKey := bootstrapOperationKey("start", fixture.request.Provision, selector)
			start, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startKey)
			if err != nil || !found || start.Request.DevelopmentBootstrap == nil || !start.Request.DevelopmentBootstrap.Valid(true) ||
				start.Request.DevelopmentBootstrap.ResolutionDigest != resolutionDigest {
				t.Fatalf("start selection=%+v found=%v err=%v", start, found, err)
			}
			// A fresh provider must consume the exact journaled patch; it must not
			// submit a second metadata resolve for the floating minor selector.
			provider = NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
			effect, err = provider.Reconcile(context.Background(), fixture.request)
			if err != nil || !effect.Pending {
				t.Fatalf("start replay=%+v err=%v", effect, err)
			}
			if fixture.journal.createCalls != 2 {
				t.Fatalf("resolve/start were not reused: create calls=%d", fixture.journal.createCalls)
			}
			processID := "pr_" + strings.Repeat("a", 32)
			startResult := bootstrapProcessResult(fixture.request.Scope, &binding, processID, "running", false, 0, "", false)
			completeBootstrapOperation(t, fixture.journal.Store, start, startResult, "")

			effect, err = provider.Reconcile(context.Background(), fixture.request)
			if err != nil || !effect.Pending {
				t.Fatalf("first status=%+v err=%v", effect, err)
			}
			statusRequest := edge.OperationRequest{Alias: fixture.request.Scope.Project, TargetAlias: fixture.request.Scope.Target, Profile: edgeBootstrapRuntime,
				BackgroundProcessID: processID, OutputLimit: edgeBootstrapStatusLimit}
			firstStatus, found, err := fixture.journal.LatestDevelopmentProcessOperation(fixture.anchor.DeviceID, edge.OperationProjectProcessStatus, statusRequest)
			if err != nil || !found || firstStatus.State != edge.OperationQueued {
				t.Fatalf("first status=%+v found=%v err=%v", firstStatus, found, err)
			}
			completeBootstrapOperation(t, fixture.journal.Store, firstStatus, bootstrapProcessResult(fixture.request.Scope, nil, processID, "running", false, 0, "", false), "")
			effect, err = provider.Reconcile(context.Background(), fixture.request)
			if err != nil || !effect.Pending {
				t.Fatalf("running poll=%+v err=%v", effect, err)
			}
			secondStatus, found, err := fixture.journal.LatestDevelopmentProcessOperation(fixture.anchor.DeviceID, edge.OperationProjectProcessStatus, statusRequest)
			if err != nil || !found || secondStatus.ID == firstStatus.ID || secondStatus.State != edge.OperationQueued {
				t.Fatalf("new read after running=%+v found=%v err=%v", secondStatus, found, err)
			}
			stdout := "go version go1.26.6 linux/amd64\n"
			if testCase.marker {
				stdout += "mcp-devbox-bootstrap-verified=go:1.26.6\n"
			}
			completeBootstrapOperation(t, fixture.journal.Store, secondStatus,
				bootstrapProcessResult(fixture.request.Scope, nil, processID, "exited", testCase.exitKnown, 0, stdout, true), "")
			provider = NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
			effect, err = provider.Reconcile(context.Background(), fixture.request)
			if err != nil {
				t.Fatalf("terminal reconcile error=%v", err)
			}
			if name == "verified" {
				if effect.Pending || effect.Failure != "" || !regexp.MustCompile(`^rs_[a-f0-9]{32}$`).MatchString(effect.ResultRef) {
					t.Fatalf("verified result=%+v", effect)
				}
			} else if effect.Pending || effect.Failure != development.FailureReconciliationNeeded || effect.ResultRef != "" {
				t.Fatalf("unverified process result accepted: %+v", effect)
			}
		})
	}
}

func TestEdgeBootstrapRejectsJournalBindingSubstitutionBeforeStarting(t *testing.T) {
	fixture := newBootstrapFixture(t, false)
	provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
	effect, err := provider.Reconcile(context.Background(), fixture.request)
	if err != nil || !effect.Pending {
		t.Fatalf("resolve queue=%+v err=%v", effect, err)
	}
	selector := bootstrapSelector{toolchain: "go", capability: "toolchain.go.v1-26", version: []string{"1", "26"}}
	key := bootstrapOperationKey("resolve", fixture.request.Provision, selector)
	resolve, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, key)
	if err != nil || !found {
		t.Fatalf("resolve=%+v found=%v err=%v", resolve, found, err)
	}
	binding := *resolve.Request.DevelopmentBootstrap
	resolution := goBootstrapResolution(selector.capability)
	binding.Resolution = &resolution
	binding.ResolutionDigest, err = development.BootstrapResolutionDigest(resolution)
	if err != nil {
		t.Fatal(err)
	}
	completeBootstrapOperation(t, fixture.journal.Store, resolve, bootstrapProjectResult(fixture.request.Scope, &binding, "registered"), "")
	fixture.journal.substituteResolve = true
	effect, err = provider.Reconcile(context.Background(), fixture.request)
	if err != nil || effect.Failure != development.FailureReconciliationNeeded || effect.Pending {
		t.Fatalf("substitution result=%+v err=%v", effect, err)
	}
	startKey := bootstrapOperationKey("start", fixture.request.Provision, selector)
	if _, found, err := fixture.journal.Store.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startKey); err != nil || found {
		t.Fatalf("substituted selection started effect; found=%v err=%v", found, err)
	}
}

func TestEdgeBootstrapCancelCancelsOnlyExistingResolveAndRequiresReconciliationForLostStart(t *testing.T) {
	t.Run("queued resolve", func(t *testing.T) {
		fixture := newBootstrapFixture(t, false)
		provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
		if effect, err := provider.Reconcile(context.Background(), fixture.request); err != nil || !effect.Pending {
			t.Fatalf("reconcile=%+v err=%v", effect, err)
		}
		if _, err := fixture.queue.Cancel(fixture.request.Lease.Job.ID); err != nil {
			t.Fatal(err)
		}
		owner, _, found, err := fixture.queue.DevelopmentProvisionOwner(fixture.request.Provision.ProvisionID)
		if err != nil || !found {
			t.Fatalf("owner=%+v found=%v err=%v", owner, found, err)
		}
		owner, err = owner.Cancel()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := fixture.queue.SaveDevelopmentObjective(owner); err != nil {
			t.Fatal(err)
		}
		cancelledRequest := fixture.request
		cancelledRequest.Provision = owner.Steps[0].Provisioning[0]
		effect, err := provider.Cancel(context.Background(), cancelledRequest)
		if err != nil || effect.Pending || effect.Failure != "" || !strings.HasPrefix(effect.ResultRef, "rs_") {
			t.Fatalf("cancel=%+v err=%v", effect, err)
		}
	})

	t.Run("failed start without process", func(t *testing.T) {
		fixture := newBootstrapFixture(t, false)
		provider := NewEdgeBootstrapProvider(fixture.journal, fixture.queue)
		if effect, err := provider.Reconcile(context.Background(), fixture.request); err != nil || !effect.Pending {
			t.Fatalf("resolve queue=%+v err=%v", effect, err)
		}
		selector := bootstrapSelector{toolchain: "go", capability: "toolchain.go.v1-26", version: []string{"1", "26"}}
		resolveKey := bootstrapOperationKey("resolve", fixture.request.Provision, selector)
		resolve, _, _ := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapResolve, resolveKey)
		binding := *resolve.Request.DevelopmentBootstrap
		resolution := goBootstrapResolution(selector.capability)
		binding.Resolution = &resolution
		binding.ResolutionDigest, _ = development.BootstrapResolutionDigest(resolution)
		completeBootstrapOperation(t, fixture.journal.Store, resolve, bootstrapProjectResult(fixture.request.Scope, &binding, "registered"), "")
		if effect, err := provider.Reconcile(context.Background(), fixture.request); err != nil || !effect.Pending {
			t.Fatalf("start queue=%+v err=%v", effect, err)
		}
		startKey := bootstrapOperationKey("start", fixture.request.Provision, selector)
		start, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, startKey)
		if err != nil || !found {
			t.Fatalf("start=%+v found=%v err=%v", start, found, err)
		}
		completeBootstrapOperation(t, fixture.journal.Store, start, edge.OperationResult{}, "project_process_unavailable")
		effect, err := provider.Reconcile(context.Background(), fixture.request)
		if err != nil || !effect.Pending {
			t.Fatalf("recovery queue=%+v err=%v", effect, err)
		}
		recoveryKey := "bootstrap-recover:" + digestStrings("aeontra-development-bootstrap-recovery-v1", start.ID, startKey, string(selector.capability))[:32]
		recovery, found, err := fixture.journal.OperationByIdempotency(fixture.anchor.DeviceID, edge.OperationProjectDevelopmentBootstrapStart, recoveryKey)
		if err != nil || !found || recovery.Request.DevelopmentRecoveryOperationID != start.ID || recovery.Request.DevelopmentRecoveryIdempotencyKey != startKey {
			t.Fatalf("recovery=%+v found=%v err=%v", recovery, found, err)
		}
		completeBootstrapOperation(t, fixture.journal.Store, recovery, edge.OperationResult{}, "project_development_reconciliation_required")
		effect, err = provider.Reconcile(context.Background(), fixture.request)
		if err != nil || effect.Pending || effect.Failure != development.FailureReconciliationNeeded || effect.ResultRef != "" {
			t.Fatalf("unknown start was claimed clean: %+v err=%v", effect, err)
		}
	})
}

func (journal *bootstrapJournal) OperationByIdempotency(device string, kind edge.OperationKind, key string) (edge.Operation, bool, error) {
	operation, found, err := journal.Store.OperationByIdempotency(device, kind, key)
	if err == nil && found && kind == edge.OperationProjectDevelopmentBootstrapResolve && journal.substituteResolve && operation.State == edge.OperationSucceeded {
		journal.substituteResolve = false
		copy := *operation.Result.DevelopmentBootstrap
		copy.CapabilityID = "toolchain.go.v1-27"
		operation.Result.DevelopmentBootstrap = &copy
	}
	return operation, found, err
}

func goBootstrapResolution(capability development.CapabilityID) development.BootstrapResolution {
	return development.BootstrapResolution{CapabilityID: capability, Toolchain: "go", Version: "1.26.6", Platform: "amd64",
		ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("1", 64), ArtifactSize: 128}
}

func bootstrapProjectResult(scope development.ObjectiveScope, binding *edge.ProjectDevelopmentBootstrapBinding, state string) edge.OperationResult {
	return edge.OperationResult{WorkspaceID: scope.Anchor.WorkspaceID, ProjectAlias: scope.Project, ProjectOwner: scope.Anchor.Owner,
		ProjectRepository: scope.Anchor.Repository, ProjectTarget: scope.Target, ProjectState: state, ProjectProfile: edgeBootstrapRuntime,
		ProjectMode: "dev", DevelopmentBootstrap: binding}
}

func bootstrapProcessResult(scope development.ObjectiveScope, binding *edge.ProjectDevelopmentBootstrapBinding, processID, state string, exitKnown bool, exitCode int, stdout string, eof bool) edge.OperationResult {
	started := time.Now().UTC().Add(-time.Minute)
	result := bootstrapProjectResult(scope, binding, "ready")
	result.BackgroundProcessID, result.BackgroundProcessState = processID, state
	result.BackgroundStartedAt = started.Format(time.RFC3339Nano)
	result.BackgroundStdout, result.BackgroundStdoutNext, result.BackgroundStdoutEOF = stdout, int64(len(stdout)), eof
	result.BackgroundStderrEOF = eof
	result.BackgroundExitKnown, result.BackgroundExitCode = exitKnown, exitCode
	if state == "exited" || state == "stopped" || state == "failed" {
		result.BackgroundFinishedAt = started.Add(time.Second).Format(time.RFC3339Nano)
	}
	return result
}

func completeBootstrapOperation(t *testing.T, store *edge.Store, operation edge.Operation, result edge.OperationResult, safeCode string) {
	t.Helper()
	lease, err := store.LeaseOperation(operation.DeviceID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("operation lease=%+v err=%v want=%s", lease, err, operation.ID)
	}
	if _, err := store.CompleteOperation(operation.DeviceID, operation.ID, lease.LeaseID, result, safeCode); err != nil {
		t.Fatal(err)
	}
}

type bootstrapFixture struct {
	edgeRoot string
	journal  *bootstrapJournal
	queue    *workqueue.Store
	anchor   development.WorkspaceAnchor
	request  ProvisionRequest
}

func newBootstrapFixture(t *testing.T, loseACK bool) bootstrapFixture {
	t.Helper()
	root := t.TempDir()
	edgeRoot := filepath.Join(root, "edge")
	journalStore, err := edge.Open(edge.Config{Root: edgeRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journalStore.Close() })
	pairing, err := journalStore.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := journalStore.Pair(pairing, "bootstrap-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(root, "queue"), ControllerID: "bootstrap-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	anchor := development.WorkspaceAnchor{DeviceID: device.ID, WorkspaceID: "ws_" + strings.Repeat("c", 32), Generation: 3, Owner: "charle-z", Repository: "repo"}
	scope, err := development.NewObjectiveScope("project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	scope.Anchor = anchor
	plan, err := development.NewProvisionPlan(edgeBootstrapProvider, edgeBootstrapPool, edgeBootstrapProfile, development.ClassWorkcell,
		"sha256:"+strings.Repeat("d", 64), []development.CapabilityID{"toolchain.go.v1-26"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewScopedObjective("objective-bootstrap-test", scope, policy, []development.StepSpec{{StepID: "validate", Requirements: []development.Requirement{{ID: "toolchain.go.v1-26"}}}})
	if err != nil {
		t.Fatal(err)
	}
	objective, _, err = queue.SaveDevelopmentObjective(objective)
	if err != nil {
		t.Fatal(err)
	}
	objective, err = objective.PlanProvisioning("validate", "provision-bootstrap-test", "sha256:"+strings.Repeat("e", 64), plan)
	if err != nil {
		t.Fatal(err)
	}
	objective, _, err = queue.SaveDevelopmentObjective(objective)
	if err != nil {
		t.Fatal(err)
	}
	attempt := objective.Steps[0].Provisioning[0]
	job, _, err := queue.Enqueue(provisionSpec(objective, attempt))
	if err != nil {
		t.Fatal(err)
	}
	objective, err = objective.BindProvisionJob("validate", attempt.ProvisionID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	objective, _, err = queue.SaveDevelopmentObjective(objective)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := queue.LeaseNext(plan.Pool, "bootstrap-worker", time.Minute)
	if err != nil || lease.Job.ID != job.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	objective, err = objective.BindProvisionFence("validate", attempt.ProvisionID, lease.Fence)
	if err != nil {
		t.Fatal(err)
	}
	objective, _, err = queue.SaveDevelopmentObjective(objective)
	if err != nil {
		t.Fatal(err)
	}
	attempt = objective.Steps[0].Provisioning[0]
	journal := &bootstrapJournal{Store: journalStore, loseNextResolveACK: loseACK}
	return bootstrapFixture{edgeRoot: edgeRoot, journal: journal, queue: queue, anchor: anchor,
		request: ProvisionRequest{Scope: scope, Provision: attempt, Lease: lease}}
}

func bootstrapPlanObjective(t *testing.T, id string, scope development.ObjectiveScope, raw ...string) (development.Objective, development.ObjectiveStep) {
	t.Helper()
	requirements, err := development.Requirements(raw...)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewScopedObjective(id, scope, policy, []development.StepSpec{{StepID: "validate", Requirements: requirements}})
	if err != nil {
		t.Fatal(err)
	}
	return objective, objective.Steps[0]
}
