package workqueue

import (
	"testing"
	"time"
)

func TestDeficitRoundRobinPreventsWorkspaceMonopoly(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	candidates := []FairCandidate{
		{JobID: "wj_00000000000000000000000000000001", Workspace: "alpha", Cost: 10, CreatedAt: now},
		{JobID: "wj_00000000000000000000000000000002", Workspace: "alpha", Cost: 10, CreatedAt: now.Add(time.Nanosecond)},
		{JobID: "wj_00000000000000000000000000000003", Workspace: "beta", Cost: 10, CreatedAt: now.Add(2 * time.Nanosecond)},
	}
	selectionTime := now.Add(time.Second)
	first, err := scheduler.Select(selectionTime, candidates)
	if err != nil || first.Workspace != "alpha" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := scheduler.Select(selectionTime, candidates[1:])
	if err != nil || second.Workspace != "beta" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestFairSchedulerAgingPreventsStarvation(t *testing.T) {
	base := time.Unix(2000, 0).UTC()
	scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	candidates := []FairCandidate{
		{JobID: "wj_00000000000000000000000000000010", Workspace: "large", Cost: 100, CreatedAt: base},
		{JobID: "wj_00000000000000000000000000000011", Workspace: "small", Cost: 1, CreatedAt: base.Add(29 * time.Second)},
	}
	selected, err := scheduler.Select(base.Add(31*time.Second), candidates)
	if err != nil || selected.JobID != candidates[0].JobID {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
}

func TestFairSchedulerIsDeterministicForEqualCandidates(t *testing.T) {
	now := time.Unix(3000, 0).UTC()
	input := []FairCandidate{
		{JobID: "wj_00000000000000000000000000000022", Workspace: "beta", Cost: 5, CreatedAt: now},
		{JobID: "wj_00000000000000000000000000000021", Workspace: "alpha", Cost: 5, CreatedAt: now},
		{JobID: "wj_00000000000000000000000000000020", Workspace: "alpha", Cost: 5, CreatedAt: now},
	}
	for attempt := 0; attempt < 20; attempt++ {
		scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := scheduler.Select(now, input)
		if err != nil || selected.JobID != "wj_00000000000000000000000000000020" {
			t.Fatalf("attempt=%d selected=%+v err=%v", attempt, selected, err)
		}
	}
}

func TestFairSchedulerRejectsInvalidCostAndTime(t *testing.T) {
	scheduler, err := NewFairScheduler(FairConfig{Quantum: 10, AgingLimit: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Select(time.Now(), []FairCandidate{{JobID: "bad", Workspace: "alpha", Cost: 0, CreatedAt: time.Now()}}); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	if _, err := NewFairScheduler(FairConfig{Quantum: 0, AgingLimit: time.Minute}); err == nil {
		t.Fatal("invalid scheduler config accepted")
	}
}
