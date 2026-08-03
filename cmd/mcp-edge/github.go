package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

var readGitHubCLIToken = loadGitHubCLIToken
var githubCLIPaths = []string{"/opt/mcp-devbox/current/libexec/gh", "/usr/local/bin/gh", "/usr/bin/gh"}

func githubCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("github subcommand required: configure, import-gh, or status")
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
	case "import-gh":
		fs := flag.NewFlagSet("github import-gh", flag.ContinueOnError)
		fs.SetOutput(stderr)
		state := fs.String("state", defaultStateRoot(), "private Edge state root")
		owner := fs.String("owner", "", "fixed GitHub user or organization")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("github import-gh does not accept positional arguments")
		}
		token, err := readGitHubCLIToken()
		if err != nil {
			return errors.New("active GitHub CLI login is unavailable")
		}
		defer func() {
			for index := range token {
				token[index] = 0
			}
		}()
		status, err := edgeclient.ConfigureGitHubCredential(*state, *owner, bytes.NewReader(token))
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

func loadGitHubCLIToken() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ghPath, err := githubCLIPath()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, ghPath, "auth", "token", "--hostname", "github.com")
	command.Env = filteredGitHubCLIEnvironment(os.Environ())
	pipe, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return nil, errors.New("GitHub CLI login is unavailable")
	}
	body, readErr := io.ReadAll(io.LimitReader(pipe, 1026))
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || len(body) > 1025 {
		for index := range body {
			body[index] = 0
		}
		return nil, errors.New("GitHub CLI login is unavailable")
	}
	return body, nil
}

func githubCLIPath() (string, error) {
	for _, path := range githubCLIPaths {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && info.Mode().Perm()&0o022 == 0 {
			return path, nil
		}
	}
	return "", errors.New("GitHub CLI login is unavailable")
}

func filteredGitHubCLIEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
