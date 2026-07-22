//go:build !windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const onboardingPreflightPath = "/usr/local/libexec/mcp-devbox/onboarding-preflight"

var onboardingUserPattern = regexp.MustCompile("^[a-z_][a-z0-9_-]{0,31}$")

var verifyOnboardingBundle = verifyInstalledEdgeBundle
var loadOnboardingIdentity = edgeclient.LoadIdentity
var pairOnboardingIdentity = edgeclient.Pair
var currentOnboardingUser = user.Current
var runOnboardingPreflight = func() error {
	return exec.Command(onboardingPreflightPath).Run()
}
var waitOnboardingService = func(service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if exec.Command("systemctl", "is-active", "--quiet", service).Run() == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("onboarding service did not become active")
}

func onboard(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "HTTPS mcp-devbox origin; optional when reusing a valid identity")
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	name := fs.String("name", "parrot-edge", "device name used only for first pairing")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("onboard accepts only server, state and name")
	}
	if err := verifyOnboardingBundle(installedBundleRoot); err != nil {
		return err
	}
	if err := runOnboardingPreflight(); err != nil {
		return errors.New("onboarding preflight failed")
	}

	identity, pairingState, err := resolveOnboardingIdentity(*server, *name, *state, stdin)
	if err != nil {
		return err
	}
	currentUser, err := currentOnboardingUser()
	if err != nil || !onboardingUserPattern.MatchString(strings.TrimSpace(currentUser.Username)) {
		return errors.New("onboarding service identity unavailable")
	}
	service := "mcp-devbox-opencode-edge@" + currentUser.Username + ".service"
	if err := waitOnboardingService(service, 30*time.Second); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "onboarding complete alias=%s service=active bundle=valid pairing=%s\n", identity.Name, pairingState)
	return nil
}

func resolveOnboardingIdentity(server, name, state string, stdin io.Reader) (edgeclient.Identity, string, error) {
	identity, _, loadErr := loadOnboardingIdentity(state)
	if loadErr == nil {
		if strings.TrimSpace(server) != "" {
			normalized, err := edgeclient.NormalizeServerURL(server)
			if err != nil {
				return edgeclient.Identity{}, "", err
			}
			if normalized != identity.ServerURL {
				return edgeclient.Identity{}, "", errors.New("existing Edge identity belongs to a different server")
			}
		}
		return identity, "reused", nil
	}

	for _, filename := range []string{"identity.json", "device.key"} {
		_, err := os.Lstat(filepath.Join(state, filename))
		if err == nil {
			return edgeclient.Identity{}, "", errors.New("existing Edge identity is invalid")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return edgeclient.Identity{}, "", errors.New("Edge state is unavailable")
		}
	}
	code, err := readPairingCode(stdin)
	if err != nil {
		return edgeclient.Identity{}, "", err
	}
	identity, err = pairOnboardingIdentity(context.Background(), edgeclient.PairOptions{
		ServerURL: server,
		Code:      code,
		Name:      name,
		StateRoot: state,
	})
	if err != nil {
		return edgeclient.Identity{}, "", err
	}
	return identity, "created", nil
}
