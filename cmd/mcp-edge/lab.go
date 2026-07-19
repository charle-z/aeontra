package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const labCommandTimeout = 30 * time.Second

var labMachinePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	labRouteLookup = detectLabRoute
	labGitRun      = runLabGit
)

func labCommand(args []string, stdout, stderr io.Writer) error {
	if err := ensureWorkcellUser(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("lab command requires init or ssh-exec")
	}
	switch args[0] {
	case "init":
		return labInit(args[1:], stdout, stderr)
	case "ssh-exec":
		return labSSHExec(args[1:], stdout, stderr)
	default:
		return errors.New("unknown lab command")
	}
}

func labInit(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lab init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	platform := fs.String("platform", "htb", "authorized lab platform")
	machine := fs.String("machine", "", "authorized machine name")
	target := fs.String("target", "", "single authorized target IPv4")
	difficulty := fs.String("difficulty", "", "EASY, MEDIUM, or HARD")
	operatingSystem := fs.String("os", "LINUX", "LINUX")
	vpnInterface := fs.String("vpn-interface", "", "VPN interface; auto-detected from the target route when omitted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("lab init accepts no positional arguments")
	}
	if strings.ToLower(strings.TrimSpace(*platform)) != "htb" {
		return errors.New("lab init currently supports only htb")
	}
	machineName := strings.TrimSpace(*machine)
	targetIP := strings.TrimSpace(*target)
	difficultyName := strings.ToUpper(strings.TrimSpace(*difficulty))
	osName := strings.ToUpper(strings.TrimSpace(*operatingSystem))
	if !labMachinePattern.MatchString(machineName) {
		return errors.New("lab machine name is invalid")
	}
	if parsed := net.ParseIP(targetIP); parsed == nil || parsed.To4() == nil || strings.Contains(targetIP, "/") {
		return errors.New("lab target must be one IPv4 address")
	} else {
		targetIP = parsed.To4().String()
	}
	if difficultyName != "EASY" && difficultyName != "MEDIUM" && difficultyName != "HARD" {
		return errors.New("lab difficulty is invalid")
	}
	if osName != "LINUX" {
		return errors.New("lab operating system must be LINUX")
	}
	ctx, cancel := context.WithTimeout(context.Background(), labCommandTimeout)
	defer cancel()
	iface, lhost, err := labRouteLookup(ctx, targetIP, strings.TrimSpace(*vpnInterface))
	if err != nil {
		return err
	}
	roots, err := edgeclient.DefaultWorkspaceRoots()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(roots.HTBLinux, 0o700); err != nil {
		return errors.New("HTB workspace root could not be created")
	}
	if err := os.Chmod(roots.HTBLinux, 0o700); err != nil {
		return errors.New("HTB workspace root permissions failed")
	}
	workspacePath := filepath.Join(roots.HTBLinux, machineName)
	if err := ensureLabWorkspace(workspacePath, machineName); err != nil {
		return err
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*state)
	if err != nil {
		return err
	}
	defer registry.Close()
	workspace, created, err := registry.AddProfile(workspacePath, edgeclient.WorkspaceProfileLinuxWorkcell)
	if err != nil {
		return err
	}
	workspace, err = registry.Configure(workspace.ID, edgeclient.WorkspaceConfiguration{
		Mode: edgeclient.WorkspaceModeHTBLinux, MachineName: machineName, TargetIP: targetIP,
		Difficulty: difficultyName, OS: osName, VPNInterface: iface,
	})
	if err != nil {
		return err
	}
	inventory, err := edgeclient.CollectLinuxToolInventory(ctx, "")
	if err != nil {
		return err
	}
	if err := edgeclient.ValidateLinuxToolInventory(inventory); err != nil {
		return err
	}
	available := 0
	for _, tool := range inventory {
		if tool.Available {
			available++
		}
	}
	status := "existing"
	if created {
		status = "created"
	}
	fmt.Fprintf(stdout, "lab-ready %s %s %s target=%s vpn=%s lhost=%s tools=%d/%d workspace=%s\n", status, workspace.ID, workspace.MachineName, workspace.TargetIP, workspace.VPNInterface, lhost, available, len(inventory), workspace.Path)
	return nil
}

func ensureLabWorkspace(path, machine string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("lab workspace is unsafe")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return errors.New("lab workspace could not be created")
		}
	} else {
		return errors.New("lab workspace is unavailable")
	}
	if _, err := edgeclient.ValidateRegisteredWorkspace(path); err != nil {
		return err
	}
	for _, name := range []string{"scans", "loot", "scripts", "reports", "tmp", "tickets"} {
		directory := filepath.Join(path, name)
		if info, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(directory, 0o700); err != nil {
				return errors.New("lab artifact directory could not be created")
			}
		} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("lab artifact directory is unsafe")
		}
	}
	gitDir := filepath.Join(path, ".git")
	if info, err := os.Lstat(gitDir); errors.Is(err, os.ErrNotExist) {
		if err := labGitRun(path, "init", "-b", "main"); err != nil {
			return errors.New("lab Git repository could not be initialized")
		}
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("lab Git repository is unsafe")
	}
	readme := filepath.Join(path, "README.md")
	if info, err := os.Lstat(readme); errors.Is(err, os.ErrNotExist) {
		content := fmt.Sprintf("# HTB %s\n\nAuthorized Hack The Box Linux lab workspace.\n", machine)
		if err := os.WriteFile(readme, []byte(content), 0o600); err != nil {
			return errors.New("lab README could not be created")
		}
		if err := labGitRun(path, "add", "README.md"); err != nil {
			return errors.New("lab README could not be staged")
		}
		if err := labGitRun(path, "-c", "user.name=HTB Workcell", "-c", "user.email=htb-workcell@localhost", "commit", "-m", "Initialize "+machine+" workspace"); err != nil {
			return errors.New("lab initial commit failed")
		}
	} else if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1<<20 {
		return errors.New("lab README is unsafe")
	}
	return nil
}

func runLabGit(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), labCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + dir, "LANG=C", "LC_ALL=C"}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil || output.Len() > 64<<10 {
		return errors.New("git command failed")
	}
	return nil
}

func detectLabRoute(ctx context.Context, target, requestedInterface string) (string, string, error) {
	command := exec.CommandContext(ctx, "ip", "route", "get", target)
	output, err := command.Output()
	if err != nil || len(output) > 16<<10 {
		return "", "", errors.New("lab target route lookup failed")
	}
	fields := strings.Fields(string(output))
	iface := ""
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == "dev" {
			iface = fields[index+1]
			break
		}
	}
	if requestedInterface != "" && iface != requestedInterface {
		return "", "", errors.New("lab target route does not use the requested VPN interface")
	}
	if iface == "" || (!strings.HasPrefix(iface, "tun") && !strings.HasPrefix(iface, "tap")) {
		return "", "", errors.New("lab target route does not use a VPN interface")
	}
	networkInterface, err := netInterfaceByName(iface)
	if err != nil {
		return "", "", errors.New("lab VPN interface is unavailable")
	}
	addresses, err := networkInterface.Addrs()
	if err != nil {
		return "", "", errors.New("lab VPN interface address is unavailable")
	}
	for _, address := range addresses {
		value := strings.SplitN(address.String(), "/", 2)[0]
		if parsed := net.ParseIP(value); parsed != nil && parsed.To4() != nil {
			return iface, parsed.To4().String(), nil
		}
	}
	return "", "", errors.New("lab VPN interface has no usable IPv4")
}

var netInterfaceByName = net.InterfaceByName
