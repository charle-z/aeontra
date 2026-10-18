package app

import (
	"errors"
	"os"
)

func serve(args []string) (serveErr error) {
	opts, err := parseServeOptions(args, os.Stderr)
	if err != nil {
		return err
	}
	runtime, err := buildRuntime(opts)
	if err != nil {
		return err
	}
	defer func() {
		serveErr = errors.Join(serveErr, runtime.Close())
	}()

	admin, err := startLocalGrantAdmin(runtime, os.Stderr)
	if err != nil {
		return err
	}
	defer admin.Close()

	transport, err := resolveTransport(opts)
	if err != nil {
		return err
	}
	return serveTransport(runtime, transport)
}
