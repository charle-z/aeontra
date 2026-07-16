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
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func openCodeFailureCode(err error) string {
	if err == nil {
		return "none"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, edgeclient.ErrKillSwitch):
		return "kill_switch"
	case errors.Is(err, edgeclient.ErrOpenCodeInterrupted):
		return "restart_interrupted"
	case errors.Is(err, edgeclient.ErrOpenCodeTerminal):
		return "terminal_replay"
	}
	message := strings.ToLower(err.Error())
	for _, item := range []struct{ contains, code string }{
		{"integrity", "installation_integrity"},
		{"version", "installation_version"},
		{"pinned opencode executable", "installation_opencode"},
		{"external driver", "installation_provider"},
		{"driver executable", "installation_driver"},
		{"workspace", "workspace"},
		{"socket", "socket"},
		{"journal", "journal"},
		{"lease", "lease"},
		{"model-turn driver terminated", "driver_exit"},
		{"opencode terminated unexpectedly (cli)", "opencode_cli"},
		{"opencode terminated unexpectedly (provider_load)", "opencode_provider_load"},
		{"opencode terminated unexpectedly (driver_connect)", "opencode_driver_connect"},
		{"opencode terminated unexpectedly (permission_ptrace)", "opencode_permission_ptrace"},
		{"opencode terminated unexpectedly (permission_connect)", "opencode_permission_connect"},
		{"opencode terminated unexpectedly (permission_spawn)", "opencode_permission_spawn"},
		{"opencode terminated unexpectedly (permission_mkdir)", "opencode_permission_mkdir"},
		{"opencode terminated unexpectedly (permission_open)", "opencode_permission_open"},
		{"opencode terminated unexpectedly (permission_rename)", "opencode_permission_rename"},
		{"opencode terminated unexpectedly (permission_remove)", "opencode_permission_remove"},
		{"opencode terminated unexpectedly (permission_chmod)", "opencode_permission_chmod"},
		{"opencode terminated unexpectedly (permission_read_dir)", "opencode_permission_read_dir"},
		{"opencode terminated unexpectedly (permission_stat)", "opencode_permission_stat"},
		{"opencode terminated unexpectedly (permission_write)", "opencode_permission_write"},
		{"opencode terminated unexpectedly (permission_read)", "opencode_permission_read"},
		{"opencode terminated unexpectedly (permission_other)", "opencode_permission_other"},
		{"opencode terminated unexpectedly (config)", "opencode_config"},
		{"opencode terminated unexpectedly (model)", "opencode_model"},
		{"opencode terminated unexpectedly (not_found)", "opencode_not_found"},
		{"opencode terminated unexpectedly (provider)", "opencode_provider"},
		{"opencode terminated unexpectedly (provider_auth)", "opencode_provider_auth"},
		{"opencode terminated unexpectedly (output_length)", "opencode_output_length"},
		{"opencode terminated unexpectedly (unknown_type)", "opencode_unknown_type"},
		{"opencode terminated unexpectedly (unknown_api)", "opencode_unknown_api"},
		{"opencode terminated unexpectedly (unknown_timeout)", "opencode_unknown_timeout"},
		{"opencode terminated unexpectedly (unknown_connection)", "opencode_unknown_connection"},
		{"opencode terminated unexpectedly (prompt_shape)", "opencode_prompt_shape"},
		{"opencode terminated unexpectedly (prompt_role)", "opencode_prompt_role"},
		{"opencode terminated unexpectedly (tool_shape)", "opencode_tool_shape"},
		{"opencode terminated unexpectedly (request_limit)", "opencode_request_limit"},
		{"opencode terminated unexpectedly (runtime_status)", "opencode_runtime_status"},
		{"opencode terminated unexpectedly (request_stage)", "opencode_request_stage"},
		{"opencode terminated unexpectedly (turn_create)", "opencode_turn_create"},
		{"opencode terminated unexpectedly (response_wait)", "opencode_response_wait"},
		{"opencode terminated unexpectedly (driver_invalid_request)", "opencode_driver_invalid_request"},
		{"opencode terminated unexpectedly (driver_status)", "opencode_driver_status"},
		{"opencode terminated unexpectedly (turn_identity)", "opencode_turn_identity"},
		{"opencode terminated unexpectedly (response_identity)", "opencode_response_identity"},
		{"opencode terminated unexpectedly (response_shape)", "opencode_response_shape"},
		{"opencode terminated unexpectedly (abort)", "opencode_abort"},
		{"opencode terminated unexpectedly (socket)", "opencode_socket"},
		{"opencode terminated unexpectedly (unknown_error)", "opencode_unknown_error"},
		{"opencode terminated unexpectedly (named_error)", "opencode_named_error"},
		{"opencode terminated unexpectedly (runtime_error)", "opencode_runtime_error"},
		{"opencode terminated", "opencode_exit"},
		{"edge endpoint", "relay_unavailable"},
		{"device was rejected", "relay_rejected"},
	} {
		if strings.Contains(message, item.contains) {
			return item.code
		}
	}
	return "internal"
}

func runOpenCodeRelay(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("opencode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	socketRoot := fs.String("socket-root", "", "private local Unix-socket root")
	opencodePath := fs.String("opencode", "", "absolute path to pinned OpenCode 1.18.1")
	driverPath := fs.String("driver", "", "absolute path to the isolated model-turn-driver")
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
	for name, value := range map[string]string{"opencode": *opencodePath, "driver": *driverPath, "provider": *providerPath, "integrity": *integrityPath} {
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
		StateRoot: *state, SocketRoot: *socketRoot, OpenCodePath: *opencodePath, DriverPath: *driverPath, ProviderPath: *providerPath,
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
			fmt.Fprintf(stderr, "mcp-edge: OpenCode runtime failed safely runtime=%s state=%s failure=%s\n", result.RuntimeID, result.State, openCodeFailureCode(runErr))
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
