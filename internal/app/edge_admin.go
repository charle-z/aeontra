package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func edgeAdmin(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("edge requires pairing-create or revoke")
	}
	switch args[0] {
	case "pairing-create":
		fs := flag.NewFlagSet("edge pairing-create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		stateRoot := fs.String("state-root", "", "absolute private state root")
		ttl := fs.Duration("ttl", edge.PairingTTL, "pairing lifetime (maximum 10m)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		store, err := openEdgeAdminStore(*stateRoot)
		if err != nil {
			return err
		}
		defer store.Close()
		code, err := store.CreatePairing(*ttl)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, code)
		return nil
	case "revoke":
		fs := flag.NewFlagSet("edge revoke", flag.ContinueOnError)
		fs.SetOutput(stderr)
		stateRoot := fs.String("state-root", "", "absolute private state root")
		deviceID := fs.String("device", "", "paired device id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		store, err := openEdgeAdminStore(*stateRoot)
		if err != nil {
			return err
		}
		defer store.Close()
		if err := store.Revoke(strings.TrimSpace(*deviceID)); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "revoked "+strings.TrimSpace(*deviceID))
		return nil
	case "devices":
		fs := flag.NewFlagSet("edge devices", flag.ContinueOnError)
		fs.SetOutput(stderr)
		stateRoot := fs.String("state-root", "", "absolute private state root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		store, err := openEdgeAdminStore(*stateRoot)
		if err != nil {
			return err
		}
		defer store.Close()
		devices, err := store.ActiveDevices()
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(devices)
	case "task-create":
		return edgeTaskCreate(args[1:], stdout, stderr)
	case "task-cancel":
		return edgeTaskCancel(args[1:], stdout, stderr)
	case "task-status":
		return edgeTaskStatus(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown edge command: %s", args[0])
	}
}

type acceptanceFlag []string

func (a *acceptanceFlag) String() string { return strings.Join(*a, ",") }
func (a *acceptanceFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("acceptance criterion must not be empty")
	}
	*a = append(*a, value)
	return nil
}

func edgeTaskCreate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("edge task-create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRoot := fs.String("state-root", "", "absolute private state root")
	deviceID := fs.String("device", "", "active device id")
	idempotency := fs.String("idempotency", "", "stable idempotency key")
	workspace := fs.String("workspace", "", "workcell-local workspace name")
	objective := fs.String("objective", "", "bounded structured objective summary")
	network := fs.String("network", string(edge.NetworkNone), "none|registry")
	maxDuration := fs.Duration("max-duration", 10*time.Minute, "maximum task duration")
	maxOutput := fs.Int64("max-output", 256<<10, "maximum result bytes")
	var acceptance acceptanceFlag
	fs.Var(&acceptance, "accept", "acceptance criterion; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openEdgeAdminStore(*stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	task, _, err := store.CreateTask(strings.TrimSpace(*deviceID), edge.TaskSpec{
		IdempotencyKey: strings.TrimSpace(*idempotency),
		Workcell:       "development",
		Objective: edge.Objective{
			Kind:       edge.ObjectiveValidate,
			Summary:    strings.TrimSpace(*objective),
			Acceptance: acceptance,
		},
		Restrictions: edge.Restrictions{
			Workspace:          strings.TrimSpace(*workspace),
			NetworkPolicy:      edge.NetworkPolicy(strings.TrimSpace(*network)),
			MaxDurationSeconds: int(maxDuration.Seconds()),
			MaxOutputBytes:     *maxOutput,
		},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(task)
}

func edgeTaskCancel(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("edge task-cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRoot := fs.String("state-root", "", "absolute private state root")
	taskID := fs.String("task", "", "edge task id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openEdgeAdminStore(*stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.CancelTask(strings.TrimSpace(*taskID)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "cancel requested "+strings.TrimSpace(*taskID))
	return nil
}

func edgeTaskStatus(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("edge task-status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRoot := fs.String("state-root", "", "absolute private state root")
	taskID := fs.String("task", "", "edge task id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := openEdgeAdminStore(*stateRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	task, err := store.TaskStatus(strings.TrimSpace(*taskID))
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(task)
}

func openEdgeAdminStore(stateRoot string) (*edge.Store, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return nil, fmt.Errorf("--state-root must be an absolute path")
	}
	return edge.Open(edge.Config{Root: filepath.Join(filepath.Clean(stateRoot), "edge"), Now: time.Now})
}
