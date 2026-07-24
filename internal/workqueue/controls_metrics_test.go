package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEstimateHistoryDisableAndResetAll(t *testing.T) {
	history, err := NewEstimateHistory(EstimateConfig{Alpha: 0.2, MinimumSamples: 1})
	if err != nil {
		t.Fatal(err)
	}
	keyA := EstimateKey{Pool: "vps.build", Device: "vps", Profile: "heavy"}
	keyB := EstimateKey{Pool: "edge.parrot", Device: "parrot", Profile: "heavy"}
	if _, ready, err := history.Observe(keyA, 100); err != nil || !ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	history.SetEnabled(false)
	if _, ready, err := history.Observe(keyB, 200); err != nil || ready {
		t.Fatalf("disabled observe ready=%v err=%v", ready, err)
	}
	if _, ready := history.Estimate(keyA); ready {
		t.Fatal("disabled history returned an estimate")
	}
	history.SetEnabled(true)
	if _, ready := history.Estimate(keyA); !ready {
		t.Fatal("re-enabled history lost existing estimate")
	}
	history.ResetAll()
	if _, ready := history.Estimate(keyA); ready {
		t.Fatal("reset-all retained estimate")
	}
}

func TestQueueMetricsAreBoundedAggregateOnly(t *testing.T) {
	now := time.Unix(5000, 0).UTC()
	candidates := []FairCandidate{
		{JobID: "wj_00000000000000000000000000000001", Workspace: "alpha", Cost: 10, CreatedAt: now.Add(-20 * time.Second)},
		{JobID: "wj_00000000000000000000000000000002", Workspace: "alpha", Cost: 20, CreatedAt: now.Add(-10 * time.Second)},
		{JobID: "wj_00000000000000000000000000000003", Workspace: "beta", Cost: 30, CreatedAt: now.Add(-5 * time.Second)},
	}
	metrics, err := CalculateQueueMetrics(now, candidates, 1, 2, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Queued != 3 || metrics.Workspaces != 2 || metrics.ActiveSlots != 1 || metrics.CapacitySlots != 2 || metrics.EstimatedWaitSeconds != 120 || metrics.OldestAgeSeconds != 20 {
		t.Fatalf("metrics=%+v", metrics)
	}
	body, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"wj_", "alpha", "beta", "payload", "path"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("metrics leaked %q: %s", forbidden, body)
		}
	}
}

func TestQueueMetricsRejectInvalidBounds(t *testing.T) {
	now := time.Now().UTC()
	candidate := FairCandidate{JobID: "wj_00000000000000000000000000000001", Workspace: "alpha", Cost: 1, CreatedAt: now}
	for _, test := range []struct {
		active, capacity int64
		unit             time.Duration
	}{
		{-1, 1, time.Second},
		{2, 1, time.Second},
		{0, 0, time.Second},
		{0, 1, 0},
		{0, 1, 25 * time.Hour},
	} {
		if _, err := CalculateQueueMetrics(now, []FairCandidate{candidate}, test.active, test.capacity, test.unit); err == nil {
			t.Fatalf("invalid metrics input accepted: %+v", test)
		}
	}
}
