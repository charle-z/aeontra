package workqueue

import (
	"errors"
	"sort"
	"time"
)

type FairConfig struct {
	Quantum    int64
	AgingLimit time.Duration
}

type FairCandidate struct {
	JobID     string
	Workspace string
	Cost      int64
	CreatedAt time.Time
}

type FairScheduler struct {
	config   FairConfig
	deficits map[string]int64
	cursor   string
}

func NewFairScheduler(config FairConfig) (*FairScheduler, error) {
	if config.Quantum < 1 || config.Quantum > maxResourceValue || config.AgingLimit < time.Second || config.AgingLimit > 24*time.Hour {
		return nil, errors.New("workqueue: fair scheduler configuration is invalid")
	}
	return &FairScheduler{config: config, deficits: make(map[string]int64)}, nil
}

func (s *FairScheduler) Select(now time.Time, candidates []FairCandidate) (FairCandidate, error) {
	if s == nil || now.IsZero() || len(candidates) == 0 || len(candidates) > 4096 {
		return FairCandidate{}, errors.New("workqueue: fair selection is invalid")
	}
	queues := make(map[string][]FairCandidate)
	workspaces := make([]string, 0)
	seenJobs := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !jobIDPattern.MatchString(candidate.JobID) || !workspacePattern.MatchString(candidate.Workspace) || candidate.Cost < 1 || candidate.Cost > maxResourceValue || candidate.CreatedAt.IsZero() || candidate.CreatedAt.After(now) {
			return FairCandidate{}, errors.New("workqueue: fair candidate is invalid")
		}
		if _, exists := seenJobs[candidate.JobID]; exists {
			return FairCandidate{}, errors.New("workqueue: duplicate fair candidate")
		}
		seenJobs[candidate.JobID] = struct{}{}
		if _, exists := queues[candidate.Workspace]; !exists {
			workspaces = append(workspaces, candidate.Workspace)
		}
		queues[candidate.Workspace] = append(queues[candidate.Workspace], candidate)
	}
	for workspace := range s.deficits {
		if _, active := queues[workspace]; !active {
			delete(s.deficits, workspace)
		}
	}
	for workspace := range queues {
		sort.Slice(queues[workspace], func(i, j int) bool {
			left, right := queues[workspace][i], queues[workspace][j]
			if left.CreatedAt.Equal(right.CreatedAt) {
				return left.JobID < right.JobID
			}
			return left.CreatedAt.Before(right.CreatedAt)
		})
	}
	sort.Strings(workspaces)

	var aged *FairCandidate
	for _, workspace := range workspaces {
		candidate := queues[workspace][0]
		if now.Sub(candidate.CreatedAt) < s.config.AgingLimit {
			continue
		}
		if aged == nil || candidate.CreatedAt.Before(aged.CreatedAt) || (candidate.CreatedAt.Equal(aged.CreatedAt) && candidate.JobID < aged.JobID) {
			copy := candidate
			aged = &copy
		}
	}
	if aged != nil {
		s.cursor = aged.Workspace
		return *aged, nil
	}

	start := 0
	if s.cursor != "" {
		index := sort.SearchStrings(workspaces, s.cursor)
		if index < len(workspaces) && workspaces[index] == s.cursor {
			start = (index + 1) % len(workspaces)
		}
	}
	for rounds := 0; rounds < len(workspaces)+1; rounds++ {
		for offset := 0; offset < len(workspaces); offset++ {
			workspace := workspaces[(start+offset)%len(workspaces)]
			deficit := s.deficits[workspace]
			if deficit > maxResourceValue-s.config.Quantum {
				deficit = maxResourceValue
			} else {
				deficit += s.config.Quantum
			}
			s.deficits[workspace] = deficit
			candidate := queues[workspace][0]
			if candidate.Cost <= deficit {
				s.deficits[workspace] = deficit - candidate.Cost
				s.cursor = workspace
				return candidate, nil
			}
		}
	}
	return FairCandidate{}, errors.New("workqueue: no fair candidate fits current deficit")
}
