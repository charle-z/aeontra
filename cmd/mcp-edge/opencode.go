//go:build !windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func runOpenCodeRelay(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("opencode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	socketRoot := fs.String("socket-root", "", "private local Unix-socket root")
	opencodePath := fs.String("opencode", "", "absolute path to pinned OpenCode 1.18.1")
	providerPath := fs.String("provider", "", "absolute path to the local external-driver provider")
	integrityPath := fs.String("integrity", "", "absolute path to the pinned npm package-lock.json")
	wait := fs.Duration("wait", 120*time.Second, "signed runtime long-poll duration")
	poll := fs.Duration("poll", 5*time.Second, "delay after an empty long-poll or safe failure")
	heartbeat := fs.Duration("heartbeat", 5*time.Second, "runtime heartbeat interval")
	outputLimit := fs.Int64("output-limit", 1<<20, "maximum transient bytes per OpenCode output stream")
	once := fs.Bool("once", false, "process at most one runtime lease")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("opencode relay does not accept positional arguments")
	}
	if os.Geteuid() == 0 {
		return errors.New("OpenCode relay refuses to run as root")
	}
	if *wait < time.Second || *wait > 180*time.Second || *poll < time.Second || *poll > time.Minute || *heartbeat < time.Second || *heartbeat > 30*time.Second {
		return errors.New("OpenCode relay timing is outside the safe bounds")
	}
	for name, value := range map[string]string{"opencode": *opencodePath, "provider": *providerPath, "integrity": *integrityPath} {
		if !filepath.IsAbs(filepath.Clean(value)) {
			return fmt.Errorf("%s path must be absolute", name)
		}
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	journal, err := edgeclient.OpenOpenCodeRuntimeJournal(*state)
	if err != nil {
		return err
	}
	defer journal.Close()
	launcher, err := edgeclient.NewOpenCodeLauncher(edgeclient.OpenCodeLauncherConfig{
		StateRoot: *state, SocketRoot: *socketRoot, OpenCodePath: *opencodePath, ProviderPath: *providerPath,
		IntegrityPath: *integrityPath, StopPath: filepath.Join(*state, "STOP"), OutputLimit: *outputLimit,
		Heartbeat: *heartbeat, Workspaces: registry, Journal: journal,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		worked, result, runErr := launcher.RunNext(ctx, *wait)
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(runErr, edgeclient.ErrKillSwitch) {
			return nil
		}
		if runErr != nil {
			fmt.Fprintf(stderr, "mcp-edge: OpenCode runtime failed safely runtime=%s state=%s\n", result.RuntimeID, result.State)
		}
		if *once {
			return runErr
		}
		delay := *poll
		if worked && runErr == nil {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}
