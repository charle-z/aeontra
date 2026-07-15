//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const maxRemoteLeaseBytes = int64(128 << 10)

func runRemoteDriver(ctx context.Context, stateRoot, socketPath string) error {
	if os.Geteuid() == 0 {
		return errors.New("remote model-turn driver refuses to run as root")
	}
	cleanRoot := filepath.Clean(stateRoot)
	cleanSocket := filepath.Clean(socketPath)
	if !pathInside(cleanRoot, cleanSocket) {
		return errors.New("remote model-turn socket must stay inside the private state root")
	}
	var lease edgeclient.ModelRuntimeLease
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, maxRemoteLeaseBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lease); err != nil {
		return errors.New("remote model-turn lease is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("remote model-turn lease has trailing data")
	}
	transport, err := edgeclient.NewRemoteEdgeTransport(edgeclient.RemoteEdgeTransportOptions{StateRoot: cleanRoot, Lease: lease})
	if err != nil {
		return fmt.Errorf("open remote model-turn transport: %w", err)
	}
	defer transport.Close()
	return modelturn.ServeDriverTransport(ctx, cleanSocket, transport, os.Stdout)
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
