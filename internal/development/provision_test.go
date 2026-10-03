package development

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestObjectiveRecordPreservesDurableProvisioning(t *testing.T) {
	policy := mustPolicy(t, TierManagedToolchain, ClassWorkcell, ClassManagedToolchain)
	objective, err := NewObjective("objective-provision", policy, []StepSpec{{StepID: "validate", Requirements: mustRequirements(t, "toolchain.go")}})
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := objective.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	plan, err := NewProvisionPlan("toolchain", "toolchain", "go", ClassManagedToolchain, "", []CapabilityID{"toolchain.go"})
	if err != nil {
		t.Fatal(err)
	}
	step := record["steps"].([]any)[0].(map[string]any)
	step["provisioning"] = []any{map[string]any{
		"provision_id": "provision-one", "source_digest": "sha256:" + strings.Repeat("a", 64), "state": "planned",
		"plan": map[string]any{"version": 1, "provider": plan.Provider, "pool": plan.Pool,
			"profile": plan.Profile, "output_class": plan.OutputClass, "capabilities": plan.Capabilities, "digest": plan.Digest},
	}}
	record["state"] = "running"
	record["revision"] = float64(2)
	body, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := ParseObjectiveRecord(body)
	if err != nil {
		t.Fatalf("durable provisioning record rejected: %v", err)
	}
	canonical, _, err := recovered.MarshalRecord()
	if err != nil || !strings.Contains(string(canonical), plan.Digest) {
		t.Fatalf("provisioning plan lost after round trip: %s err=%v", canonical, err)
	}
}

func provisionObjectiveFixture(t *testing.T) (Objective, ProvisionPlan) {
	t.Helper()
	scope, err := NewObjectiveScope("project", "edge")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := NewScopedObjective("objective-provision", scope,
		mustPolicy(t, TierManagedToolchain, ClassWorkcell, ClassManagedToolchain),
		[]StepSpec{{StepID: "validate", Requirements: mustRequirements(t, "toolchain.go")}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewProvisionPlan("toolchain", "toolchain", "go", ClassManagedToolchain, "", []CapabilityID{"toolchain.go"})
	if err != nil {
		t.Fatal(err)
	}
	return objective, plan
}

func TestObjectiveProvisioningRequiresFenceAndCannotRewriteHistory(t *testing.T) {
	objective, plan := provisionObjectiveFixture(t)
	before := objective
	objective, err := objective.PlanProvisioning("validate", "provision-one", "sha256:"+strings.Repeat("a", 64), plan)
	if err != nil || ValidateTransition(before, objective) != nil {
		t.Fatalf("plan err=%v", err)
	}
	if _, _, err := objective.PlanAttempt("validate", "attempt-one", "sha256:"+strings.Repeat("a", 64),
		[]EnvironmentAttestation{mustEnvironment(t, "toolchain", ClassManagedToolchain, 1, "toolchain.go")}); err == nil {
		t.Fatal("execution overlaps provisioning")
	}
	if _, err := objective.RefineRequirements("validate", mustRequirements(t, "build.make")); err == nil {
		t.Fatal("active provisioning requirements changed")
	}
	before = objective
	objective, err = objective.BindProvisionJob("validate", "provision-one", "wj_"+strings.Repeat("b", 32))
	if err != nil || ValidateTransition(before, objective) != nil {
		t.Fatalf("bind err=%v", err)
	}
	if _, err := objective.CompleteProvision("validate", "provision-one", "sha256:"+strings.Repeat("c", 64)); err == nil {
		t.Fatal("receipt without fence accepted")
	}
	before = objective
	objective, err = objective.BindProvisionFence("validate", "provision-one", 2)
	if err != nil || ValidateTransition(before, objective) != nil {
		t.Fatalf("fence err=%v", err)
	}
	if _, err := objective.BindProvisionFence("validate", "provision-one", 1); err == nil {
		t.Fatal("stale fence accepted")
	}
	before = objective
	objective, err = objective.CompleteProvision("validate", "provision-one", "sha256:"+strings.Repeat("c", 64))
	if err != nil || ValidateTransition(before, objective) != nil {
		t.Fatalf("completion err=%v", err)
	}
	if objective.State != ObjectiveRunning || objective.Steps[0].State != StepPlanned {
		t.Fatal("provisioning counted as semantic success")
	}
	if _, err := objective.Accept(); err == nil {
		t.Fatal("provisioning accepted objective")
	}
	for _, mutate := range []func(*ProvisioningAttempt){
		func(p *ProvisioningAttempt) { p.SourceDigest = "sha256:" + strings.Repeat("d", 64) },
		func(p *ProvisioningAttempt) { p.JobID = "wj_" + strings.Repeat("e", 32) },
		func(p *ProvisioningAttempt) { p.ReceiptDigest = "sha256:" + strings.Repeat("f", 64) },
		func(p *ProvisioningAttempt) { p.JobFence++ },
	} {
		forged := objective.clone()
		forged.Revision++
		mutate(&forged.Steps[0].Provisioning[0])
		if ValidateTransition(objective, forged) == nil {
			t.Fatal("terminal provisioning history rewritten")
		}
	}
	clone := objective.clone()
	clone.Steps[0].Provisioning[0].Plan.Capabilities[0] = "host.escape"
	if objective.Steps[0].Provisioning[0].Plan.Capabilities[0] != "toolchain.go" {
		t.Fatal("plan shares mutable capability slice")
	}
}

func TestObjectiveProvisioningBoundAndCancellation(t *testing.T) {
	objective, plan := provisionObjectiveFixture(t)
	for i := 0; i < MaxProvisioningAttemptsPerStep; i++ {
		id := fmt.Sprintf("provision-%d", i)
		next, err := objective.PlanProvisioning("validate", id, "sha256:"+strings.Repeat("a", 64), plan)
		if err != nil {
			t.Fatal(err)
		}
		next, err = next.BindProvisionJob("validate", id, fmt.Sprintf("wj_%032x", i+1))
		if err != nil {
			t.Fatal(err)
		}
		next, err = next.FailProvision("validate", id, FailureExternalTransient)
		if err != nil {
			t.Fatal(err)
		}
		objective = next
	}
	if _, err := objective.PlanProvisioning("validate", "provision-overflow", "sha256:"+strings.Repeat("a", 64), plan); err == nil {
		t.Fatal("unbounded provisioning")
	}
	objective, plan = provisionObjectiveFixture(t)
	queued, err := objective.PlanProvisioning("validate", "provision-cancel", "sha256:"+strings.Repeat("a", 64), plan)
	if err != nil {
		t.Fatal(err)
	}
	queued, err = queued.BindProvisionJob("validate", "provision-cancel", "wj_"+strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := queued.Cancel()
	if err != nil || !cancelled.Valid() || ValidateTransition(queued, cancelled) != nil || cancelled.Steps[0].Provisioning[0].State != ProvisioningCancelled {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestProvisionTransitionRejectsSkippedFenceAndActiveRequirementChange(t *testing.T) {
	objective, plan := provisionObjectiveFixture(t)
	planned, err := objective.PlanProvisioning("validate", "provision-one", "sha256:"+strings.Repeat("a", 64), plan)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := planned.BindProvisionJob("validate", "provision-one", "wj_"+strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	forged := queued.clone()
	forged.Steps[0].Provisioning[0].JobFence = 99
	if ValidateTransition(planned, forged) == nil {
		t.Fatal("job bind also forged unobserved lease fence")
	}
	// A refined subset of the promised capabilities still cannot mutate an active contract.
	plan, err = NewProvisionPlan("toolchain", "toolchain", "go", ClassManagedToolchain, "", []CapabilityID{"toolchain.go", "build.make"})
	if err != nil {
		t.Fatal(err)
	}
	planned, err = objective.PlanProvisioning("validate", "provision-two", "sha256:"+strings.Repeat("a", 64), plan)
	if err != nil {
		t.Fatal(err)
	}
	forged = planned.clone()
	forged.Revision++
	forged.Steps[0].Requirements = mustRequirements(t, "build.make", "toolchain.go")
	if !forged.Valid() {
		t.Fatal("fixture should be a valid snapshot")
	}
	if ValidateTransition(planned, forged) == nil {
		t.Fatal("active provision contract changed during effect")
	}
}
