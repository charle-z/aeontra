package debian

import (
	"strings"
	"testing"
)

func TestP16PackageWorkflowProvesRollbackAndIdempotentMigration(t *testing.T) {
	workflow := repoFile(t, ".github/workflows/p15-edge.yml")
	for _, required := range []string{
		"edge-state-fixture",
		"fail-edge-service",
		"cmp /fixtures/legacy-state/identity.json /home/charles/.config/mcp-devbox-edge/identity.json",
		"cmp /fixtures/legacy-state/identity.json /home/charles/.local/state/mcp-edge/identity.json",
		".mcp-edge-state-migration-v1.json",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("P16 package workflow missing rollback/idempotency evidence %q", required)
		}
	}
}

func TestP16DoctorUsesOnlyClosedRepairAuthority(t *testing.T) {
	doctor := repoFile(t, "cmd/mcp-edge/doctor_unix.go")
	for _, required := range []string{
		"mcp-devbox-edge-repair.service",
		"doctorRecoverMigration",
		"doctorPlanMigration",
		"doctorApplyMigration",
		"alias=%s",
	} {
		if !strings.Contains(doctor, required) {
			t.Errorf("doctor missing closed repair contract %q", required)
		}
	}
	if strings.Contains(doctor, "exec.Command(args") || strings.Contains(doctor, "--command") {
		t.Fatal("doctor exposes free command execution")
	}
}
