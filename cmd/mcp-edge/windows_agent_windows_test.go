//go:build windows

package main

import (
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestWindowsAgentDispatchesServiceBeforeWorkcellPreflight(t *testing.T) {
	originalIsService := windowsAgentIsService
	originalRunService := windowsAgentRunService
	t.Cleanup(func() {
		windowsAgentIsService = originalIsService
		windowsAgentRunService = originalRunService
	})

	windowsAgentIsService = func() (bool, error) { return true, nil }
	dispatched := errors.New("service dispatcher reached")
	windowsAgentRunService = func(config windowsAgentConfig) error {
		if config.workspaceRoot != `Z:\unsafe-missing-root` {
			t.Fatalf("workspace root = %q", config.workspaceRoot)
		}
		if config.serviceIdentity != `NT SERVICE\MissingAeontraEdge` {
			t.Fatalf("service identity = %q", config.serviceIdentity)
		}
		return dispatched
	}

	err := runWindowsAgent([]string{
		"--root", `Z:\unsafe-missing-root`,
		"--service-identity", `NT SERVICE\MissingAeontraEdge`,
	}, nil, io.Discard, io.Discard)
	if !errors.Is(err, dispatched) {
		t.Fatalf("runWindowsAgent() error = %v, want dispatcher sentinel", err)
	}
}

func TestWindowsAgentServiceReportsStartPendingBeforePreflight(t *testing.T) {
	originalEnsureWorkcell := windowsAgentEnsureWorkcellUser
	t.Cleanup(func() { windowsAgentEnsureWorkcellUser = originalEnsureWorkcell })
	windowsAgentEnsureWorkcellUser = func() error {
		t.Fatal("token preflight ran for a substituted service identity")
		return nil
	}
	statuses := make(chan svc.Status, 3)
	service := windowsAgentService{config: windowsAgentConfig{
		serviceIdentity: `NT SERVICE\MissingAeontraEdge`,
		workspaceRoot:   `Z:\unsafe-missing-root`,
	}}

	serviceSpecific, exitCode := service.Execute(nil, make(chan svc.ChangeRequest), statuses)
	if !serviceSpecific || exitCode != 1 {
		t.Fatalf("Execute() = (%t, %d), want service-specific failure 1", serviceSpecific, exitCode)
	}
	select {
	case status := <-statuses:
		if status.State != svc.StartPending {
			t.Fatalf("first status = %v, want StartPending", status.State)
		}
	default:
		t.Fatal("Execute() did not report StartPending before preflight")
	}
}

func TestWindowsAgentServiceUsesFixedIdentityAuthorityInsteadOfInteractiveElevation(t *testing.T) {
	originalEnsureWorkcell := windowsAgentEnsureWorkcellUser
	originalEnsureIdentity := windowsAgentEnsureServiceIdentity
	t.Cleanup(func() {
		windowsAgentEnsureWorkcellUser = originalEnsureWorkcell
		windowsAgentEnsureServiceIdentity = originalEnsureIdentity
	})

	windowsAgentEnsureWorkcellUser = func() error {
		t.Fatal("SCM service used the interactive UAC elevation guard")
		return nil
	}
	identityChecked := false
	windowsAgentEnsureServiceIdentity = func(string) error {
		identityChecked = true
		return errors.New("identity check sentinel")
	}
	statuses := make(chan svc.Status, 3)
	service := windowsAgentService{config: windowsAgentConfig{
		serviceIdentity: windowsAgentServiceIdentity,
		workspaceRoot:   `D:\Aeontra\Workspaces`,
	}}

	serviceSpecific, exitCode := service.Execute(nil, make(chan svc.ChangeRequest), statuses)
	if !serviceSpecific || exitCode != 1 {
		t.Fatalf("Execute() = (%t, %d), want service-specific failure 1", serviceSpecific, exitCode)
	}
	if !identityChecked {
		t.Fatal("fixed service identity authority was not checked")
	}
	if status := <-statuses; status.State != svc.StartPending {
		t.Fatalf("first status = %v, want StartPending", status.State)
	}
}

func TestValidateWindowsServiceIdentityRejectsWrongOrAdministrativeToken(t *testing.T) {
	expected, err := windows.StringToSid("S-1-5-80-100")
	if err != nil {
		t.Fatal(err)
	}
	other, err := windows.StringToSid("S-1-5-80-200")
	if err != nil {
		t.Fatal(err)
	}

	if err := validateWindowsServiceIdentity(expected, expected, false); err != nil {
		t.Fatalf("matching non-administrative identity rejected: %v", err)
	}
	if err := validateWindowsServiceIdentity(other, expected, false); err == nil {
		t.Fatal("different service identity accepted")
	}
	if err := validateWindowsServiceIdentity(expected, expected, true); err == nil {
		t.Fatal("administrative service identity accepted")
	}
}

func TestWindowsPairRequestIsIdempotentAfterPairing(t *testing.T) {
	originalLoadIdentity := windowsAgentLoadIdentity
	t.Cleanup(func() { windowsAgentLoadIdentity = originalLoadIdentity })
	windowsAgentLoadIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, nil
	}

	root := t.TempDir()
	requestPath := filepath.Join(root, "pair-request.json")
	if err := os.WriteFile(requestPath, []byte(`{"server":"https://example.invalid","name":"windows","code":"ep_stale"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := consumeWindowsPairRequest(root, requestPath, nil, nil); err != nil {
		t.Fatalf("consumeWindowsPairRequest(existing identity) error = %v", err)
	}
	if _, err := os.Lstat(requestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale request remains after cleanup: %v", err)
	}
	if err := consumeWindowsPairRequest(root, requestPath, nil, nil); err != nil {
		t.Fatalf("consumeWindowsPairRequest(restart) error = %v", err)
	}
}
