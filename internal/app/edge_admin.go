package app

import (
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
	default:
		return fmt.Errorf("unknown edge command: %s", args[0])
	}
}

func openEdgeAdminStore(stateRoot string) (*edge.Store, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return nil, fmt.Errorf("--state-root must be an absolute path")
	}
	return edge.Open(edge.Config{Root: filepath.Join(filepath.Clean(stateRoot), "edge"), Now: time.Now})
}
