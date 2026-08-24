//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A driver that stays alive but never publishes its private socket reproduces the
// production stall observed on the paired device: the runtime confirmed startup in
// milliseconds, created no model turn, and only ended when the whole lease expired.
// The startup budget must bound that wait so the failure is fast and classified
// instead of silently consuming the execution budget the caller asked for.
func TestOpenCodeLauncherBoundsStalledDriverStartup(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	stalled := filepath.Join(t.TempDir(), "stalled-driver")
	if err := os.WriteFile(stalled, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.launcher.config.DriverPath = stalled
	// Assigned directly so the regression stays fast; the constructor validates the
	// supported range separately.
	fixture.launcher.config.DriverStartupBudget = 300 * time.Millisecond

	if fixture.lease.TimeoutSeconds < 5 {
		t.Fatalf("fixture lease budget is %ds, too small to prove the distinction", fixture.lease.TimeoutSeconds)
	}
	leaseBudget := time.Duration(fixture.lease.TimeoutSeconds) * time.Second

	started := time.Now()
	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	elapsed := time.Since(started)

	if !errors.Is(err, errStartupDriverNotReady) {
		t.Fatalf("err=%v, want the closed startup classification", err)
	}
	if result.State != OpenCodeLocalFailed {
		t.Fatalf("state=%q, want %q", result.State, OpenCodeLocalFailed)
	}
	if elapsed >= leaseBudget {
		t.Fatalf("stalled startup consumed %s of a %s lease; the execution budget was not preserved", elapsed, leaseBudget)
	}
}

// A driver that exits before publishing its socket must keep its own diagnosis and
// must not be reported as a startup budget expiry.
func TestOpenCodeLauncherReportsDriverExitBeforeSocket(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	failing := filepath.Join(t.TempDir(), "failing-driver")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.launcher.config.DriverPath = failing
	fixture.launcher.config.DriverStartupBudget = 5 * time.Second

	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	if err == nil {
		t.Fatal("a driver exiting before its socket must fail the lease")
	}
	if errors.Is(err, errStartupDriverNotReady) {
		t.Fatalf("driver exit misclassified as a startup budget expiry: %v", err)
	}
	if result.State != OpenCodeLocalFailed {
		t.Fatalf("state=%q, want %q", result.State, OpenCodeLocalFailed)
	}
}

// The closed classification must not leak paths, commands, stderr or model content.
func TestStartupClassificationCarriesNoPrivateDetail(t *testing.T) {
	message := errStartupDriverNotReady.Error()
	for _, forbidden := range []string{"/", "\\", "sh", "exec", "sleep", "socket "} {
		if contains(message, forbidden) {
			t.Fatalf("startup classification %q exposes %q", message, forbidden)
		}
	}
}

func contains(value, substring string) bool {
	return len(substring) > 0 && len(value) >= len(substring) && indexOf(value, substring) >= 0
}

func indexOf(value, substring string) int {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return i
		}
	}
	return -1
}

// The constructor must accept a safe default and reject values outside the
// supported range, in the same style as the existing heartbeat validation.
func TestOpenCodeLauncherValidatesDriverStartupBudget(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	base := OpenCodeLauncherConfig{
		StateRoot: fixture.state, SocketRoot: filepath.Join(fixture.state, openCodeRuntimeDirName),
		OpenCodePath: fixture.executable, ProviderPath: fixture.provider, BubblewrapPath: fixture.bubblewrap,
		IntegrityPath: fixture.lock, OutputLimit: 4096, Heartbeat: time.Second,
		Workspaces: fixture.registry, Journal: fixture.journal,
	}

	defaulted, err := NewOpenCodeLauncher(base)
	if err != nil {
		t.Fatalf("zero budget must adopt the default: %v", err)
	}
	if defaulted.config.DriverStartupBudget != openCodeDefaultDriverStartupBudget {
		t.Fatalf("budget=%s, want %s", defaulted.config.DriverStartupBudget, openCodeDefaultDriverStartupBudget)
	}

	for name, budget := range map[string]time.Duration{
		"below the supported range": openCodeMinDriverStartupBudget - time.Millisecond,
		"above the supported range": openCodeMaxDriverStartupBudget + time.Millisecond,
	} {
		invalid := base
		invalid.DriverStartupBudget = budget
		if _, err := NewOpenCodeLauncher(invalid); err == nil {
			t.Fatalf("%s was accepted: %s", name, budget)
		}
	}
}
