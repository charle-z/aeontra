//go:build !windows

package main

import (
	"os"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const runtimeTestRelease = "p15.0.9"
const runtimeTestCommit = "0123456789abcdef0123456789abcdef01234567"

func TestInspectEdgeRuntimeReportsManagedSingleProcess(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	stateRoot := t.TempDir()
	lock, err := edgeclient.AcquireEdgeInstanceLock(stateRoot, runtimeTestRelease, runtimeTestCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "active", Active: true, MainPID: os.Getpid()}
	}

	report := inspectEdgeRuntime(stateRoot, "mcp-devbox-opencode-edge@charles.service")
	if !report.Healthy || !report.ServiceActive || report.ProcessState != "single" || report.LockState != "held" || report.Coherence != "managed" {
		t.Fatalf("unexpected managed report: %+v", report)
	}
	if report.ProcessRelease != runtimeTestRelease || report.ProcessCommit != runtimeTestCommit || len(report.Blockers) != 0 {
		t.Fatalf("unexpected managed identity: %+v", report)
	}
}

func TestInspectEdgeRuntimeUsesAuthoritativeInvocationWhenSystemctlIsUnavailable(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	t.Setenv("SYSTEMD_EXEC_PID", "")
	stateRoot := t.TempDir()
	lock, err := edgeclient.AcquireEdgeInstanceLock(stateRoot, runtimeTestRelease, runtimeTestCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "inactive"}
	}

	report := inspectEdgeRuntime(stateRoot, "mcp-devbox-opencode-edge@charles.service")
	if !report.Healthy || !report.ServiceActive || report.ServiceState != "active" || report.ServicePID != os.Getpid() || report.Coherence != "managed" {
		t.Fatalf("authoritative invocation was not accepted: %+v", report)
	}
}

func TestInspectEdgeRuntimeAcceptsControlledManualProcess(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	t.Setenv("INVOCATION_ID", "")
	stateRoot := t.TempDir()
	lock, err := edgeclient.AcquireEdgeInstanceLock(stateRoot, runtimeTestRelease, runtimeTestCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "inactive"}
	}

	report := inspectEdgeRuntime(stateRoot, "")
	if !report.Healthy || report.ServiceActive || report.ProcessState != "single" || report.Coherence != "manual" || report.LockState != "held" {
		t.Fatalf("manual process report: %+v", report)
	}
}

func TestInspectEdgeRuntimeDetectsDuplicateServiceProcess(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	stateRoot := t.TempDir()
	lock, err := edgeclient.AcquireEdgeInstanceLock(stateRoot, runtimeTestRelease, runtimeTestCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "active", Active: true, MainPID: os.Getpid() + 1}
	}

	report := inspectEdgeRuntime(stateRoot, "mcp-devbox-opencode-edge@charles.service")
	if report.Healthy || report.ProcessState != "duplicate" || report.Coherence != "duplicate" || !hasRuntimeBlocker(report.Blockers, "edge_process_duplicate") {
		t.Fatalf("duplicate process was not detected: %+v", report)
	}
}

func TestInspectEdgeRuntimeDetectsServiceWithoutLock(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "active", Active: true, MainPID: os.Getpid()}
	}

	report := inspectEdgeRuntime(stateRoot, "mcp-devbox-opencode-edge@charles.service")
	if report.Healthy || report.ProcessState != "incoherent" || report.Coherence != "incoherent" || !hasRuntimeBlocker(report.Blockers, "edge_service_without_lock") {
		t.Fatalf("service without lock was not detected: %+v", report)
	}
}

func TestInspectEdgeRuntimeAcceptsLegacyStateWithoutPermanentBlock(t *testing.T) {
	restoreRuntimeHealthHooks(t)
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	inspectEdgeSystemdService = func(string) edgeServiceObservation {
		return edgeServiceObservation{State: "inactive"}
	}

	report := inspectEdgeRuntime(stateRoot, "mcp-devbox-opencode-edge@charles.service")
	if report.Healthy || report.LockState != "missing" || report.ProcessState != "inactive" || report.Coherence != "stopped" || !hasRuntimeBlocker(report.Blockers, "edge_process_inactive") {
		t.Fatalf("legacy state report: %+v", report)
	}
	lock, err := edgeclient.AcquireEdgeInstanceLock(stateRoot, runtimeTestRelease, runtimeTestCommit)
	if err != nil {
		t.Fatalf("legacy state could not acquire the new lock: %v", err)
	}
	defer lock.Close()
}

func restoreRuntimeHealthHooks(t *testing.T) {
	t.Helper()
	oldInspect := inspectEdgeSystemdService
	t.Cleanup(func() { inspectEdgeSystemdService = oldInspect })
}

func hasRuntimeBlocker(blockers []string, expected string) bool {
	for _, blocker := range blockers {
		if blocker == expected {
			return true
		}
	}
	return false
}
