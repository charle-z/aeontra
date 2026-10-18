package workqueue

import (
	"errors"
	"math"
	"sync"
)

type EstimateConfig struct {
	Alpha          float64
	MinimumSamples int
}

type EstimateKey struct {
	Pool    string
	Device  string
	Profile string
}

type estimateState struct {
	value   float64
	samples int
}

type EstimateHistory struct {
	config  EstimateConfig
	mu      sync.RWMutex
	values  map[EstimateKey]estimateState
	enabled bool
}

func NewEstimateHistory(config EstimateConfig) (*EstimateHistory, error) {
	if math.IsNaN(config.Alpha) || math.IsInf(config.Alpha, 0) || config.Alpha <= 0 || config.Alpha > 1 || config.MinimumSamples < 1 || config.MinimumSamples > 1000 {
		return nil, errors.New("workqueue: estimate configuration is invalid")
	}
	return &EstimateHistory{config: config, values: make(map[EstimateKey]estimateState), enabled: true}, nil
}

func (h *EstimateHistory) Observe(key EstimateKey, sample float64) (float64, bool, error) {
	if h == nil || !validEstimateKey(key) || math.IsNaN(sample) || math.IsInf(sample, 0) || sample <= 0 || sample > 1e12 {
		return 0, false, errors.New("workqueue: estimate sample is invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled {
		return 0, false, nil
	}
	state := h.values[key]
	if state.samples == 0 {
		state.value = sample
	} else {
		minimum := state.value / 4
		maximum := state.value * 4
		if sample < minimum {
			sample = minimum
		}
		if sample > maximum {
			sample = maximum
		}
		state.value = (1-h.config.Alpha)*state.value + h.config.Alpha*sample
	}
	state.samples++
	h.values[key] = state
	return state.value, state.samples >= h.config.MinimumSamples, nil
}

func (h *EstimateHistory) Estimate(key EstimateKey) (float64, bool) {
	if h == nil || !validEstimateKey(key) {
		return 0, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.enabled {
		return 0, false
	}
	state, found := h.values[key]
	return state.value, found && state.samples >= h.config.MinimumSamples
}

func (h *EstimateHistory) Reset(key EstimateKey) {
	if h == nil || !validEstimateKey(key) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.values, key)
}

func validEstimateKey(key EstimateKey) bool {
	return poolPattern.MatchString(key.Pool) && workspacePattern.MatchString(key.Device) && profilePattern.MatchString(key.Profile)
}

type ShadowInput struct {
	Urgency       float64
	AgeSeconds    float64
	Unlocks       float64
	CacheBenefit  float64
	EstimatedCost float64
}

func CalculateShadowScore(input ShadowInput) (float64, error) {
	values := []float64{input.Urgency, input.AgeSeconds, input.Unlocks, input.CacheBenefit, input.EstimatedCost}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e12 {
			return 0, errors.New("workqueue: shadow score input is invalid")
		}
	}
	if input.EstimatedCost <= 0 {
		return 0, errors.New("workqueue: shadow score cost is invalid")
	}
	score := (4*input.Urgency + input.AgeSeconds + 2*input.Unlocks + input.CacheBenefit) / (1 + input.EstimatedCost)
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0, errors.New("workqueue: shadow score is invalid")
	}
	return score, nil
}
