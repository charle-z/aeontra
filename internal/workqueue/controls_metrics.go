package workqueue

import (
	"errors"
	"math"
	"time"
)

func (h *EstimateHistory) SetEnabled(enabled bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

func (h *EstimateHistory) ResetAll() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.values = make(map[EstimateKey]estimateState)
}

type QueueMetrics struct {
	Queued               int64 `json:"queued"`
	Workspaces           int64 `json:"workspaces"`
	ActiveSlots          int64 `json:"active_slots"`
	CapacitySlots        int64 `json:"capacity_slots"`
	EstimatedWaitSeconds int64 `json:"estimated_wait_seconds"`
	OldestAgeSeconds     int64 `json:"oldest_age_seconds"`
}

func CalculateQueueMetrics(now time.Time, candidates []FairCandidate, activeSlots, capacitySlots int64, unitCostDuration time.Duration) (QueueMetrics, error) {
	if now.IsZero() || len(candidates) > 4096 || activeSlots < 0 || capacitySlots < 1 || activeSlots > capacitySlots || unitCostDuration < time.Millisecond || unitCostDuration > 24*time.Hour {
		return QueueMetrics{}, errors.New("workqueue: queue metrics input is invalid")
	}
	workspaces := make(map[string]struct{})
	var totalCost int64
	var oldest time.Time
	for _, candidate := range candidates {
		if !jobIDPattern.MatchString(candidate.JobID) || !workspacePattern.MatchString(candidate.Workspace) || candidate.Cost < 1 || candidate.Cost > maxResourceValue || candidate.CreatedAt.IsZero() || candidate.CreatedAt.After(now) {
			return QueueMetrics{}, errors.New("workqueue: queue metrics candidate is invalid")
		}
		if totalCost > maxResourceValue-candidate.Cost {
			return QueueMetrics{}, errors.New("workqueue: queue metrics cost overflow")
		}
		totalCost += candidate.Cost
		workspaces[candidate.Workspace] = struct{}{}
		if oldest.IsZero() || candidate.CreatedAt.Before(oldest) {
			oldest = candidate.CreatedAt
		}
	}
	available := capacitySlots - activeSlots
	if available < 1 {
		available = 1
	}
	wait := math.Ceil(float64(totalCost) * unitCostDuration.Seconds() / float64(available))
	if math.IsNaN(wait) || math.IsInf(wait, 0) || wait < 0 || wait > 1e12 {
		return QueueMetrics{}, errors.New("workqueue: queue wait estimate is invalid")
	}
	oldestAge := int64(0)
	if !oldest.IsZero() {
		oldestAge = int64(now.Sub(oldest) / time.Second)
	}
	return QueueMetrics{
		Queued:               int64(len(candidates)),
		Workspaces:           int64(len(workspaces)),
		ActiveSlots:          activeSlots,
		CapacitySlots:        capacitySlots,
		EstimatedWaitSeconds: int64(wait),
		OldestAgeSeconds:     oldestAge,
	}, nil
}
