package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func githubCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("github subcommand required: configure or status")
	}
	switch args[0] {
	case "configure":
		fs := flag.NewFlagSet("github configure", flag.ContinueOnError)
		fs.SetOutput(stderr)
		state := fs.String("state", defaultStateRoot(), "private Edge state root")
		owner := fs.String("owner", "", "fixed GitHub user or organization")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("github configure does not accept positional arguments")
		}
		status, err := edgeclient.ConfigureGitHubCredential(*state, *owner, stdin)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(status)
	case "status":
		fs := flag.NewFlagSet("github status", flag.ContinueOnError)
		fs.SetOutput(stderr)
		state := fs.String("state", defaultStateRoot(), "private Edge state root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("github status does not accept positional arguments")
		}
		status, err := edgeclient.LocalGitHubCredentialStatus(*state)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(status)
	default:
		return errors.New("unknown github subcommand")
	}
}
