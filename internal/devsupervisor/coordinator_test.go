package devsupervisor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type mutableCatalogSource struct {
	mu      sync.RWMutex
	catalog development.EnvironmentCatalog
	err     error
}

func (source *mutableCatalogSource) Catalog(context.Context, development.Objective) (development.EnvironmentCatalog, error) {
	source.mu.RLock()
	defer source.mu.RUnlock()
	if source.err != nil {
		return development.EnvironmentCatalog{}, source.err
	}
	return source.catalog, nil
}

func (source *mutableCatalogSource) Set(catalog development.EnvironmentCatalog) {
	source.mu.Lock()
	source.catalog = catalog
	source.err = nil
	source.mu.Unlock()
}

func TestSupervisorPlansLeastAuthorityStartsAndRecoversAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store := openSupervisorStore(t, root, "supervisor-restart")
	l3 := supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go")
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go", "network.host-shared")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell, l3)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-restart", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassL3Sandbox, development.ClassWorkcell},
		"toolchain.go",
	)
	if _, created, err := supervisor.Create(context.Background(), objective); err != nil || !created {
		t.Fatalf("create created=%t err=%v", created, err)
	}

	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started || started.Environment.EnvironmentID != l3.EnvironmentID ||
		started.Environment.Class != development.ClassL3Sandbox {
		t.Fatalf("least-authority environment not started: %+v", started)
	}
	if len(started.Objective.Steps[0].Attempts) != 1 ||
		started.Objective.Steps[0].Attempts[0].AttemptID == "" ||
		started.Objective.Steps[0].Attempts[0].State != development.AttemptRunning {
		t.Fatalf("running attempt not persisted: %+v", started.Objective)
	}
	attemptID := started.Objective.Steps[0].Attempts[0].AttemptID
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openSupervisorStore(t, root, "supervisor-restart")
	defer store.Close()
	supervisor = newSupervisor(t, store, source)
	recovered, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Steps[0].Attempts[0].AttemptID != attemptID ||
		recovered.Steps[0].Attempts[0].State != development.AttemptRunning {
		t.Fatalf("restart lost running attempt: %+v", recovered)
	}
	replayed, err := supervisor.StartStep(context.Background(), objective.ObjectiveID, "validate")
	if err != nil || !replayed.Reused || !replayed.Started ||
		replayed.Objective.Steps[0].Attempts[0].AttemptID != attemptID {
		t.Fatalf("start replay=%+v err=%v", replayed, err)
	}
}

func TestSupervisorRejectsCatalogDriftAndAutomaticallyReplans(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-drift")
	defer store.Close()
	before := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	after := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 2, "toolchain.go", "build.make")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, before)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-drift", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	planned, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	firstID := planned.Objective.Steps[0].Attempts[0].AttemptID
	source.Set(supervisorCatalog(t, after))

	rejected, err := supervisor.StartStep(context.Background(), objective.ObjectiveID, "validate")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Started || rejected.Rejected != development.FailureCapabilityDrift ||
		rejected.Action != development.ActionProvisionOrMigrate {
		t.Fatalf("drift was not durably rejected: %+v", rejected)
	}
	if rejected.Objective.Steps[0].Attempts[0].State != development.AttemptFailed {
		t.Fatalf("drifted attempt remains runnable: %+v", rejected.Objective)
	}

	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	attempts := started.Objective.Steps[0].Attempts
	if !started.Started || len(attempts) != 2 ||
		attempts[0].AttemptID != firstID ||
		attempts[1].ParentAttemptID != firstID ||
		attempts[1].EnvironmentDigest != after.Digest ||
		attempts[1].State != development.AttemptRunning {
		t.Fatalf("drift was not automatically replanned: %+v", started)
	}
}

func TestSupervisorRefinedRequirementsMigrateWithoutManualTargetChoice(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-refine")
	defer store.Close()
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	runner := supervisorEnvironment(t, "runner", development.ClassIsolatedRunner, 1, "toolchain.go", "service.postgres")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell, runner)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-refine", development.TierIsolatedRunner,
		[]development.ExecutionClass{development.ClassWorkcell, development.ClassIsolatedRunner}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	planned, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil || planned.Resolution.Environment.EnvironmentID != workcell.EnvironmentID {
		t.Fatalf("initial plan=%+v err=%v", planned, err)
	}
	additional := supervisorRequirements(t, "service.postgres")
	if _, err := supervisor.RefineRequirements(context.Background(), objective.ObjectiveID, "validate", additional); err != nil {
		t.Fatal(err)
	}
	rejected, err := supervisor.StartStep(context.Background(), objective.ObjectiveID, "validate")
	if err != nil || rejected.Rejected != development.FailureCapabilityMissing {
		t.Fatalf("refined preflight=%+v err=%v", rejected, err)
	}
	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started || started.Environment.EnvironmentID != runner.EnvironmentID ||
		len(started.Objective.Steps[0].Attempts) != 2 {
		t.Fatalf("supervisor did not migrate to compatible runner: %+v", started)
	}
}

func TestSupervisorPlanRetryIsIdempotent(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-idempotent")
	defer store.Close()
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-idempotent", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	first, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Objective.Revision != first.Objective.Revision || !second.Reused ||
		second.Objective.Steps[0].Attempts[0].AttemptID != first.Objective.Steps[0].Attempts[0].AttemptID {
		t.Fatalf("plan retry created another effect: first=%+v second=%+v", first, second)
	}
}

func TestSupervisorConcurrentIdenticalPlanConvergesOnOneAttempt(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-concurrent")
	defer store.Close()
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-concurrent", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	const workers = 8
	results := make(chan PlanResult, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("identical concurrent plan failed: %v", err)
	}
	attemptID := ""
	count := 0
	for result := range results {
		count++
		got := result.Objective.Steps[0].Attempts[0].AttemptID
		if attemptID == "" {
			attemptID = got
		} else if got != attemptID {
			t.Fatalf("concurrent plan diverged: %s != %s", got, attemptID)
		}
	}
	if count != workers {
		t.Fatalf("plan result count=%d want=%d", count, workers)
	}
	persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil || len(persisted.Steps[0].Attempts) != 1 ||
		persisted.Steps[0].Attempts[0].AttemptID != attemptID {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}

func TestSupervisorCatalogUnavailableFailsBeforeRevision(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-catalog")
	defer store.Close()
	source := &mutableCatalogSource{err: errors.New("offline")}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-catalog", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassWorkcell}, "toolchain.go")
	created, _, err := supervisor.Create(context.Background(), objective)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("catalog error=%v", err)
	}
	current, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil || current.Revision != created.Revision || len(current.Steps[0].Attempts) != 0 {
		t.Fatalf("catalog failure mutated objective: %+v err=%v", current, err)
	}
}

func openSupervisorStore(t *testing.T, root, controller string) *workqueue.Store {
	t.Helper()
	store, err := workqueue.Open(workqueue.Config{Root: root, ControllerID: controller})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newSupervisor(t *testing.T, store ObjectiveStore, source CatalogSource) *Supervisor {
	t.Helper()
	supervisor, err := New(store, source)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor
}

func supervisorEnvironment(t *testing.T, id string, class development.ExecutionClass, generation uint64, raw ...string) development.EnvironmentAttestation {
	t.Helper()
	capabilities, err := development.NewCapabilitySet(raw...)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := development.NewEnvironmentAttestation(id, class, generation, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func supervisorCatalog(t *testing.T, environments ...development.EnvironmentAttestation) development.EnvironmentCatalog {
	t.Helper()
	catalog, err := development.NewEnvironmentCatalog(environments...)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func supervisorRequirements(t *testing.T, raw ...string) []development.Requirement {
	t.Helper()
	requirements, err := development.Requirements(raw...)
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}

func supervisorObjective(t *testing.T, id string, max development.AuthorityTier, classes []development.ExecutionClass, requirements ...string) development.Objective {
	t.Helper()
	policy, err := development.NewResolutionPolicy(max, classes...)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := development.NewObjectiveScope("project", "parrot-trusted-linux")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewScopedObjective(id, scope, policy, []development.StepSpec{{
		StepID: "validate", Requirements: supervisorRequirements(t, requirements...),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return objective
}

func supervisorSourceDigest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func TestSupervisorCodeFailureRetryKeepsAuthorityAndRequiresChangedSource(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-code-retry")
	defer store.Close()
	l3 := supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go")
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go", "network.host-shared")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, l3, workcell)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-code-retry", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassL3Sandbox, development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	first := started.Objective.Steps[0].Attempts[0]
	if first.EnvironmentID != l3.EnvironmentID {
		t.Fatalf("initial environment=%s want=%s", first.EnvironmentID, l3.EnvironmentID)
	}
	if _, err := supervisor.FailStep(context.Background(), objective.ObjectiveID, "validate", development.FailureCode); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); err == nil {
		t.Fatal("code retry reused unchanged source")
	}
	retried, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("b"))
	if err != nil {
		t.Fatal(err)
	}
	attempts := retried.Objective.Steps[0].Attempts
	if len(attempts) != 2 || attempts[1].EnvironmentDigest != first.EnvironmentDigest ||
		attempts[1].SourceDigest == first.SourceDigest || attempts[1].State != development.AttemptRunning {
		t.Fatalf("code retry changed authority or failed to bind source change: %+v", attempts)
	}
}

func TestSupervisorTransientRetryDoesNotMoveToNewLowerAuthorityEnvironment(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-transient")
	defer store.Close()
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-transient", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassL3Sandbox, development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	first := started.Objective.Steps[0].Attempts[0]
	if _, err := supervisor.FailStep(context.Background(), objective.ObjectiveID, "validate", development.FailureExternalTransient); err != nil {
		t.Fatal(err)
	}
	l3 := supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go")
	source.Set(supervisorCatalog(t, l3, workcell))

	retried, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	attempts := retried.Objective.Steps[0].Attempts
	if len(attempts) != 2 || attempts[1].EnvironmentDigest != first.EnvironmentDigest ||
		attempts[1].EnvironmentID != workcell.EnvironmentID {
		t.Fatalf("transient retry migrated authority: %+v", attempts)
	}
}

func TestSupervisorConcurrentDifferentPlansDoNotForkObjective(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-divergent")
	defer store.Close()
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, workcell)}
	supervisor := newSupervisor(t, store, source)
	objective := supervisorObjective(t, "objective-divergent", development.TierWorkcell,
		[]development.ExecutionClass{development.ClassWorkcell}, "toolchain.go")
	_, _, _ = supervisor.Create(context.Background(), objective)

	sources := []string{supervisorSourceDigest("a"), supervisorSourceDigest("b")}
	var wait sync.WaitGroup
	successes := make(chan PlanResult, len(sources))
	failures := make(chan error, len(sources))
	for _, digest := range sources {
		digest := digest
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := supervisor.PlanStep(context.Background(), objective.ObjectiveID, "validate", digest)
			if err != nil {
				failures <- err
				return
			}
			successes <- result
		}()
	}
	wait.Wait()
	close(successes)
	close(failures)

	successCount := 0
	for range successes {
		successCount++
	}
	failureCount := 0
	for range failures {
		failureCount++
	}
	if successCount != 1 || failureCount != 1 {
		t.Fatalf("divergent concurrent plan success=%d failure=%d", successCount, failureCount)
	}
	persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil || len(persisted.Steps[0].Attempts) != 1 {
		t.Fatalf("divergent plan forked objective: %+v err=%v", persisted, err)
	}
}

func TestSupervisorRefusesLegacyUnscopedObjectiveDispatch(t *testing.T) {
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), "supervisor-unscoped")
	defer store.Close()
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewObjective("objective-unscoped", policy, []development.StepSpec{{
		StepID: "validate", Requirements: supervisorRequirements(t, "toolchain.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
		t.Fatal(err)
	}
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	supervisor := newSupervisor(t, store, &mutableCatalogSource{catalog: supervisorCatalog(t, workcell)})
	if _, err := supervisor.Status(context.Background(), objective.ObjectiveID); !errors.Is(err, ErrObjectiveScopeUnavailable) {
		t.Fatalf("legacy unscoped objective became dispatchable: %v", err)
	}
}
