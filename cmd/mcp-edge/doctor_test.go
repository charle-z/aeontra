//go:build !windows

package main

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os/user"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/edgelifecycle"
)

func TestDoctorReportsReadyWithoutOpaqueIdentifiers(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	stubHealthyDoctor(t)

	var stdout bytes.Buffer
	if err := doctorCommand(nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := "edge_doctor status=ready bundle=valid layout=valid identity=valid alias=parrot service=active rootless=podman\n"
	if stdout.String() != want {
		t.Fatalf("output=%q want=%q", stdout.String(), want)
	}
	for _, opaque := range []string{"ed_", "ws_", "mr_"} {
		if strings.Contains(stdout.String(), opaque) {
			t.Fatalf("doctor exposed opaque identifier %q", opaque)
		}
	}
}

func TestDoctorReportsSetupRequiredWithoutPairingOrRepair(t *testing.T) {
	restoreDoctorHooks(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	doctorVerifyBundle = func(string) error { return nil }
	doctorInspectLayout = func(edgelifecycle.InventoryConfig) (edgelifecycle.LayoutReport, error) {
		return edgelifecycle.LayoutReport{}, nil
	}
	doctorCurrentUser = func() (*user.User, error) { return &user.User{Username: "charles", Uid: "1000"}, nil }
	doctorDiscoverRootless = func(int) (*edgeclient.RootlessContainerEndpoint, error) { return nil, nil }
	doctorLoadIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, errors.New("missing")
	}
	doctorStartRepair = func() error {
		t.Fatal("plain doctor started repair")
		return nil
	}

	var stdout bytes.Buffer
	if err := doctorCommand(nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "edge_doctor status=setup_required bundle=valid layout=valid identity=missing service=inactive rootless=missing\n" {
		t.Fatalf("output=%q", stdout.String())
	}
}

func TestDoctorReturnsDegradedWhenServiceIsInactive(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	stubHealthyDoctor(t)
	doctorServiceActive = func(string) bool { return false }

	var stdout bytes.Buffer
	err := doctorCommand(nil, &stdout, &bytes.Buffer{})
	if err == nil || err.Error() != "edge doctor found a degraded installation" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(stdout.String(), "status=degraded") || !strings.Contains(stdout.String(), "service=inactive") {
		t.Fatalf("output=%q", stdout.String())
	}
}

func TestDoctorBlocksUnsafeLayout(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	doctorVerifyBundle = func(string) error { return nil }
	doctorInspectLayout = func(edgelifecycle.InventoryConfig) (edgelifecycle.LayoutReport, error) {
		return edgelifecycle.LayoutReport{Blockers: []edgelifecycle.Blocker{{Code: edgelifecycle.BlockerPreferredStateSymlink}}}, nil
	}
	var stdout bytes.Buffer
	err := doctorCommand(nil, &stdout, &bytes.Buffer{})
	if err == nil || err.Error() != "edge doctor found an unsafe layout" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(stdout.String(), "status=blocked") || strings.Contains(stdout.String(), "/home/") {
		t.Fatalf("output=%q", stdout.String())
	}
}

func TestDoctorRepairUsesClosedMigrationAndFixedRepairService(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	stubHealthyDoctor(t)
	sequence := []string{}
	doctorRecoverMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationResult, error) {
		sequence = append(sequence, "recover")
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusNotNeeded}, nil
	}
	doctorPlanMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationPlan, error) {
		sequence = append(sequence, "plan")
		return edgelifecycle.StateMigrationPlan{Version: edgelifecycle.StateMigrationVersion, Needed: true, Kind: edgelifecycle.MigrationLegacyToPreferred}, nil
	}
	doctorApplyMigration = func(_ edgelifecycle.StateMigrationConfig, _ edgelifecycle.StateMigrationPlan, hooks edgelifecycle.MigrationHooks) (edgelifecycle.StateMigrationResult, error) {
		sequence = append(sequence, "apply")
		if hooks.RetainVerifiedJournal {
			t.Fatal("doctor repair unexpectedly retained a package transaction journal")
		}
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusMigrated}, nil
	}
	doctorStartRepair = func() error {
		sequence = append(sequence, "fixed-service")
		return nil
	}

	var stdout bytes.Buffer
	if err := doctorCommand([]string{"--repair"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sequence, ",") != "recover,plan,apply,fixed-service" {
		t.Fatalf("sequence=%v", sequence)
	}
	if !strings.Contains(stdout.String(), "status=ready") {
		t.Fatalf("output=%q", stdout.String())
	}
}

func TestDoctorRejectsAllInputsExceptRepairFlag(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--repair", "extra"}, {"--unknown"}} {
		if err := doctorCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("doctor accepted args %v", args)
		}
	}
}

func stubHealthyDoctor(t *testing.T) {
	t.Helper()
	doctorVerifyBundle = func(string) error { return nil }
	doctorInspectLayout = func(edgelifecycle.InventoryConfig) (edgelifecycle.LayoutReport, error) {
		return edgelifecycle.LayoutReport{}, nil
	}
	doctorCurrentUser = func() (*user.User, error) { return &user.User{Username: "charles", Uid: "1000"}, nil }
	doctorDiscoverRootless = func(uid int) (*edgeclient.RootlessContainerEndpoint, error) {
		if uid != 1000 {
			t.Fatalf("uid=%d", uid)
		}
		return &edgeclient.RootlessContainerEndpoint{Engine: "podman"}, nil
	}
	doctorLoadIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{Name: "parrot", DeviceID: "ed_0123456789abcdef0123456789abcdef"}, nil, nil
	}
	doctorServiceActive = func(service string) bool {
		if service != "mcp-devbox-opencode-edge@charles.service" {
			t.Fatalf("service=%q", service)
		}
		return true
	}
}

func restoreDoctorHooks(t *testing.T) {
	t.Helper()
	oldVerify := doctorVerifyBundle
	oldInspect := doctorInspectLayout
	oldLoad := doctorLoadIdentity
	oldUser := doctorCurrentUser
	oldRootless := doctorDiscoverRootless
	oldActive := doctorServiceActive
	oldStart := doctorStartRepair
	oldRecover := doctorRecoverMigration
	oldPlan := doctorPlanMigration
	oldApply := doctorApplyMigration
	t.Cleanup(func() {
		doctorVerifyBundle = oldVerify
		doctorInspectLayout = oldInspect
		doctorLoadIdentity = oldLoad
		doctorCurrentUser = oldUser
		doctorDiscoverRootless = oldRootless
		doctorServiceActive = oldActive
		doctorStartRepair = oldStart
		doctorRecoverMigration = oldRecover
		doctorPlanMigration = oldPlan
		doctorApplyMigration = oldApply
	})
}
