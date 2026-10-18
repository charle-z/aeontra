package workqueue

import "testing"

func TestEWMARequiresMinimumSamplesAndSeparatesKeys(t *testing.T) {
	history, err := NewEstimateHistory(EstimateConfig{Alpha: 0.2, MinimumSamples: 3})
	if err != nil {
		t.Fatal(err)
	}
	keyA := EstimateKey{Pool: "edge.parrot", Device: "parrot", Profile: "heavy"}
	keyB := EstimateKey{Pool: "edge.parrot", Device: "laptop", Profile: "heavy"}
	for _, sample := range []float64{100, 120} {
		if _, ready, err := history.Observe(keyA, sample); err != nil || ready {
			t.Fatalf("sample=%v ready=%v err=%v", sample, ready, err)
		}
	}
	estimate, ready, err := history.Observe(keyA, 140)
	if err != nil || !ready || estimate <= 100 || estimate >= 140 {
		t.Fatalf("estimate=%v ready=%v err=%v", estimate, ready, err)
	}
	if _, ready := history.Estimate(keyB); ready {
		t.Fatal("estimate leaked across device key")
	}
}

func TestEWMAClampsOutliersAndCanReset(t *testing.T) {
	history, err := NewEstimateHistory(EstimateConfig{Alpha: 0.5, MinimumSamples: 1})
	if err != nil {
		t.Fatal(err)
	}
	key := EstimateKey{Pool: "vps.build", Device: "vps", Profile: "heavy"}
	first, ready, err := history.Observe(key, 100)
	if err != nil || !ready || first != 100 {
		t.Fatalf("first=%v ready=%v err=%v", first, ready, err)
	}
	second, ready, err := history.Observe(key, 10000)
	if err != nil || !ready || second != 250 {
		t.Fatalf("clamped estimate=%v ready=%v err=%v", second, ready, err)
	}
	history.Reset(key)
	if _, ready := history.Estimate(key); ready {
		t.Fatal("reset estimate remained ready")
	}
}

func TestShadowScoreCannotChangeAuthorizationOrResources(t *testing.T) {
	request := ResourceVector{CPUMillis: 650, MemoryMiB: 1280, IOWeight: 25, PIDs: 512, Slots: 1}
	maximum := ResourceVector{CPUMillis: 800, MemoryMiB: 1792, IOWeight: 50, PIDs: 512, Slots: 1}
	originalRequest, originalMaximum := request, maximum
	score, err := CalculateShadowScore(ShadowInput{Urgency: 2, AgeSeconds: 30, Unlocks: 1, CacheBenefit: 3, EstimatedCost: 120})
	if err != nil || score <= 0 {
		t.Fatalf("score=%v err=%v", score, err)
	}
	if request != originalRequest || maximum != originalMaximum {
		t.Fatal("shadow scoring mutated authority")
	}
	for _, input := range []ShadowInput{{EstimatedCost: 0}, {Urgency: -1, EstimatedCost: 1}, {Urgency: 1, EstimatedCost: 1e20}} {
		if _, err := CalculateShadowScore(input); err == nil {
			t.Fatalf("invalid shadow input accepted: %+v", input)
		}
	}
}
