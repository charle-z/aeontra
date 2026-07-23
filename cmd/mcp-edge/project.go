package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type localProjectStores struct {
	projects   *edgeclient.ProjectRegistry
	workspaces *edgeclient.WorkspaceRegistry
}

type localProjectDiscovery struct {
	owner     string
	roots     edgeclient.WorkspaceRoots
	inspector edgeclient.ProjectCheckoutInspector
}

func (stores *localProjectStores) Close() {
	if stores == nil {
		return
	}
	if stores.projects != nil {
		_ = stores.projects.Close()
	}
	if stores.workspaces != nil {
		_ = stores.workspaces.Close()
	}
}

var openLocalProjectStores = func() (*localProjectStores, error) {
	if err := ensureWorkcellUser(); err != nil {
		return nil, err
	}
	state := defaultStateRoot()
	github, err := edgeclient.LocalGitHubCredentialStatus(state)
	if err != nil || !github.Configured {
		return nil, errors.New("local GitHub owner authority is not configured")
	}
	workspaces, err := edgeclient.OpenWorkspaceRegistry(state)
	if err != nil {
		return nil, err
	}
	projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: github.Owner, Workspaces: workspaces,
	})
	if err != nil {
		_ = workspaces.Close()
		return nil, err
	}
	return &localProjectStores{projects: projects, workspaces: workspaces}, nil
}

var openLocalProjectDiscovery = func() (localProjectDiscovery, error) {
	if err := ensureWorkcellUser(); err != nil {
		return localProjectDiscovery{}, err
	}
	github, err := edgeclient.LocalGitHubCredentialStatus(defaultStateRoot())
	if err != nil || !github.Configured {
		return localProjectDiscovery{}, errors.New("local GitHub owner authority is not configured")
	}
	roots, err := edgeclient.DefaultWorkspaceRoots()
	if err != nil {
		return localProjectDiscovery{}, err
	}
	return localProjectDiscovery{owner: github.Owner, roots: roots}, nil
}

func projectCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("project requires discover, status or resolve")
	}
	operation := args[0]
	if operation != "discover" && operation != "status" && operation != "resolve" {
		return errors.New("project accepts only discover, status or resolve")
	}
	fs := flag.NewFlagSet("project "+operation, flag.ContinueOnError)
	fs.SetOutput(stderr)
	alias := fs.String("alias", "", "human project alias")
	repository := fs.String("repository", "", "owner-bound repository name")
	target := fs.String("target", "", "human execution target alias")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *alias == "" {
		return errors.New("project command arguments are invalid")
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if operation == "discover" {
		if *repository == "" || *target != "" {
			return errors.New("project discovery requires --alias and --repository")
		}
		local, err := openLocalProjectDiscovery()
		if err != nil {
			return err
		}
		decision, err := edgeclient.DiscoverProjectCheckout(context.Background(), edgeclient.ProjectDiscoveryConfig{
			Roots: local.roots, Inspector: local.inspector,
		}, edgeclient.ProjectDiscoveryRequest{Alias: *alias, Owner: local.owner, Repository: *repository})
		if err != nil {
			return err
		}
		return encoder.Encode(decision.SafeStatus())
	}
	if *repository != "" {
		return errors.New("project lookup accepts only --alias and optional --target")
	}
	stores, err := openLocalProjectStores()
	if err != nil {
		return err
	}
	defer stores.Close()
	if operation == "status" {
		status, err := stores.projects.Status(context.Background(), *alias, *target)
		if err != nil {
			return err
		}
		return encoder.Encode(status)
	}
	resolution, err := stores.projects.Resolve(context.Background(), *alias, *target)
	if err != nil {
		status, statusErr := stores.projects.Status(context.Background(), *alias, *target)
		if statusErr == nil {
			_ = encoder.Encode(status)
		}
		return err
	}
	return encoder.Encode(resolution.SafeStatus())
}
