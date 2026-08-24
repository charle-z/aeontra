//go:build !windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestOnboardReusesValidIdentityWithoutPairingCodeOrDeviceIDOutput(t *testing.T) {
	restoreOnboardingHooks(t)
	state := t.TempDir()
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{ServerURL: "https://mcp.example.com", DeviceID: "ed_0123456789abcdef0123456789abcdef", Name: "parrot"}, nil, nil
	}
	pairOnboardingIdentity = func(context.Context, edgeclient.PairOptions) (edgeclient.Identity, error) {
		t.Fatal("valid identity attempted to pair again")
		return edgeclient.Identity{}, nil
	}
	currentOnboardingUser = func() (*user.User, error) { return &user.User{Username: "charles"}, nil }
	expectedService := edgeServiceName("charles")
	waitOnboardingService = func(service string, timeout time.Duration) error {
		if service != expectedService || timeout != 30*time.Second {
			t.Fatalf("service=%q timeout=%s", service, timeout)
		}
		return nil
	}

	var stdout bytes.Buffer
	if err := onboard([]string{"--state", state}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "onboarding complete alias=parrot service=active bundle=valid pairing=reused\n" {
		t.Fatalf("output=%q", stdout.String())
	}
	if strings.Contains(stdout.String(), "ed_") {
		t.Fatalf("opaque device id leaked: %s", stdout.String())
	}
}

func TestOnboardExistingIdentityRejectsDifferentServer(t *testing.T) {
	restoreOnboardingHooks(t)
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{ServerURL: "https://one.example.com", Name: "parrot"}, nil, nil
	}
	pairOnboardingIdentity = func(context.Context, edgeclient.PairOptions) (edgeclient.Identity, error) {
		t.Fatal("mismatched existing identity attempted pairing")
		return edgeclient.Identity{}, nil
	}
	if err := onboard([]string{"--server", "https://two.example.com", "--state", t.TempDir()}, strings.NewReader("ignored"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil || err.Error() != "existing Edge identity belongs to a different server" {
		t.Fatalf("err=%v", err)
	}
}

func TestOnboardInvalidExistingIdentityDoesNotOverwriteOrRepairSilently(t *testing.T) {
	restoreOnboardingHooks(t)
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "identity.json"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, errors.New("invalid")
	}
	pairOnboardingIdentity = func(context.Context, edgeclient.PairOptions) (edgeclient.Identity, error) {
		t.Fatal("invalid existing identity was overwritten")
		return edgeclient.Identity{}, nil
	}
	if err := onboard([]string{"--server", "https://mcp.example.com", "--state", state}, strings.NewReader("pair-code"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil || err.Error() != "existing Edge identity is invalid" {
		t.Fatalf("err=%v", err)
	}
	content, err := os.ReadFile(filepath.Join(state, "identity.json"))
	if err != nil || string(content) != "invalid" {
		t.Fatalf("identity changed: content=%q err=%v", content, err)
	}
}

func TestOnboardFreshStatePairsOnceAndReturnsAlias(t *testing.T) {
	restoreOnboardingHooks(t)
	state := filepath.Join(t.TempDir(), "state")
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, errors.New("missing")
	}
	calls := 0
	pairOnboardingIdentity = func(_ context.Context, opts edgeclient.PairOptions) (edgeclient.Identity, error) {
		calls++
		if opts.ServerURL != "https://mcp.example.com" || opts.Code != "ep_test" || opts.Name != "parrot" || opts.StateRoot != state {
			t.Fatalf("pair options=%+v", opts)
		}
		return edgeclient.Identity{Name: "parrot", DeviceID: "ed_0123456789abcdef0123456789abcdef", ServerURL: "https://mcp.example.com"}, nil
	}
	currentOnboardingUser = func() (*user.User, error) { return &user.User{Username: "charles"}, nil }
	waitOnboardingService = func(string, time.Duration) error { return nil }

	var stdout bytes.Buffer
	if err := onboard([]string{"--server", "https://mcp.example.com", "--state", state, "--name", "parrot"}, strings.NewReader("ep_test\n"), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || stdout.String() != "onboarding complete alias=parrot service=active bundle=valid pairing=created\n" {
		t.Fatalf("calls=%d output=%q", calls, stdout.String())
	}
}

func TestOnboardFreshStateStillRequiresServerAndPairingCode(t *testing.T) {
	restoreOnboardingHooks(t)
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, errors.New("missing")
	}
	if err := onboard([]string{"--state", filepath.Join(t.TempDir(), "state")}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("fresh onboarding accepted without pairing code")
	}
}

func restoreOnboardingHooks(t *testing.T) {
	t.Helper()
	oldVerify := verifyOnboardingBundle
	oldLoad := loadOnboardingIdentity
	oldPair := pairOnboardingIdentity
	oldUser := currentOnboardingUser
	oldPreflight := runOnboardingPreflight
	oldWait := waitOnboardingService
	t.Cleanup(func() {
		verifyOnboardingBundle = oldVerify
		loadOnboardingIdentity = oldLoad
		pairOnboardingIdentity = oldPair
		currentOnboardingUser = oldUser
		runOnboardingPreflight = oldPreflight
		waitOnboardingService = oldWait
	})
}
