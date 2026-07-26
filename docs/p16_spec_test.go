package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP16SchedulerAndEdgeLifecycleContractIsDocumented(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	files := map[string]string{
		"spec":         "../specs/007-global-work-scheduler/spec.md",
		"threat model": "../specs/007-global-work-scheduler/threat-model.md",
		"plan":         "../specs/007-global-work-scheduler/plan.md",
		"tasks":        "../specs/007-global-work-scheduler/tasks.md",
		"ADR":          "adr/0004-p16-global-scheduler-separated-execution-pools.md",
		"baseline":     "baselines/2026-07-22-p16-capacity.md",
		"migration":    "edge-lifecycle-migration.md",
		"installation": "install-edge-parrot-p16.md",
	}
	contents := make(map[string]string, len(files))
	for name, path := range files {
		contents[name] = read(path)
	}

	spec := contents["spec"]
	for _, required := range []string{
		"Global Work Scheduler and Edge lifecycle",
		"at most two operator commands",
		"must not be required to supply",
		"No automatic fallback",
		"Deficit Round Robin",
		"Cgroups/rootless controls enforce",
		"Redis is a non-goal",
		"new chat can resolve and continue a registered project by alias",
	} {
		if !containsNormalizedProse(spec, required) {
			t.Errorf("P16 spec missing %q", required)
		}
	}
	for _, literal := range []string{"650 millicores", "build@vps", "build@edge"} {
		if !strings.Contains(spec, literal) {
			t.Errorf("P16 spec missing literal %q", literal)
		}
	}

	threat := contents["threat model"]
	for _, required := range []string{
		"silent target fallback",
		"rootful Docker socket",
		"duplicate execution after disconnect",
		"Optimization equations never grant or expand authority",
	} {
		if !containsNormalizedProse(threat, required) {
			t.Errorf("P16 threat model missing %q", required)
		}
	}
	if !strings.Contains(threat, "`p12`") {
		t.Error("P16 threat model missing literal `p12`")
	}

	plan := contents["plan"]
	for _, required := range []string{
		"rollback",
	} {
		if !containsNormalizedProse(plan, required) {
			t.Errorf("P16 plan missing %q", required)
		}
	}
	for _, literal := range []string{
		"Step 1 — P12/P15 inventory and migration model",
		"Step 2 — One-command package lifecycle and guided onboarding",
		"Step 7 — Rootless VPS builder spike and selection",
		"50%, 65%, 80%",
	} {
		if !strings.Contains(plan, literal) {
			t.Errorf("P16 plan missing literal %q", literal)
		}
	}

	tasks := contents["tasks"]
	for _, required := range []string{
		"Fresh chat resolves/continues/builds a real project with alias + target only",
		"Twenty simultaneous requests yield exactly one active build",
		"No Edge-to-VPS fallback",
	} {
		if !containsNormalizedProse(tasks, required) {
			t.Errorf("P16 tasks missing %q", required)
		}
	}
	if !strings.Contains(tasks, "`p12`") {
		t.Error("P16 tasks missing literal `p12`")
	}

	baseline := contents["baseline"]
	for _, required := range []string{
		"CPU is the demonstrated binding resource",
	} {
		if !containsNormalizedProse(baseline, required) {
			t.Errorf("P16 capacity baseline missing %q", required)
		}
	}
	for _, literal := range []string{
		"2 vCPU",
		"3.805 GiB",
		"p95 host busy",
		"100.00%",
		"Second dashboard peak",
		"50/65/80",
	} {
		if !strings.Contains(baseline, literal) {
			t.Errorf("P16 capacity baseline missing literal %q", literal)
		}
	}

	adr := contents["ADR"]
	for _, required := range []string{
		"Use a private SQLite database",
		"separate execution pools",
		"No automatic fallback",
		"Redis queue",
		"opaque IDs",
	} {
		if !containsNormalizedProse(adr, required) {
			t.Errorf("P16 ADR missing %q", required)
		}
	}

	migration := contents["migration"]
	for _, required := range []string{
		"Unknown content is never moved",
		"no longer performs a direct shell move",
	} {
		if !containsNormalizedProse(migration, required) {
			t.Errorf("P16 migration documentation missing %q", required)
		}
	}
	for _, literal := range []string{
		"mcp-edge lifecycle inspect",
		"prepare-state-migration",
		"rollback-state-migration",
		"RENAME_NOREPLACE",
		"RENAME_EXCHANGE",
		"recovered_complete",
		"postinst.in",
	} {
		if !strings.Contains(migration, literal) {
			t.Errorf("P16 migration documentation missing literal %q", literal)
		}
	}

	installation := contents["installation"]
	for _, required := range []string{
		"at most these two local commands",
		"forced service-health failure restores legacy state byte for byte",
		"must not automatically delete user repositories",
		"validation pending",
	} {
		if !containsNormalizedProse(installation, required) {
			t.Errorf("P16 installation documentation missing %q", required)
		}
	}
	for _, literal := range []string{"pairing=reused", "mcp-edge doctor --repair"} {
		if !strings.Contains(installation, literal) {
			t.Errorf("P16 installation documentation missing literal %q", literal)
		}
	}

	docMap := read("documentation-map.md")
	for _, required := range []string{
		"specs/007-global-work-scheduler",
		"0004-p16-global-scheduler-separated-execution-pools.md",
		"2026-07-22-p16-capacity.md",
		"edge-lifecycle-migration.md",
		"install-edge-parrot-p16.md",
	} {
		if !strings.Contains(docMap, required) {
			t.Errorf("documentation map missing %q", required)
		}
	}
}
