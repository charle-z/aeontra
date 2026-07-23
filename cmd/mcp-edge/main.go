package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func main() {
	if handled, err := runAskpassIfRequested(os.Stdout); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "pair":
		err = pair(args[1:], stdin, stdout, stderr)
	case "onboard":
		err = onboard(args[1:], stdin, stdout, stderr)
	case "run":
		err = runWorkcell(args[1:], stderr)
	case "opencode":
		err = runOpenCodeRelay(args[1:], stderr)
	case "workspace":
		err = workspaceCommand(args[1:], stdout, stderr)
	case "project":
		err = projectCommand(args[1:], stdout, stderr)
	case "lab":
		err = labCommand(args[1:], stdout, stderr)
	case "bundle":
		err = bundleCommand(args[1:], stdout)
	case "github":
		err = githubCommand(args[1:], stdin, stdout, stderr)
	case "lifecycle":
		err = lifecycleCommand(args[1:], stdout, stderr)
	case "doctor":
		err = doctorCommand(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintln(stderr, "unknown command: "+args[0])
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "mcp-edge: "+err.Error())
		return 1
	}
	return 0
}

func pair(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "HTTPS mcp-devbox origin")
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	name := fs.String("name", "wsl-development", "device name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	code, err := readPairingCode(stdin)
	if err != nil {
		return err
	}
	identity, err := edgeclient.Pair(context.Background(), edgeclient.PairOptions{ServerURL: *server, Code: code, Name: *name, StateRoot: *state})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "paired "+identity.DeviceID)
	return nil
}

func runWorkcell(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	root := fs.String("root", "", "dedicated Linux workspace root")
	poll := fs.Duration("poll", 5*time.Second, "empty queue polling interval")
	leaseTTL := fs.Duration("lease", time.Minute, "task lease duration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *poll <= 0 || *leaseTTL < 15*time.Second || *leaseTTL > 10*time.Minute {
		return errors.New("poll must be positive and lease must be between 15s and 10m")
	}
	if err := ensureWorkcellUser(); err != nil {
		return err
	}
	validatedRoot, err := edgeclient.ValidateWorkcellRoot(*root)
	if err != nil {
		return err
	}
	if pathsOverlap(validatedRoot, *state) {
		return errors.New("workcell and private state roots must not overlap")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return errors.New("bubblewrap is required for the development workcell")
	}
	transport, err := edgeclient.NewTransport(*state, nil)
	if err != nil {
		return err
	}
	journal, err := edgeclient.OpenJournal(*state)
	if err != nil {
		return err
	}
	defer journal.Close()
	identity, _, err := edgeclient.LoadIdentity(*state)
	if err != nil {
		return err
	}
	opaqueDeviceID := strings.TrimPrefix(identity.DeviceID, "ed_")
	if len(opaqueDeviceID) < 16 {
		return errors.New("paired device identity is invalid")
	}
	holder := "agent-" + opaqueDeviceID[:16]
	heartbeatInterval := *leaseTTL / 3
	if heartbeatInterval > 5*time.Second {
		heartbeatInterval = 5 * time.Second
	}
	runner := edgeclient.Runner{
		Transport:         transport,
		Journal:           journal,
		Executor:          &edgeclient.Workcell{Root: validatedRoot},
		Holder:            holder,
		LeaseTTL:          *leaseTTL,
		HeartbeatInterval: heartbeatInterval,
		StopPath:          filepath.Join(*state, "STOP"),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		worked, err := runner.RunOnce(ctx)
		if errors.Is(err, edgeclient.ErrKillSwitch) || errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		if err != nil {
			fmt.Fprintln(stderr, "mcp-edge: task cycle failed safely")
		}
		delay := *poll
		if worked {
			delay = 250 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}

func readPairingCode(input io.Reader) (string, error) {
	reader := bufio.NewReader(io.LimitReader(input, 256))
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.New("pairing code unavailable")
	}
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "ep_") || len(value) > 128 {
		return "", errors.New("valid pairing code required on stdin")
	}
	return value, nil
}

func defaultStateRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	stateBase := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateBase == "" || !filepath.IsAbs(stateBase) {
		stateBase = filepath.Join(home, ".local", "state")
	}
	preferred := filepath.Join(stateBase, "mcp-edge")
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	if _, err := os.Stat(filepath.Join(preferred, "identity.json")); err == nil {
		return preferred
	}
	if _, err := os.Stat(filepath.Join(legacy, "identity.json")); err == nil {
		return legacy
	}
	return preferred
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return contains(left, right) || contains(right, left)
}

func contains(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))))
}

func usage(output io.Writer) {
	fmt.Fprint(output, `mcp-edge — outbound-only mcp-devbox development workcell

Usage:
  mcp-edge pair --server https://mcp.example.com [--state <ABS_PATH>] [--name wsl-development]
  mcp-edge onboard --server https://mcp.example.com [--state <ABS_PATH>] [--name parrot-edge]
  mcp-edge lifecycle inspect
  mcp-edge lifecycle migrate-state
  mcp-edge lifecycle recover-state
  mcp-edge lifecycle prepare-state-migration
  mcp-edge lifecycle finalize-state-migration
  mcp-edge lifecycle rollback-state-migration
  mcp-edge doctor [--repair]
  mcp-edge project status --alias <PROJECT> [--target <ALIAS>]
  mcp-edge project resolve --alias <PROJECT> [--target <ALIAS>]
  mcp-edge run --root <ABS_LINUX_PATH> [--state <ABS_PATH>] [--poll 5s] [--lease 1m]
  mcp-edge opencode --opencode <ABS_PATH> --provider <ABS_PATH> --integrity <ABS_PATH> [--bubblewrap <ABS_PATH>] [--state <ABS_PATH>]
  mcp-edge workspace add [--profile sandbox|linux-workcell] <ABS_LINUX_PATH> [--state <ABS_PATH>]
  mcp-edge workspace configure <OPAQUE_ID> --mode dev|htb-linux [local metadata] [--state <ABS_PATH>]
  mcp-edge workspace inventory <OPAQUE_ID> [--state <ABS_PATH>]
  mcp-edge workspace list [--state <ABS_PATH>]
  mcp-edge workspace remove --id <OPAQUE_ID> [--state <ABS_PATH>]
  mcp-edge lab init --platform htb --machine <NAME> --target <IP> --difficulty EASY|MEDIUM|HARD [--vpn-interface tun0] [--state <ABS_PATH>]
  mcp-edge lab retarget --workspace-id <OPAQUE_ID> --target <IP> [--state <ABS_PATH>]
  mcp-edge lab ssh-exec --username <USER> --source <FILE> --extract-after <PREFIX> --command <COMMAND> [--save-output <FILE>]
  mcp-edge bundle verify
  mcp-edge github configure --owner <GITHUB_OWNER> [--state <ABS_PATH>]  # token on stdin
  mcp-edge github status [--state <ABS_PATH>]

The pairing code is read from stdin and is never accepted as a command-line flag.
Create the local STOP file to activate the kill switch.
`)
}
