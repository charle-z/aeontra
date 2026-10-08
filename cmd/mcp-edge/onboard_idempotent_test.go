//go:build !windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
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

	var stdout, stderr bytes.Buffer
	if err := onboard([]string{"--state", state}, onboardingUnreadInput{t}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "onboarding complete alias=parrot service=active bundle=valid pairing=reused\n" {
		t.Fatalf("output=%q", stdout.String())
	}
	if strings.Contains(stdout.String(), "ed_") {
		t.Fatalf("opaque device id leaked: %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "Pairing code") || !strings.Contains(stderr.String(), "Next: run mcp-edge doctor") {
		t.Fatalf("reuse guidance=%q", stderr.String())
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

func TestOnboardFreshStatePromptsOnceWithoutEchoingPipedCode(t *testing.T) {
	restoreOnboardingHooks(t)
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{}, nil, errors.New("missing")
	}
	pairOnboardingIdentity = func(_ context.Context, opts edgeclient.PairOptions) (edgeclient.Identity, error) {
		if opts.Code != "ep_private-test-code" || opts.ServerURL != "https://mcp.example.com" {
			t.Fatalf("unexpected pairing options")
		}
		return edgeclient.Identity{Name: "parrot"}, nil
	}
	currentOnboardingUser = func() (*user.User, error) { return &user.User{Username: "charles"}, nil }
	waitOnboardingService = func(string, time.Duration) error { return nil }
	var stdout, stderr bytes.Buffer
	if err := onboard([]string{"--server", "https://mcp.example.com/", "--state", t.TempDir()}, strings.NewReader("ep_private-test-code\n"), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stderr.String(), "Pairing code (stdin): ") != 1 || !strings.Contains(stderr.String(), "Next: run mcp-edge doctor") {
		t.Fatalf("guidance=%q", stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "ep_private-test-code") {
		t.Fatal("pairing code echoed")
	}
}

func TestOnboardFreshStateValidatesServerBeforeReadingPairingCode(t *testing.T) {
	for _, server := range []string{"", "http://mcp.example.com", "https://user:secret@mcp.example.com", "https://mcp.example.com/path"} {
		t.Run(server, func(t *testing.T) {
			restoreOnboardingHooks(t)
			verifyOnboardingBundle = func(string) error { return nil }
			runOnboardingPreflight = func() error { return nil }
			loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
				return edgeclient.Identity{}, nil, errors.New("missing")
			}
			pairOnboardingIdentity = func(context.Context, edgeclient.PairOptions) (edgeclient.Identity, error) {
				t.Fatal("invalid server reached pairing endpoint")
				return edgeclient.Identity{}, nil
			}
			var stdout, stderr bytes.Buffer
			err := onboard([]string{"--server", server, "--state", t.TempDir()}, onboardingUnreadInput{t}, &stdout, &stderr)
			if err == nil || err.Error() != "edge server must be an HTTPS origin" {
				t.Fatalf("err=%v", err)
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "Pairing code") {
				t.Fatalf("premature output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

type onboardingUnreadInput struct{ t *testing.T }

func (input onboardingUnreadInput) Read([]byte) (int, error) {
	input.t.Fatal("onboarding unexpectedly read stdin")
	return 0, io.EOF
}

func TestOnboardServiceFailurePreservesIdentityAndProvidesBoundedGuidance(t *testing.T) {
	restoreOnboardingHooks(t)
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return nil }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		return edgeclient.Identity{Name: "parrot"}, nil, nil
	}
	currentOnboardingUser = func() (*user.User, error) { return &user.User{Username: "charles"}, nil }
	waitOnboardingService = func(string, time.Duration) error { return errors.New("private service detail") }
	var stdout bytes.Buffer
	err := onboard([]string{"--state", t.TempDir()}, onboardingUnreadInput{t}, &stdout, &bytes.Buffer{})
	if err == nil || err.Error() != "onboarding service did not become active; identity preserved; run mcp-edge doctor, then rerun mcp-edge onboard" {
		t.Fatalf("err=%v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure reported completion: %q", stdout.String())
	}
}

func TestOnboardPreflightFailureDoesNotReadCodeOrPair(t *testing.T) {
	restoreOnboardingHooks(t)
	verifyOnboardingBundle = func(string) error { return nil }
	runOnboardingPreflight = func() error { return errors.New("private preflight detail") }
	loadOnboardingIdentity = func(string) (edgeclient.Identity, ed25519.PrivateKey, error) {
		t.Fatal("failed preflight reached identity resolution")
		return edgeclient.Identity{}, nil, nil
	}
	var stdout, stderr bytes.Buffer
	err := onboard([]string{"--server", "https://mcp.example.com", "--state", t.TempDir()}, onboardingUnreadInput{t}, &stdout, &stderr)
	if err == nil || err.Error() != "onboarding preflight failed; run mcp-edge doctor for diagnosis" {
		t.Fatalf("err=%v", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("failure reported pairing/completion: stdout=%q stderr=%q", stdout.String(), stderr.String())
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
