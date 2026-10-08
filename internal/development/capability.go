package development

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	capabilitySetDigestDomain = "aeontra-development-capability-set-v1\x00"
	attestationDigestDomain   = "aeontra-development-capability-attestation-v1\x00"
)

var (
	capabilityIDPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	capabilityVersionPattern = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){0,3}$`)
	identityPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// CapabilityID names one execution property. Repositories may require a
// capability name, but only an attested environment can satisfy it.
type CapabilityID string

// Requirement is one capability required by an execution step.
type Requirement struct {
	ID CapabilityID
}

// CapabilitySet is a canonical immutable set used by environment attestations.
type CapabilitySet struct {
	ids []CapabilityID
}

func NewCapabilitySet(raw ...string) (CapabilitySet, error) {
	ids := make([]CapabilityID, 0, len(raw))
	for _, value := range raw {
		id, err := normalizeCapabilityID(value)
		if err != nil {
			return CapabilitySet{}, err
		}
		ids = append(ids, id)
	}
	return capabilitySetFromIDs(ids)
}

func Requirements(raw ...string) ([]Requirement, error) {
	ids := make([]CapabilityID, 0, len(raw))
	for _, value := range raw {
		id, err := normalizeCapabilityID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
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

func (s CapabilitySet) IDs() []CapabilityID {
	return append([]CapabilityID(nil), s.ids...)
}

func (s CapabilitySet) Has(id CapabilityID) bool {
	if !capabilityIDPattern.MatchString(string(id)) {
		return false
	}
	index := sort.Search(len(s.ids), func(i int) bool { return s.ids[i] >= id })
	return index < len(s.ids) && s.ids[index] == id
}

func (s CapabilitySet) Missing(requirements []Requirement) []CapabilityID {
	missing := make([]CapabilityID, 0, len(requirements))
	for _, requirement := range requirements {
		if !s.Has(requirement.ID) {
			missing = append(missing, requirement.ID)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}

func (s CapabilitySet) Digest() string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(capabilitySetDigestDomain))
	for _, id := range s.ids {
		_, _ = hash.Write([]byte(id))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func capabilitySetFromIDs(ids []CapabilityID) (CapabilitySet, error) {
	seen := make(map[CapabilityID]struct{}, len(ids))
	canonical := make([]CapabilityID, 0, len(ids))
	for _, id := range ids {
		if !capabilityIDPattern.MatchString(string(id)) {
			return CapabilitySet{}, errors.New("development capability id is invalid")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		canonical = append(canonical, id)
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i] < canonical[j] })
	return CapabilitySet{ids: canonical}, nil
}

func normalizeCapabilityID(raw string) (CapabilityID, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if !capabilityIDPattern.MatchString(value) {
		return "", errors.New("development capability id is invalid")
	}
	return CapabilityID(value), nil
}

// VersionCapabilityIDs returns the generic capability plus cumulative numeric
// version prefixes. An observed 1.26.6 therefore satisfies exact requirements
// for 1, 1.26 and 1.26.6 without claiming compatibility with another major or
// minor version.
func VersionCapabilityIDs(base, version string) ([]CapabilityID, error) {
	baseID, err := normalizeCapabilityID(base)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalCapabilityVersion(version)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(canonical, ".")
	result := []CapabilityID{baseID}
	for index := range parts {
		id, err := normalizeCapabilityID(string(baseID) + ".v" + strings.Join(parts[:index+1], "-"))
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func VersionRequirement(base, version string) (Requirement, error) {
	ids, err := VersionCapabilityIDs(base, version)
	if err != nil {
		return Requirement{}, err
	}
	return Requirement{ID: ids[len(ids)-1]}, nil
}

func canonicalCapabilityVersion(raw string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if !capabilityVersionPattern.MatchString(value) {
		return "", errors.New("development capability version is invalid")
	}
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	for _, part := range parts {
		if len(part) > 1 && part[0] == '0' {
			return "", errors.New("development capability version is not canonical")
		}
	}
	return strings.Join(parts, "."), nil
}

type AuthorityTier uint8

const (
	TierL3Sandbox AuthorityTier = iota + 1
	TierWorkcell
	TierManagedToolchain
	TierToolbox
	TierRootlessRuntime
	TierBrokeredPrivileged
	TierIsolatedRunner
)

type ExecutionClass string

const (
	ClassL3Sandbox                ExecutionClass = "l3-sandbox"
	ClassWorkcell                 ExecutionClass = "workcell"
	ClassManagedToolchain         ExecutionClass = "managed-toolchain"
	ClassToolbox                  ExecutionClass = "toolbox"
	ClassRootlessRuntime          ExecutionClass = "rootless-runtime"
	ClassBrokeredPrivileged       ExecutionClass = "brokered-privileged"
	ClassIsolatedRunner           ExecutionClass = "isolated-runner"
	ClassExternalValidationRunner ExecutionClass = "external-validation-runner"
)

func (class ExecutionClass) Tier() (AuthorityTier, bool) {
	switch class {
	case ClassL3Sandbox:
		return TierL3Sandbox, true
	case ClassWorkcell:
		return TierWorkcell, true
	case ClassManagedToolchain:
		return TierManagedToolchain, true
	case ClassToolbox:
		return TierToolbox, true
	case ClassRootlessRuntime:
		return TierRootlessRuntime, true
	case ClassBrokeredPrivileged:
		return TierBrokeredPrivileged, true
	case ClassIsolatedRunner, ClassExternalValidationRunner:
		return TierIsolatedRunner, true
	default:
		return 0, false
	}
}

// EnvironmentAttestation is server-owned evidence about one execution target.
// Repository content never constructs or enlarges it.
type EnvironmentAttestation struct {
	EnvironmentID string
	Class         ExecutionClass
	Generation    uint64
	Capabilities  CapabilitySet
	Digest        string
}

func NewEnvironmentAttestation(environmentID string, class ExecutionClass, generation uint64, capabilities CapabilitySet) (EnvironmentAttestation, error) {
	environmentID = strings.TrimSpace(environmentID)
	if !identityPattern.MatchString(environmentID) || generation == 0 {
		return EnvironmentAttestation{}, errors.New("development environment identity is invalid")
	}
	if _, ok := class.Tier(); !ok {
		return EnvironmentAttestation{}, errors.New("development execution class is invalid")
	}
	canonical, err := capabilitySetFromIDs(capabilities.ids)
	if err != nil {
		return EnvironmentAttestation{}, err
	}
	attestation := EnvironmentAttestation{
		EnvironmentID: environmentID,
		Class:         class,
		Generation:    generation,
		Capabilities:  canonical,
	}
	attestation.Digest = attestationDigest(attestation)
	return attestation, nil
}

func (a EnvironmentAttestation) Valid() bool {
	if !identityPattern.MatchString(a.EnvironmentID) || a.Generation == 0 {
		return false
	}
	if _, ok := a.Class.Tier(); !ok {
		return false
	}
	canonical, err := capabilitySetFromIDs(a.Capabilities.ids)
	if err != nil || canonical.Digest() != a.Capabilities.Digest() {
		return false
	}
	return a.Digest == attestationDigest(a)
}

func attestationDigest(attestation EnvironmentAttestation) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(attestationDigestDomain))
	_, _ = hash.Write([]byte(attestation.EnvironmentID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(attestation.Class))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.FormatUint(attestation.Generation, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(attestation.Capabilities.Digest()))
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// ResolutionPolicy is the objective's authority envelope. Empty allowlists are
// invalid so execution never gains a new class merely because one is available.
type ResolutionPolicy struct {
	maxTier AuthorityTier
	allowed []ExecutionClass
}

func NewResolutionPolicy(maxTier AuthorityTier, allowed ...ExecutionClass) (ResolutionPolicy, error) {
	if maxTier < TierL3Sandbox || maxTier > TierIsolatedRunner || len(allowed) == 0 {
		return ResolutionPolicy{}, errors.New("development resolution policy is invalid")
	}
	seen := make(map[ExecutionClass]struct{}, len(allowed))
	classes := make([]ExecutionClass, 0, len(allowed))
	for _, class := range allowed {
		tier, ok := class.Tier()
		if !ok || tier > maxTier {
			return ResolutionPolicy{}, errors.New("development resolution policy is invalid")
		}
		if _, duplicate := seen[class]; duplicate {
			continue
		}
		seen[class] = struct{}{}
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	return ResolutionPolicy{maxTier: maxTier, allowed: classes}, nil
}

func (p ResolutionPolicy) MaxTier() AuthorityTier {
	return p.maxTier
}

func (p ResolutionPolicy) AllowedClasses() []ExecutionClass {
	return append([]ExecutionClass(nil), p.allowed...)
}

func (p ResolutionPolicy) Allows(attestation EnvironmentAttestation) bool {
	if !attestation.Valid() {
		return false
	}
	tier, _ := attestation.Class.Tier()
	if tier > p.maxTier {
		return false
	}
	index := sort.Search(len(p.allowed), func(i int) bool { return p.allowed[i] >= attestation.Class })
	return index < len(p.allowed) && p.allowed[index] == attestation.Class
}

func (p ResolutionPolicy) AllowsClass(class ExecutionClass) bool {
	tier, ok := class.Tier()
	if !ok || tier > p.maxTier {
		return false
	}
	index := sort.Search(len(p.allowed), func(i int) bool { return p.allowed[i] >= class })
	return index < len(p.allowed) && p.allowed[index] == class
}

func (p ResolutionPolicy) valid() bool {
	if p.maxTier < TierL3Sandbox || p.maxTier > TierIsolatedRunner || len(p.allowed) == 0 {
		return false
	}
	for index, class := range p.allowed {
		tier, ok := class.Tier()
		if !ok || tier > p.maxTier || (index > 0 && p.allowed[index-1] >= class) {
			return false
		}
	}
	return true
}
