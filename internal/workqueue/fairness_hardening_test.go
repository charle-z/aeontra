package workqueue

import (
	"testing"
	"time"
)

func TestFairSchedulerRejectsDuplicateJobIdentity(t *testing.T) {
	now := time.Unix(6000, 0).UTC()
	scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	jobID := "wj_00000000000000000000000000000001"
	_, err = scheduler.Select(now, []FairCandidate{
		{JobID: jobID, Workspace: "alpha", Cost: 5, CreatedAt: now},
		{JobID: jobID, Workspace: "beta", Cost: 5, CreatedAt: now},
	})
	if err == nil {
		t.Fatal("duplicate job identity accepted")
	}
}

func TestFairSchedulerDropsDeficitForInactiveWorkspace(t *testing.T) {
	now := time.Unix(7000, 0).UTC()
	scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	alpha := FairCandidate{JobID: "wj_00000000000000000000000000000011", Workspace: "alpha", Cost: 20, CreatedAt: now}
	beta := FairCandidate{JobID: "wj_00000000000000000000000000000012", Workspace: "beta", Cost: 10, CreatedAt: now}
	if _, err := scheduler.Select(now.Add(time.Second), []FairCandidate{alpha, beta}); err != nil {
		t.Fatal(err)
	}
	if _, exists := scheduler.deficits["alpha"]; !exists {
		t.Fatal("expected active workspace deficit")
	}
	if _, err := scheduler.Select(now.Add(2*time.Second), []FairCandidate{beta}); err != nil {
		t.Fatal(err)
	}
	if _, exists := scheduler.deficits["alpha"]; exists {
		t.Fatal("inactive workspace deficit retained")
	}
}
