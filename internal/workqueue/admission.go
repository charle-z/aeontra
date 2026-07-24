package workqueue

import (
	"errors"
	"math"
)

const maxResourceValue int64 = 1_000_000_000

type ResourceVector struct {
	CPUMillis int64
	MemoryMiB int64
	IOWeight  int64
	PIDs      int64
	Slots     int64
}

type PoolProfile struct {
	Pool    string
	Profile string
	Budget  ResourceVector
	Maximum ResourceVector
}

type AdmissionReason string

const (
	AdmissionAllowed                AdmissionReason = "allowed"
	AdmissionInsufficientResources  AdmissionReason = "insufficient_resources"
	AdmissionProfileMaximumExceeded AdmissionReason = "profile_maximum_exceeded"
)

type AdmissionDecision struct {
	Allowed   bool
	Reason    AdmissionReason
	Remaining ResourceVector
}

type PoolRegistry struct {
	profiles map[string]PoolProfile
}

func ParseResourceVector(input map[string]float64) (ResourceVector, error) {
	var vector ResourceVector
	for name, raw := range input {
		if math.IsNaN(raw) || math.IsInf(raw, 0) || raw < 0 || raw > float64(maxResourceValue) || math.Trunc(raw) != raw {
			return ResourceVector{}, errors.New("workqueue: resource value is invalid")
		}
		value := int64(raw)
		switch name {
		case "cpu_millis":
			vector.CPUMillis = value
		case "memory_mib":
			vector.MemoryMiB = value
		case "io_weight":
			vector.IOWeight = value
		case "pids":
			vector.PIDs = value
		case "slots":
			vector.Slots = value
		default:
			return ResourceVector{}, errors.New("workqueue: resource dimension is unknown")
		}
	}
	if err := validateResourceVector(vector, false); err != nil {
		return ResourceVector{}, err
	}
	return vector, nil
}

func NewPoolRegistry(profiles []PoolProfile) (*PoolRegistry, error) {
	if len(profiles) == 0 || len(profiles) > 256 {
		return nil, errors.New("workqueue: pool registry is invalid")
	}
	registry := &PoolRegistry{profiles: make(map[string]PoolProfile, len(profiles))}
	for _, item := range profiles {
		if !poolPattern.MatchString(item.Pool) || !profilePattern.MatchString(item.Profile) || validateResourceVector(item.Budget, true) != nil || validateResourceVector(item.Maximum, true) != nil || !item.Maximum.fits(item.Budget) {
			return nil, errors.New("workqueue: pool profile is invalid")
		}
		key := item.Pool + "\x00" + item.Profile
		if _, exists := registry.profiles[key]; exists {
			return nil, errors.New("workqueue: duplicate pool profile")
		}
		registry.profiles[key] = item
	}
	return registry, nil
}

func (r *PoolRegistry) Admit(pool, profile string, active, requested ResourceVector) (AdmissionDecision, error) {
	if r == nil || !poolPattern.MatchString(pool) || !profilePattern.MatchString(profile) || validateResourceVector(active, false) != nil || validateResourceVector(requested, true) != nil {
		return AdmissionDecision{}, errors.New("workqueue: admission request is invalid")
	}
	item, found := r.profiles[pool+"\x00"+profile]
	if !found {
		return AdmissionDecision{}, errors.New("workqueue: pool profile not found")
	}
	if !requested.fits(item.Maximum) {
		return AdmissionDecision{Reason: AdmissionProfileMaximumExceeded}, nil
	}
	used, ok := active.add(requested)
	if !ok || !used.fits(item.Budget) {
		return AdmissionDecision{Reason: AdmissionInsufficientResources}, nil
	}
	return AdmissionDecision{Allowed: true, Reason: AdmissionAllowed, Remaining: item.Budget.subtract(used)}, nil
}

func validateResourceVector(vector ResourceVector, requirePositive bool) error {
	values := []int64{vector.CPUMillis, vector.MemoryMiB, vector.IOWeight, vector.PIDs, vector.Slots}
	for _, value := range values {
		if value < 0 || value > maxResourceValue {
			return errors.New("workqueue: resource vector is invalid")
		}
	}
	if requirePositive && (vector.CPUMillis == 0 || vector.MemoryMiB == 0 || vector.IOWeight == 0 || vector.PIDs == 0 || vector.Slots == 0) {
		return errors.New("workqueue: resource vector is incomplete")
	}
	return nil
}

func (v ResourceVector) fits(limit ResourceVector) bool {
	return v.CPUMillis <= limit.CPUMillis && v.MemoryMiB <= limit.MemoryMiB && v.IOWeight <= limit.IOWeight && v.PIDs <= limit.PIDs && v.Slots <= limit.Slots
}

func (v ResourceVector) add(other ResourceVector) (ResourceVector, bool) {
	result := ResourceVector{
		CPUMillis: v.CPUMillis + other.CPUMillis,
		MemoryMiB: v.MemoryMiB + other.MemoryMiB,
		IOWeight:  v.IOWeight + other.IOWeight,
		PIDs:      v.PIDs + other.PIDs,
		Slots:     v.Slots + other.Slots,
	}
	if result.CPUMillis < v.CPUMillis || result.MemoryMiB < v.MemoryMiB || result.IOWeight < v.IOWeight || result.PIDs < v.PIDs || result.Slots < v.Slots || validateResourceVector(result, false) != nil {
		return ResourceVector{}, false
	}
	return result, true
}

func (v ResourceVector) subtract(other ResourceVector) ResourceVector {
	return ResourceVector{
		CPUMillis: v.CPUMillis - other.CPUMillis,
		MemoryMiB: v.MemoryMiB - other.MemoryMiB,
		IOWeight:  v.IOWeight - other.IOWeight,
		PIDs:      v.PIDs - other.PIDs,
		Slots:     v.Slots - other.Slots,
	}
}
