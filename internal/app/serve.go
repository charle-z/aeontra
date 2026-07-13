package app

import "os"

func serve(args []string) error {
	opts, err := parseServeOptions(args, os.Stderr)
	if err != nil {
		return err
	}
	runtime, err := buildRuntime(opts)
	if err != nil {
		return err
	}
	defer runtime.Close()

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
