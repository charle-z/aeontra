package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("model-turn-driver", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "", "private MCP Devbox state root")
	socketPath := flags.String("socket", "", "private Unix socket path; defaults under the state root")
	remote := flags.Bool("remote", false, "serve a signed RemoteEdgeTransport lease read from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *stateRoot == "" || !filepath.IsAbs(*stateRoot) {
		return errors.New("--state-root must be an absolute path")
	}
	cleanStateRoot := filepath.Clean(*stateRoot)
	if *socketPath == "" {
		*socketPath = filepath.Join(cleanStateRoot, modelturn.DefaultDriverSocketName)
	}
	if !filepath.IsAbs(*socketPath) {
		return errors.New("--socket must be an absolute path")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *remote {
		return runRemoteDriver(ctx, cleanStateRoot, filepath.Clean(*socketPath))
	}
	store, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(cleanStateRoot, "model-turns")})
	if err != nil {
		return fmt.Errorf("open model turn store: %w", err)
	}
	defer store.Close()
	return modelturn.ServeDriver(ctx, filepath.Clean(*socketPath), store, os.Stdout)
}
