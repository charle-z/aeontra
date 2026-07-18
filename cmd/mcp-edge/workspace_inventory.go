package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func workspaceInventory(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace inventory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	idFlag := fs.String("id", "", "opaque workspace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("workspace inventory accepts one workspace id")
	}
	id := strings.TrimSpace(*idFlag)
	if fs.NArg() == 1 {
		if id != "" {
			return errors.New("workspace inventory accepts either --id or one positional id")
		}
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" {
		return errors.New("workspace inventory requires one workspace id")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	workspace, err := registry.Get(id)
	if err != nil {
		return err
	}
	if workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell {
		return errors.New("workspace inventory requires a linux-workcell profile")
	}
	entries, err := edgeclient.CollectLinuxToolInventory(context.Background(), "")
	if err != nil {
		return err
	}
	if err := edgeclient.ValidateLinuxToolInventory(entries); err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(true)
	return encoder.Encode(struct {
		WorkspaceID string                               `json:"workspace_id"`
		Profile     edgeclient.WorkspaceProfile          `json:"profile"`
		Mode        edgeclient.WorkspaceMode             `json:"mode"`
		Tools       []edgeclient.LinuxToolInventoryEntry `json:"tools"`
	}{WorkspaceID: workspace.ID, Profile: workspace.Profile, Mode: workspace.Mode, Tools: entries})
}
