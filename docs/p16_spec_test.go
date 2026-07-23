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
		"650 millicores",
		"cgroups/rootless controls enforce",
		"Redis is a non-goal",
		"build@vps or build@edge",
		"new chat can resolve and continue a registered project by alias",
	} {
		if !strings.Contains(strings.ToLower(spec), strings.ToLower(required)) {
			t.Errorf("P16 spec missing %q", required)
		}
	}

	threat := contents["threat model"]
	for _, required := range []string{
		"silent target fallback",
		"rootful Docker socket",
		"unknown `p12` directory",
		"duplicate execution after disconnect",
		"Optimization equations never grant or expand authority",
	} {
		if !strings.Contains(strings.ToLower(threat), strings.ToLower(required)) {
			t.Errorf("P16 threat model missing %q", required)
		}
	}

	plan := contents["plan"]
	for _, required := range []string{
		"Step 1 — P12/P15 inventory and migration model",
		"Step 2 — One-command package lifecycle and guided onboarding",
		"Step 7 — Rootless VPS builder spike and selection",
		"50%, 65%, 80%",
		"rollback",
	} {
		if !strings.Contains(strings.ToLower(plan), strings.ToLower(required)) {
			t.Errorf("P16 plan missing %q", required)
		}
	}

	tasks := contents["tasks"]
	for _, required := range []string{
		"directory named `p12`",
		"Fresh chat resolves/continues/builds a real project with alias + target only",
		"Twenty simultaneous requests yield exactly one active build",
		"No Edge-to-VPS fallback",
	} {
		if !strings.Contains(strings.ToLower(tasks), strings.ToLower(required)) {
			t.Errorf("P16 tasks missing %q", required)
		}
	}

	baseline := contents["baseline"]
	for _, required := range []string{
		"2 vCPU",
		"3.805 GiB",
		"p95 host busy",
		"100.00%",
		"CPU is the demonstrated binding resource",
		"second dashboard peak",
		"50/65/80",
	} {
		if !strings.Contains(strings.ToLower(baseline), strings.ToLower(required)) {
			t.Errorf("P16 capacity baseline missing %q", required)
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
		if !strings.Contains(strings.ToLower(adr), strings.ToLower(required)) {
			t.Errorf("P16 ADR missing %q", required)
		}
	}

	migration := contents["migration"]
	for _, required := range []string{
		"mcp-edge lifecycle inspect",
		"prepare-state-migration",
		"rollback-state-migration",
		"RENAME_NOREPLACE",
		"RENAME_EXCHANGE",
		"recovered_complete",
		"unknown content is never moved",
		"postinst.in` no longer performs a direct shell move",
	} {
		if !strings.Contains(strings.ToLower(migration), strings.ToLower(required)) {
			t.Errorf("P16 migration documentation missing %q", required)
		}
	}

	installation := contents["installation"]
	for _, required := range []string{
		"at most these two local commands",
		"pairing=reused",
		"mcp-edge doctor --repair",
		"forced service-health failure restores legacy state byte for byte",
		"must not automatically delete user repositories",
		"validation pending",
	} {
		if !strings.Contains(strings.ToLower(installation), strings.ToLower(required)) {
			t.Errorf("P16 installation documentation missing %q", required)
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
