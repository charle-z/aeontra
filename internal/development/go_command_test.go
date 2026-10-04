package development

import (
	"strings"
	"testing"
)

func TestGoCommandRequirementEvidenceIsCanonicalAndSourceBound(t *testing.T) {
	source := "sha256:" + strings.Repeat("a", 64)
	conservative := []CapabilityID{"toolchain.go.v1-26-6", "toolchain.go.v1-26-8", "toolchain.pnpm.v10-13-1"}
	evidence := GoCommandRequirements{Version: 1, SourceDigest: source, ExactRequirements: []CapabilityID{"toolchain.go.v1-26-8"}, MinimumVersions: []GoMinimum{{Manifest: "go.mod", Version: "1.26.6"}}}
	if !evidence.Valid(source, conservative) {
		t.Fatal("canonical provenance rejected")
	}
	for name, mutate := range map[string]func(*GoCommandRequirements){
		"version":    func(e *GoCommandRequirements) { e.Version++ },
		"source":     func(e *GoCommandRequirements) { e.SourceDigest = "sha256:" + strings.Repeat("b", 64) },
		"drop pin":   func(e *GoCommandRequirements) { e.ExactRequirements = nil },
		"invent pin": func(e *GoCommandRequirements) { e.ExactRequirements = []CapabilityID{"toolchain.go.v1-26-9"} },
		"wrong tool": func(e *GoCommandRequirements) { e.ExactRequirements = []CapabilityID{"toolchain.pnpm.v10-13-1"} },
		"duplicate pin": func(e *GoCommandRequirements) {
			e.ExactRequirements = append(e.ExactRequirements, e.ExactRequirements[0])
		},
		"wrong manifest":                 func(e *GoCommandRequirements) { e.MinimumVersions[0].Manifest = "package.json" },
		"workspace cannot explain a pin": func(e *GoCommandRequirements) { e.MinimumVersions[0].Manifest = "go.work" },
		"duplicate manifest":             func(e *GoCommandRequirements) { e.MinimumVersions = append(e.MinimumVersions, e.MinimumVersions[0]) },
		"minimum mismatch":               func(e *GoCommandRequirements) { e.MinimumVersions[0].Version = "1.26.5" },
		"noncanonical":                   func(e *GoCommandRequirements) { e.MinimumVersions[0].Version = "1.026.6" },
		"range":                          func(e *GoCommandRequirements) { e.MinimumVersions[0].Version = ">=1.26.6" },
		"unbounded":                      func(e *GoCommandRequirements) { e.MinimumVersions[0].Version = strings.Repeat("1", 256) + ".26.6" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := evidence
			changed.MinimumVersions = append([]GoMinimum(nil), evidence.MinimumVersions...)
			changed.ExactRequirements = append([]CapabilityID(nil), evidence.ExactRequirements...)
			mutate(&changed)
			if changed.Valid(source, conservative) {
				t.Fatal("malformed/conflicting evidence accepted")
			}
		})
	}
}

func TestGoCommandMinimumDoesNotRelaxExactResolver(t *testing.T) {
	ids, _ := VersionCapabilityIDs("toolchain.go", "1.26.8")
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, string(id))
	}
	set, _ := NewCapabilitySet(names...)
	required, _ := Requirements("toolchain.go.v1-26-6")
	if len(set.Missing(required)) != 1 || set.Has("toolchain.go.v1-26-6") {
		t.Fatal("newer Go falsely attests older exact patch")
	}
}
