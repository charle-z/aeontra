package devsupervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type fixedProvisioner struct {
	plans []development.ProvisionPlan
	err   error
}

func TestProvisioningUnsupportedOffersPreserveValidAlternativesAndUnavailableStates(t *testing.T) {
	for _, mode := range []string{"all-unsupported", "valid-first", "valid-last", "empty-offer", "transient-first", "transient-last", "contradictory-offer"} {
		t.Run(mode, func(t *testing.T) {
			store, source, _, objective, plan := provisioningFixture(t, "objective-provision-unsupported")
			unsupported := fixedProvisioner{err: fmt.Errorf("fixed selector: %w", ErrProvisionUnsupported)}
			valid := fixedProvisioner{plans: []development.ProvisionPlan{plan}}
			transient := fixedProvisioner{err: errors.New("provider temporarily unavailable")}
			providers := []Provisioner{unsupported, unsupported}
			want := ErrProvisionUnsupported
			switch mode {
			case "valid-first":
				providers, want = []Provisioner{valid, unsupported}, nil
			case "valid-last":
				providers, want = []Provisioner{unsupported, valid}, nil
			case "empty-offer":
				providers, want = []Provisioner{unsupported, fixedProvisioner{}}, ErrProvisionUnavailable
			case "transient-first":
				providers, want = []Provisioner{transient, unsupported}, ErrProvisionUnavailable
			case "transient-last":
				providers, want = []Provisioner{unsupported, transient}, ErrProvisionUnavailable
			case "contradictory-offer":
				providers, want = []Provisioner{fixedProvisioner{plans: valid.plans, err: ErrProvisionUnsupported}}, ErrProvisionUnavailable
			}
			supervisor, err := newSupervisor(t, store, source).WithProvisioning(store, providers...)
			if err != nil {
				t.Fatal(err)
			}
			result, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
			if !errors.Is(err, want) {
				t.Fatalf("offer classification: %v, want %v", err, want)
			}
			jobs, err := store.List(20)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
			if err != nil {
				t.Fatal(err)
			}
			if want == nil {
				if len(jobs) != 1 || len(persisted.Steps[0].Provisioning) != 1 || result.Provision.Plan.Digest != plan.Digest {
					t.Fatal("valid alternative did not select the existing governed plan")
				}
			} else if len(jobs) != 0 || len(persisted.Steps[0].Provisioning) != 0 {
				t.Fatal("unavailable or unsupported offer created an effect")
			}
		})
	}
}

func (provider fixedProvisioner) Plans(context.Context, development.Objective, development.ObjectiveStep, development.EnvironmentCatalog) ([]development.ProvisionPlan, error) {
	return provider.plans, provider.err
}

type lostProvisionACK struct {
	*workqueue.Store
	enqueue bool
	save    bool
}

var errLostProvisionACK = errors.New("test: provisioning acknowledgement lost")

func (store *lostProvisionACK) Enqueue(spec workqueue.Spec) (workqueue.Job, bool, error) {
	job, created, err := store.Store.Enqueue(spec)
	if err == nil && store.enqueue {
		store.enqueue = false
		return workqueue.Job{}, false, errLostProvisionACK
	}
	return job, created, err
}

func (store *lostProvisionACK) SaveDevelopmentObjective(objective development.Objective) (development.Objective, bool, error) {
	value, created, err := store.Store.SaveDevelopmentObjective(objective)
	if err == nil && store.save {
		store.save = false
		return development.Objective{}, false, errLostProvisionACK
	}
	return value, created, err
}

func provisioningFixture(t *testing.T, id string) (*workqueue.Store, *mutableCatalogSource, *Supervisor, development.Objective, development.ProvisionPlan) {
	t.Helper()
	store := openSupervisorStore(t, filepath.Join(t.TempDir(), "queue"), id)
	t.Cleanup(func() { _ = store.Close() })
	l3 := supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go")
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, l3)}
	objective := supervisorObjective(t, id, development.TierManagedToolchain,
		[]development.ExecutionClass{development.ClassL3Sandbox, development.ClassManagedToolchain}, "toolchain.rust.1.95.0")
	plan, err := development.NewProvisionPlan("toolchain", "development.toolchain", "rust-1.95.0", development.ClassManagedToolchain, "", []development.CapabilityID{"toolchain.rust.1.95.0"})
	if err != nil {
		t.Fatal(err)
	}
	supervisor := newSupervisor(t, store, source)
	supervisor, err = supervisor.WithProvisioning(store, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Create(context.Background(), objective); err != nil {
		t.Fatal(err)
	}
	return store, source, supervisor, objective, plan
}

func TestProvisioningLostEnqueueACKRecoversSameJobAfterSupervisorRestart(t *testing.T) {
	store, source, _, objective, plan := provisioningFixture(t, "objective-provision-lost-ack")
	lost := &lostProvisionACK{Store: store, enqueue: true, save: true}
	supervisor := newSupervisor(t, lost, source)
	supervisor, err := supervisor.WithProvisioning(lost, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); !errors.Is(err, errLostProvisionACK) {
		t.Fatalf("enqueue lost ACK err=%v", err)
	}
	persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Steps[0].Provisioning) != 1 || persisted.Steps[0].Provisioning[0].State != development.ProvisioningPlanned {
		t.Fatalf("plan was not durable before enqueue: %+v", persisted)
	}
	// Recover through a fresh supervisor, not through remembered return values.
	supervisor = newSupervisor(t, store, source)
	supervisor, err = supervisor.WithProvisioning(store, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready || result.Provision.JobID == "" || result.Provision.State != development.ProvisioningQueued {
		t.Fatalf("lost enqueue did not recover binding: %+v", result)
	}
	lease, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Job.ID != result.Provision.JobID {
		t.Fatal("recovery created a second effect")
	}
	if _, err := store.LeaseNext(plan.Pool, "provision-worker-two", time.Minute); !errors.Is(err, workqueue.ErrNoJobAvailable) {
		t.Fatalf("duplicate provision queued: %v", err)
	}
	if _, err := store.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateSucceeded, Summary: "provisioned"}); err != nil {
		t.Fatal(err)
	}
	result, err = supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if !errors.Is(err, ErrProvisionUnverified) || result.Provision.State != development.ProvisioningSucceeded || result.Provision.JobFence != lease.Fence {
		t.Fatalf("receipt bypassed re-attestation: %+v err=%v", result, err)
	}
	if result.Objective.State != development.ObjectiveRunning || len(result.Objective.Steps[0].Attempts) != 0 {
		t.Fatal("provision counted as execution")
	}
	installed := supervisorEnvironment(t, "rust", development.ClassManagedToolchain, 1, "toolchain.rust.1.95.0")
	source.Set(supervisorCatalog(t, installed))
	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil || !started.Started || started.Environment.Digest != installed.Digest || len(started.Objective.Steps[0].Provisioning) != 1 {
		t.Fatalf("same objective did not continue: %+v err=%v", started, err)
	}
}

func TestProvisioningLostACKRecoversAfterDatabaseReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	controller := "provision-restart"
	store := openSupervisorStore(t, root, controller)
	t.Cleanup(func() { _ = store.Close() })
	objective := supervisorObjective(t, "objective-provision-restart", development.TierManagedToolchain,
		[]development.ExecutionClass{development.ClassManagedToolchain}, "toolchain.rust.v1-95-0")
	plan, err := development.NewProvisionPlan("toolchain", "development.toolchain", "rust-1.95.0", development.ClassManagedToolchain, "", []development.CapabilityID{"toolchain.rust.v1-95-0"})
	if err != nil {
		t.Fatal(err)
	}
	source := &mutableCatalogSource{catalog: supervisorCatalog(t, supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go"))}
	lost := &lostProvisionACK{Store: store, enqueue: true}
	supervisor, err := newSupervisor(t, store, source).WithProvisioning(lost, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Create(context.Background(), objective); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); !errors.Is(err, errLostProvisionACK) {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openSupervisorStore(t, root, controller)
	supervisor, err = newSupervisor(t, store, source).WithProvisioning(store, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(plan.Pool, "restart-worker", time.Minute)
	if err != nil || lease.Job.ID != recovered.Provision.JobID {
		t.Fatalf("enqueue recovery=%+v lease=%+v err=%v", recovered, lease, err)
	}
	if _, err := store.Complete(lease.Job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: workqueue.StateSucceeded, Summary: "provisioned"}); err != nil {
		t.Fatal(err)
	}
	// Restart at the boundary between queue completion and objective receipt.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openSupervisorStore(t, root, controller)
	supervisor, err = newSupervisor(t, store, source).WithProvisioning(store, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err = supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if !errors.Is(err, ErrProvisionUnverified) || recovered.Provision.State != development.ProvisioningSucceeded {
		t.Fatalf("completion recovery=%+v err=%v", recovered, err)
	}
	// Receipt alone still grants no execution capability after restart.
	if _, err := store.LeaseNext(plan.Pool, "restart-worker", time.Minute); !errors.Is(err, workqueue.ErrNoJobAvailable) {
		t.Fatalf("restart duplicated effect: %v", err)
	}
	source.Set(supervisorCatalog(t, supervisorEnvironment(t, "rust", plan.OutputClass, 1, "toolchain.rust.v1-95-0")))
	started, err := supervisor.EnsureStepRunning(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil || !started.Started || len(started.Objective.Steps[0].Provisioning) != 1 {
		t.Fatalf("re-attestation continuation=%+v err=%v", started, err)
	}
}

func TestProvisioningCancelCancelsBoundQueueJob(t *testing.T) {
	store, _, supervisor, objective, _ := provisioningFixture(t, "objective-provision-cancel")
	result, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a"))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := supervisor.Cancel(context.Background(), objective.ObjectiveID)
	if err != nil {
		t.Fatal(err)
	}
	job, found, err := store.Get(result.Provision.JobID)
	if err != nil || !found || job.State != workqueue.StateCancelled || cancelled.Objective.State != development.ObjectiveCancelled {
		t.Fatalf("cancel queue=%+v objective=%+v err=%v", job, cancelled, err)
	}
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); err == nil {
		t.Fatal("cancelled objective dispatched")
	}
}

func TestProvisioningCancelRecoversJobAfterLostEnqueueACK(t *testing.T) {
	store, source, _, objective, plan := provisioningFixture(t, "objective-provision-lost-cancel")
	lost := &lostProvisionACK{Store: store, enqueue: true}
	supervisor := newSupervisor(t, store, source)
	supervisor, err := supervisor.WithProvisioning(lost, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); !errors.Is(err, errLostProvisionACK) {
		t.Fatal(err)
	}
	if _, err := supervisor.Cancel(context.Background(), objective.ObjectiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute); !errors.Is(err, workqueue.ErrNoJobAvailable) {
		t.Fatalf("cancel left orphaned effect: %v", err)
	}
}

func TestProvisioningPolicyAndProviderValidationFailBeforeEffect(t *testing.T) {
	for _, kind := range []string{"invalid-plan", "forbidden-class", "provider-error", "incomplete-capabilities"} {
		t.Run(kind, func(t *testing.T) {
			store, source, _, objective, plan := provisioningFixture(t, "objective-provision-policy")
			provider := fixedProvisioner{plans: []development.ProvisionPlan{plan}}
			switch kind {
			case "invalid-plan":
				provider.plans[0].Digest = supervisorSourceDigest("b")
			case "forbidden-class":
				provider.plans[0], _ = development.NewProvisionPlan("vm", "development.vm", "isolated", development.ClassIsolatedRunner, "", plan.Capabilities)
			case "provider-error":
				provider.err = errors.New("provider unavailable")
			case "incomplete-capabilities":
				provider.plans[0], _ = development.NewProvisionPlan("toolchain", plan.Pool, plan.Profile, plan.OutputClass, "", []development.CapabilityID{"toolchain.go"})
			}
			supervisor := newSupervisor(t, store, source)
			supervisor, err := supervisor.WithProvisioning(store, provider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); !errors.Is(err, ErrProvisionUnavailable) {
				t.Fatalf("policy err=%v", err)
			}
			persisted, err := supervisor.Status(context.Background(), objective.ObjectiveID)
			if err != nil || len(persisted.Steps[0].Provisioning) != 0 {
				t.Fatal("invalid plan persisted")
			}
			if _, err := store.LeaseNext(plan.Pool, "provision-worker-one", time.Minute); !errors.Is(err, workqueue.ErrNoJobAvailable) {
				t.Fatalf("invalid plan enqueued: %v", err)
			}
		})
	}
}
