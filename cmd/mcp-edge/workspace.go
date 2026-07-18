package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func workspaceCommand(args []string, stdout, stderr io.Writer) error {
	if err := ensureWorkcellUser(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("workspace command requires add, configure, inventory, list, or remove")
	}
	switch args[0] {
	case "add":
		return workspaceAdd(args[1:], stdout, stderr)
	case "configure":
		return workspaceConfigure(args[1:], stdout, stderr)
	case "inventory":
		return workspaceInventory(args[1:], stdout, stderr)
	case "list":
		return workspaceList(args[1:], stdout, stderr)
	case "remove":
		return workspaceRemove(args[1:], stdout, stderr)
	default:
		return errors.New("unknown workspace command")
	}
}

func workspaceAdd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	pathFlag := fs.String("path", "", "absolute local workspace path")
	profileFlag := fs.String("profile", string(edgeclient.WorkspaceProfileSandbox), "sandbox or linux-workcell")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("workspace add accepts one local path")
	}
	path := strings.TrimSpace(*pathFlag)
	if fs.NArg() == 1 {
		if path != "" {
			return errors.New("workspace add accepts either --path or one positional path")
		}
		path = strings.TrimSpace(fs.Arg(0))
	}
	if path == "" {
		return errors.New("workspace add requires one local path")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	profile := edgeclient.WorkspaceProfile(strings.TrimSpace(*profileFlag))
	workspace, created, err := registry.AddProfile(path, profile)
	if err != nil {
		return err
	}
	status := "existing"
	if created {
		status = "added"
	}
	if workspace.Profile == edgeclient.WorkspaceProfileLinuxWorkcell {
		fmt.Fprintf(stdout, "%s %s %s %s %s\n", status, workspace.ID, workspace.Profile, workspace.Mode, workspace.Path)
	} else {
		fmt.Fprintf(stdout, "%s %s %s\n", status, workspace.ID, workspace.Path)
	}
	return nil
}

func workspaceList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("workspace list accepts no positional arguments")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	workspaces, err := registry.List()
	if err != nil {
		return err
	}
	for _, workspace := range workspaces {
		if workspace.Profile == edgeclient.WorkspaceProfileLinuxWorkcell {
			fmt.Fprintf(stdout, "%s %s %s %s %s\n", workspace.ID, workspace.Profile, workspace.Mode, workspace.NetworkPosture, workspace.Path)
		} else {
			fmt.Fprintf(stdout, "%s %s\n", workspace.ID, workspace.Path)
		}
	}
	return nil
}

func workspaceRemove(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	id := fs.String("id", "", "opaque workspace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*id) == "" {
		return errors.New("workspace remove requires exactly --id")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	if err := registry.Remove(*id); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "removed "+*id)
	return nil
}
