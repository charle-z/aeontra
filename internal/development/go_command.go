package development

import (
	"go/version"
	"slices"
	"strings"
)

// GoCommandRequirements is optional, source-bound inspection evidence for the
// registered Go command. It is not an environment attestation or a replacement
// for the conservative requirements used by other commands and older Edges.
type GoCommandRequirements struct {
	Version           int            `json:"version"`
	SourceDigest      string         `json:"source_digest"`
	ExactRequirements []CapabilityID `json:"exact_requirements"`
	MinimumVersions   []GoMinimum    `json:"minimum_versions"`
}

type GoMinimum struct {
	Manifest string `json:"manifest"`
	Version  string `json:"version"`
}

// Valid binds canonical provenance to the same source and conservative Go
// observations. Every legacy Go requirement must remain explained by a module
// minimum or an exact pin; unrelated tool requirements are not reinterpreted.
func (e GoCommandRequirements) Valid(source string, conservative []CapabilityID) bool {
	if e.Version != 1 || !sourceDigestPattern.MatchString(source) || e.SourceDigest != source ||
		len(e.ExactRequirements) > 1 || len(e.MinimumVersions) < 1 || len(e.MinimumVersions) > 2 {
		return false
	}
	covered := make([]CapabilityID, 0, len(e.ExactRequirements)+2)
	for i, id := range e.ExactRequirements {
		if i > 0 && e.ExactRequirements[i-1] >= id {
			return false
		}
		pin, ok := strings.CutPrefix(string(id), "toolchain.go.v")
		if !ok || !CanonicalGoVersion(strings.ReplaceAll(pin, "-", ".")) || !slices.Contains(conservative, id) {
			return false
		}
		covered = append(covered, id)
	}
	for i, minimum := range e.MinimumVersions {
		if (minimum.Manifest != "go.mod" && minimum.Manifest != "go.work") ||
			(i > 0 && e.MinimumVersions[i-1].Manifest >= minimum.Manifest) || !CanonicalGoVersion(minimum.Version) {
			return false
		}
		requirement, err := VersionRequirement("toolchain.go", minimum.Version)
		if err != nil {
			return false
		}
		// The legacy detector does not read go.work. Its minimum cannot
		// explain away an exact pin observed in another manifest.
		if minimum.Manifest == "go.mod" {
			if slices.Contains(conservative, requirement.ID) {
				covered = append(covered, requirement.ID)
			} else if slices.Contains(conservative, "toolchain.go") {
				// Legacy detection does not recognize Go // comments; the
				// strict module directive supplies their minimum provenance.
				covered = append(covered, "toolchain.go")
			} else {
				return false
			}
		}
	}
	for _, id := range conservative {
		if (id == "toolchain.go" || strings.HasPrefix(string(id), "toolchain.go.")) && !slices.Contains(covered, id) {
			return false
		}
	}
	return true
}

func CanonicalGoVersion(value string) bool {
	if len(value) > 32 || strings.HasPrefix(value, "0.") || strings.Count(value, ".") < 1 || strings.Count(value, ".") > 2 ||
		!version.IsValid("go"+value) || !capabilityVersionPattern.MatchString(value) || strings.HasPrefix(value, "v") {
		return false
	}
	_, err := canonicalCapabilityVersion(value)
	return err == nil
}
