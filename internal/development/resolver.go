package development

import (
	"errors"
	"fmt"
	"sort"
)

type ResolutionFailure string

const (
	ResolutionFailurePolicy       ResolutionFailure = "policy"
	ResolutionFailureCapabilities ResolutionFailure = "capabilities"
)

var ErrNoCompatibleEnvironment = errors.New("no compatible development environment")

type ResolutionError struct {
	Reason  ResolutionFailure
	Missing []CapabilityID
}

func (e *ResolutionError) Error() string {
	if e == nil {
		return ErrNoCompatibleEnvironment.Error()
	}
	if len(e.Missing) == 0 {
		return fmt.Sprintf("%s: %s", ErrNoCompatibleEnvironment, e.Reason)
	}
	return fmt.Sprintf("%s: %s missing=%v", ErrNoCompatibleEnvironment, e.Reason, e.Missing)
}

func (e *ResolutionError) Unwrap() error {
	return ErrNoCompatibleEnvironment
}

type Resolution struct {
	Environment EnvironmentAttestation
	Migrated    bool
}

// Resolve chooses the lowest-authority single environment that satisfies every
// requirement inside the objective's policy envelope. It never combines ambient
// authority from multiple environments into one attempt.
func Resolve(requirements []Requirement, current *EnvironmentAttestation, candidates []EnvironmentAttestation, policy ResolutionPolicy) (Resolution, error) {
	if !policy.valid() {
		return Resolution{}, errors.New("development resolution policy is invalid")
	}
	normalized, err := normalizeRequirementList(requirements)
	if err != nil {
		return Resolution{}, err
	}
	if current != nil {
		if !current.Valid() {
			return Resolution{}, errors.New("current development environment attestation is invalid")
		}
		if policy.Allows(*current) && len(current.Capabilities.Missing(normalized)) == 0 {
			return Resolution{Environment: *current}, nil
		}
	}

	seen := make(map[string]string, len(candidates))
	eligible := make([]resolutionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Valid() {
			return Resolution{}, errors.New("development environment attestation is invalid")
		}
		if digest, duplicate := seen[candidate.EnvironmentID]; duplicate {
			if digest != candidate.Digest {
				return Resolution{}, errors.New("development environment generation is ambiguous")
			}
			continue
		}
		seen[candidate.EnvironmentID] = candidate.Digest
		if !policy.Allows(candidate) {
			continue
		}
		tier, _ := candidate.Class.Tier()
		eligible = append(eligible, resolutionCandidate{
			environment: candidate,
			tier:        tier,
			missing:     candidate.Capabilities.Missing(normalized),
		})
	}
	if len(eligible) == 0 {
		return Resolution{}, &ResolutionError{
			Reason:  ResolutionFailurePolicy,
			Missing: requirementIDs(normalized),
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		left, right := eligible[i], eligible[j]
		if len(left.missing) != len(right.missing) {
			return len(left.missing) < len(right.missing)
		}
		if left.tier != right.tier {
			return left.tier < right.tier
		}
		if left.environment.Class != right.environment.Class {
			return left.environment.Class < right.environment.Class
		}
		return left.environment.EnvironmentID < right.environment.EnvironmentID
	})
	best := eligible[0]
	if len(best.missing) != 0 {
		return Resolution{}, &ResolutionError{
			Reason:  ResolutionFailureCapabilities,
			Missing: append([]CapabilityID(nil), best.missing...),
		}
	}
	migrated := current != nil && current.Digest != best.environment.Digest
	return Resolution{Environment: best.environment, Migrated: migrated}, nil
}

type resolutionCandidate struct {
	environment EnvironmentAttestation
	tier        AuthorityTier
	missing     []CapabilityID
}

func normalizeRequirementList(requirements []Requirement) ([]Requirement, error) {
	ids := make([]CapabilityID, 0, len(requirements))
	for _, requirement := range requirements {
		if !capabilityIDPattern.MatchString(string(requirement.ID)) {
			return nil, errors.New("development capability requirement is invalid")
		}
		ids = append(ids, requirement.ID)
	}
	set, err := capabilitySetFromIDs(ids)
	if err != nil {
		return nil, err
	}
	result := make([]Requirement, 0, len(set.ids))
	for _, id := range set.ids {
		result = append(result, Requirement{ID: id})
	}
	return result, nil
}

func requirementIDs(requirements []Requirement) []CapabilityID {
	result := make([]CapabilityID, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, requirement.ID)
	}
	return result
}
