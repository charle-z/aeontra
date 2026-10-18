package workqueue

import (
	"math"
	"testing"
)

func TestParseResourceVectorRejectsInvalidDimensions(t *testing.T) {
	valid := map[string]float64{
		"cpu_millis": 650,
		"memory_mib": 1280,
		"io_weight":  25,
		"pids":       512,
		"slots":      1,
	}
	vector, err := ParseResourceVector(valid)
	if err != nil {
		t.Fatal(err)
	}
	if vector != (ResourceVector{CPUMillis: 650, MemoryMiB: 1280, IOWeight: 25, PIDs: 512, Slots: 1}) {
		t.Fatalf("vector=%+v", vector)
	}

	cases := []map[string]float64{
		{"cpu_millis": -1},
		{"cpu_millis": math.NaN()},
		{"cpu_millis": math.Inf(1)},
		{"cpu_millis": float64(maxResourceValue) + 1},
		{"cpu_millis": 0.5},
		{"unknown": 1},
	}
	for index, input := range cases {
		if _, err := ParseResourceVector(input); err == nil {
			t.Fatalf("case %d accepted: %#v", index, input)
		}
	}
}

func TestAdmissionNeverExceedsAnyPoolBudget(t *testing.T) {
	registry, err := NewPoolRegistry([]PoolProfile{{
		Pool:    "vps.build",
		Profile: "heavy",
		Budget:  ResourceVector{CPUMillis: 1300, MemoryMiB: 2560, IOWeight: 100, PIDs: 1024, Slots: 2},
		Maximum: ResourceVector{CPUMillis: 800, MemoryMiB: 1792, IOWeight: 50, PIDs: 512, Slots: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := ResourceVector{CPUMillis: 650, MemoryMiB: 1280, IOWeight: 25, PIDs: 512, Slots: 1}
	decision, err := registry.Admit("vps.build", "heavy", ResourceVector{}, request)
	if err != nil || !decision.Allowed || decision.Remaining != (ResourceVector{CPUMillis: 650, MemoryMiB: 1280, IOWeight: 75, PIDs: 512, Slots: 1}) {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}

	for name, active := range map[string]ResourceVector{
		"cpu":    {CPUMillis: 700, MemoryMiB: 1280, IOWeight: 25, PIDs: 512, Slots: 1},
		"memory": {CPUMillis: 650, MemoryMiB: 1400, IOWeight: 25, PIDs: 512, Slots: 1},
		"io":     {CPUMillis: 650, MemoryMiB: 1280, IOWeight: 80, PIDs: 512, Slots: 1},
		"pids":   {CPUMillis: 650, MemoryMiB: 1280, IOWeight: 25, PIDs: 600, Slots: 1},
		"slots":  {CPUMillis: 650, MemoryMiB: 1280, IOWeight: 25, PIDs: 512, Slots: 2},
	} {
		t.Run(name, func(t *testing.T) {
			decision, err := registry.Admit("vps.build", "heavy", active, request)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Allowed || decision.Reason != AdmissionInsufficientResources {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestPoolRegistryRejectsRemoteExpansionAndAmbiguity(t *testing.T) {
	_, err := NewPoolRegistry([]PoolProfile{
		{Pool: "edge.parrot", Profile: "heavy", Budget: ResourceVector{CPUMillis: 1000, MemoryMiB: 2048, IOWeight: 100, PIDs: 512, Slots: 1}, Maximum: ResourceVector{CPUMillis: 1000, MemoryMiB: 2048, IOWeight: 100, PIDs: 512, Slots: 1}},
		{Pool: "edge.parrot", Profile: "heavy", Budget: ResourceVector{CPUMillis: 1000, MemoryMiB: 2048, IOWeight: 100, PIDs: 512, Slots: 1}, Maximum: ResourceVector{CPUMillis: 1000, MemoryMiB: 2048, IOWeight: 100, PIDs: 512, Slots: 1}},
	})
	if err == nil {
		t.Fatal("duplicate pool/profile accepted")
	}

	registry, err := NewPoolRegistry([]PoolProfile{{
		Pool: "edge.parrot", Profile: "heavy",
		Budget:  ResourceVector{CPUMillis: 1000, MemoryMiB: 2048, IOWeight: 100, PIDs: 512, Slots: 1},
		Maximum: ResourceVector{CPUMillis: 800, MemoryMiB: 1792, IOWeight: 50, PIDs: 512, Slots: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := registry.Admit("edge.parrot", "heavy", ResourceVector{}, ResourceVector{CPUMillis: 900, MemoryMiB: 1280, IOWeight: 25, PIDs: 256, Slots: 1})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.Reason != AdmissionProfileMaximumExceeded {
		t.Fatalf("decision=%+v", decision)
	}
	if _, err := registry.Admit("vps.build", "heavy", ResourceVector{}, ResourceVector{Slots: 1}); err == nil {
		t.Fatal("unknown pool accepted")
	}
}
