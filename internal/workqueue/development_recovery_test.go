package workqueue

import (
	"fmt"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestDevelopmentProvisionOwnerRecoversUnboundJobAndRejectsAmbiguity(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "provision-owner"})
	plan, err := development.NewProvisionPlan("toolchain", "development.toolchain", "go", development.ClassWorkcell, "", []development.CapabilityID{"toolchain.go"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		objective := developmentObjectiveFixture(t, fmt.Sprintf("objective-provision-owner-%d", i))
		objective.Scope, err = development.NewObjectiveScope("project", "test-edge")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
			t.Fatal(err)
		}
		objective, err = objective.PlanProvisioning("validate", "provision-owner-1", objectiveSourceDigest("a"), plan)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
			t.Fatal(err)
		}
		owner, step, found, err := store.DevelopmentProvisionOwner("provision-owner-1")
		if i == 0 {
			if err != nil || !found || step != "validate" || owner.ObjectiveID != objective.ObjectiveID || owner.Steps[0].Provisioning[0].JobID != "" {
				t.Fatalf("lost-binding lookup=%+v step=%q found=%t err=%v", owner, step, found, err)
			}
		} else if err == nil || found {
			t.Fatal("ambiguous provision identity was accepted")
		}
	}
	if _, _, found, err := store.DevelopmentProvisionOwner("not-present"); err != nil || found {
		t.Fatalf("absent provision found=%t err=%v", found, err)
	}
}

func TestLeasesForHolderCannotBeCrowdedOutByTerminalHistory(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "provision-leases"})
	const pool, holder = "development.toolchain", "provision-worker"
	for i := 0; i < MaxListResults+1; i++ {
		job, _, err := store.Enqueue(Spec{IdempotencyKey: fmt.Sprintf("provision-%03d", i), Workspace: "project", Pool: pool, Profile: "go", PayloadHash: objectiveSourceDigest("a")})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := store.LeaseNext(pool, holder, time.Minute)
		if err != nil || lease.Job.ID != job.ID {
			t.Fatal(err)
		}
		if _, err := store.Complete(job.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "provisioned"}); err != nil {
			t.Fatal(err)
		}
	}
	job, _, err := store.Enqueue(Spec{IdempotencyKey: "provision-active", Workspace: "project", Pool: pool, Profile: "go", PayloadHash: objectiveSourceDigest("b")})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext(pool, holder, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.LeasesForHolder(pool, holder, 4)
	if err != nil || len(leases) != 1 || leases[0].Job.ID != job.ID || leases[0].ID != lease.ID || leases[0].Fence != lease.Fence || leases[0].ExpiresAt.IsZero() {
		t.Fatalf("active leases=%+v err=%v", leases, err)
	}
	if leases, err := store.LeasesForHolder(pool, "other-worker", 4); err != nil || len(leases) != 0 {
		t.Fatal("lease list crossed worker identity")
	}
}
