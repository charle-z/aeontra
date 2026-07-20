//go:build !windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const onboardingPreflightPath = "/usr/local/libexec/mcp-devbox/onboarding-preflight"

var onboardingUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func onboard(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "HTTPS mcp-devbox origin")
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	name := fs.String("name", "parrot-edge", "device name")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("onboard accepts only server, state and name")
	}
	if err := verifyInstalledEdgeBundle(installedBundleRoot); err != nil {
		return err
	}
	preflight := exec.Command(onboardingPreflightPath)
	if err := preflight.Run(); err != nil {
		return errors.New("onboarding preflight failed")
	}
	code, err := readPairingCode(stdin)
	if err != nil {
		return err
	}
	identity, err := edgeclient.Pair(context.Background(), edgeclient.PairOptions{
		ServerURL: *server, Code: code, Name: *name, StateRoot: *state,
	})
	if err != nil {
		return err
	}
	currentUser, err := user.Current()
	if err != nil || !onboardingUserPattern.MatchString(strings.TrimSpace(currentUser.Username)) {
		return errors.New("onboarding service identity unavailable")
	}
	service := "mcp-devbox-opencode-edge@" + currentUser.Username + ".service"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("systemctl", "is-active", "--quiet", service).Run() == nil {
			fmt.Fprintf(stdout, "onboarding complete device=%s service=active bundle=valid\n", identity.DeviceID)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("onboarding service did not become active")
}
