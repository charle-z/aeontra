package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func workspaceConfigure(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace configure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	idFlag := fs.String("id", "", "opaque workspace id")
	mode := fs.String("mode", string(edgeclient.WorkspaceModeDev), "dev or htb-linux")
	machine := fs.String("machine", "", "authorized lab machine name")
	target := fs.String("target", "", "single authorized lab target IPv4")
	difficulty := fs.String("difficulty", "", "EASY, MEDIUM, or HARD")
	operatingSystem := fs.String("os", "", "LINUX")
	vpnInterface := fs.String("vpn-interface", "tun0", "local VPN interface")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("workspace configure accepts one workspace id")
	}
	id := strings.TrimSpace(*idFlag)
	if fs.NArg() == 1 {
		if id != "" {
			return errors.New("workspace configure accepts either --id or one positional id")
		}
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" {
		return errors.New("workspace configure requires one workspace id")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	workspace, err := registry.Configure(id, edgeclient.WorkspaceConfiguration{
		Mode:         edgeclient.WorkspaceMode(strings.TrimSpace(*mode)),
		MachineName:  *machine,
		TargetIP:     *target,
		Difficulty:   *difficulty,
		OS:           *operatingSystem,
		VPNInterface: *vpnInterface,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "configured %s %s %s %s\n", workspace.ID, workspace.Profile, workspace.Mode, workspace.Path)
	return nil
}
