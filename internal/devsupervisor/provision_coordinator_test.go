package devsupervisor

import (
	"context"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestProvisionPoolRecoversLostBindingWithoutAnotherScheduler(t *testing.T) {
	store, source, supervisor, objective, plan := provisioningFixture(t, "objective-provision-round")
	lost := &lostProvisionACK{Store: store, enqueue: true}
	supervisor, err := supervisor.WithProvisioning(lost, fixedProvisioner{plans: []development.ProvisionPlan{plan}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.ProvisionStep(context.Background(), objective.ObjectiveID, "validate", supervisorSourceDigest("a")); err == nil {
		t.Fatal("lost ACK fixture did not run")
	}
	executor := &recordingProvisionExecutor{source: source, environment: supervisorEnvironment(t, "rust", plan.OutputClass, 1, "toolchain.rust.1.95.0"), pending: true}
	registry := map[string]ProvisionExecutor{plan.Provider: executor}
	if err := supervisor.ReconcileProvisionPool(context.Background(), plan.Pool, "coordinator-provision", registry); err != nil {
		t.Fatal(err)
	}
	if len(executor.requests) != 1 || executor.requests[0].Provision.JobID == "" || executor.requests[0].Provision.JobFence == 0 {
		t.Fatal("coordinator did not recover and bind the job before dispatch")
	}
	executor.pending = false
	if err := supervisor.ReconcileProvisionPool(context.Background(), plan.Pool, "coordinator-provision", registry); err != nil {
		t.Fatal(err)
	}
	current, err := supervisor.Status(context.Background(), objective.ObjectiveID)
	if err != nil || current.Steps[0].Provisioning[0].State != development.ProvisioningSucceeded {
		t.Fatal("coordinator did not persist completion")
	}
	count := len(executor.requests)
	if err := supervisor.ReconcileProvisionPool(context.Background(), plan.Pool, "coordinator-provision", registry); err != nil || len(executor.requests) != count {
		t.Fatalf("terminal job replayed effect: %v", err)
	}
}
