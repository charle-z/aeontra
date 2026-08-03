//go:build !windows

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
	"github.com/charle-z/mcp-devbox/internal/edgeupdate"
)

var servicePattern = regexp.MustCompile(`^mcp-devbox-opencode-edge@[a-z_][a-z0-9_-]{0,31}\.service$`)
var systemctlCommand = func(args ...string) ([]byte, error) {
	return exec.Command("systemctl", args...).CombinedOutput()
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "mcp-bundle-updater must run as root")
		return 1
	}
	key, err := compiledPublicKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, bundle.ManifestInvalid)
		return 1
	}
	service, err := configuredService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "updater service configuration is invalid")
		return 1
	}
	engine := edgeupdate.Engine{Root: "/opt/mcp-devbox", PublicKey: key, Service: service}
	var status edgeupdate.Status
	operation, err := parseUpdaterOperation(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch operation {
	case "status":
		status, err = engine.Status()
	case "update":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		status, err = (edgeupdate.OfficialResolver{PublicKey: key}).UpdateStable(ctx, engine)
	case "rollback":
		status, err = engine.Rollback()
	case "repair":
		status, err = repairInstallation(context.Background(), engine, edgeupdate.OfficialResolver{PublicKey: key}, service)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	encoded, _ := json.Marshal(status)
	fmt.Println(string(encoded))
	return 0
}

func parseUpdaterOperation(args []string) (string, error) {
	switch {
	case len(args) == 1 && (args[0] == "status" || args[0] == "rollback" || args[0] == "repair"):
		return args[0], nil
	case len(args) == 2 && args[0] == "update" && args[1] == "stable":
		return "update", nil
	default:
		return "", errors.New("accepted operations are status, update stable, rollback, or repair")
	}
}

func compiledPublicKey() (ed25519.PublicKey, error) {
	key, err := hex.DecodeString(buildinfo.EdgeBundlePublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("invalid compiled public key")
	}
	return ed25519.PublicKey(key), nil
}

func configuredService() (*systemdService, error) {
	content, err := os.ReadFile("/etc/mcp-devbox/edge-user")
	if err != nil {
		return nil, err
	}
	name := "mcp-devbox-opencode-edge@" + strings.TrimSpace(string(content)) + ".service"
	if !servicePattern.MatchString(name) {
		return nil, errors.New("invalid service")
	}
	return &systemdService{name: name}, nil
}

type systemdService struct{ name string }

func repairInstallation(ctx context.Context, engine edgeupdate.Engine, resolver edgeupdate.OfficialResolver, service *systemdService) (edgeupdate.Status, error) {
	status, err := engine.Status()
	if err != nil || status.Release == "" {
		return resolver.UpdateStable(ctx, engine)
	}
	releaseRoot := filepath.Join("/opt/mcp-devbox", edgeupdate.ReleasesDirectory, status.Release)
	if _, err := bundle.LoadTrusted(releaseRoot, engine.PublicKey); err != nil {
		return resolver.UpdateStable(ctx, engine)
	}
	for component, relative := range bundle.DefaultLayout() {
		if err := repairComponentPermissions(releaseRoot, component, relative); err != nil {
			return edgeupdate.Status{}, errors.New("official component permissions repair failed")
		}
	}
	links := map[string]string{
		"/usr/local/bin/mcp-edge":                            "/opt/mcp-devbox/current/bin/mcp-edge",
		"/usr/local/libexec/mcp-devbox/model-turn-driver":    "/opt/mcp-devbox/current/libexec/model-turn-driver",
		"/usr/local/libexec/mcp-devbox/mcp-autopilot-worker": "/opt/mcp-devbox/current/libexec/mcp-autopilot-worker",
		"/usr/local/libexec/mcp-devbox/mcp-bundle-updater":   "/opt/mcp-devbox/current/libexec/mcp-bundle-updater",
		"/usr/local/libexec/mcp-devbox/node":                 "/opt/mcp-devbox/current/libexec/node",
		"/opt/mcp-devbox/opencode-provider":                  "/opt/mcp-devbox/current/opencode-provider",
		"/opt/mcp-devbox/opencode-1.18.1":                    "/opt/mcp-devbox/current/opencode",
	}
	for destination, target := range links {
		if err := repairOfficialLink(destination, target); err != nil {
			return edgeupdate.Status{}, err
		}
	}
	if err := reconcileBundledGitHubCLI(releaseRoot); err != nil {
		return edgeupdate.Status{}, err
	}
	if err := service.InstallUnit(releaseRoot); err != nil {
		return edgeupdate.Status{}, err
	}
	if !service.EdgeHealthy() {
		if err := service.RestartEdge(); err != nil || !service.EdgeHealthy() {
			return edgeupdate.Status{}, errors.New("edge repair health check failed")
		}
	}
	return engine.Status()
}

func repairComponentPermissions(releaseRoot, component, relative string) error {
	mode := os.FileMode(0o644)
	if component == bundle.ComponentEdge || component == bundle.ComponentDriver || component == bundle.ComponentWorker || component == bundle.ComponentUpdater || component == bundle.ComponentNode || component == bundle.ComponentGitHubCLI || component == bundle.ComponentOpenCode {
		mode = 0o755
	}
	err := os.Chmod(filepath.Join(releaseRoot, filepath.FromSlash(relative)), mode)
	if component == bundle.ComponentGitHubCLI && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func repairOfficialLink(destination, target string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return errors.New("official link directory unavailable")
	}
	if existing, err := os.Readlink(destination); err == nil && existing == target {
		return nil
	}
	if _, err := os.Lstat(destination); err == nil {
		backup := destination + ".p14-backup"
		if _, backupErr := os.Lstat(backup); !errors.Is(backupErr, os.ErrNotExist) {
			return errors.New("official link backup already exists")
		}
		if err := os.Rename(destination, backup); err != nil {
			return errors.New("old official component backup failed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("official link is unsafe")
	}
	temporary := destination + ".next"
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return errors.New("official link staging failed")
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return errors.New("official link activation failed")
	}
	return nil
}

func (s *systemdService) InstallUnit(releaseRoot string) error {
	if err := reconcileBundledGitHubCLI(releaseRoot); err != nil {
		return err
	}
	source := releaseRoot + "/systemd/mcp-devbox-opencode-edge@.service"
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("signed edge unit is unavailable")
	}
	temporary := "/etc/systemd/system/.mcp-devbox-opencode-edge@.service.next"
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.New("edge unit staging failed")
	}
	if err := os.Rename(temporary, "/etc/systemd/system/mcp-devbox-opencode-edge@.service"); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if _, err := systemctlCommand("daemon-reload"); err != nil {
		return errors.New("systemd reload failed")
	}
	return retireLegacyEdgeServices()
}

func retireLegacyEdgeServices() error {
	for _, unit := range []string{"mcp-devbox-edge.service", "mcp-devbox-opencode-edge.service"} {
		output, err := systemctlCommand("show", unit, "--property=LoadState", "--value")
		if err != nil {
			return errors.New("legacy Edge service inspection failed")
		}
		switch strings.TrimSpace(string(output)) {
		case "not-found":
			continue
		case "loaded":
		default:
			return errors.New("legacy Edge service state is invalid")
		}
		// The legacy Edge may be the process that requested this updater. Keep it
		// alive until RestartEdge starts the managed unit; the managed unit's
		// Conflicts ordering then performs one atomic handoff without leaving the
		// updater waiting on its own caller to stop.
		if _, err := systemctlCommand("disable", unit); err != nil {
			return errors.New("legacy Edge service retirement failed")
		}
	}
	return nil
}

func reconcileBundledGitHubCLI(releaseRoot string) error {
	return reconcileBundledGitHubCLIAt(releaseRoot, "/usr/local/bin/gh", "/opt/mcp-devbox/current/libexec/gh")
}

func reconcileBundledGitHubCLIAt(releaseRoot, destination, target string) error {
	info, err := os.Lstat(filepath.Join(releaseRoot, "libexec/gh"))
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("signed GitHub CLI is unsafe")
		}
		return repairOfficialLink(destination, target)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("signed GitHub CLI is unavailable")
	}
	existing, readErr := os.Readlink(destination)
	if readErr == nil && existing == target {
		return os.Remove(destination)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil
	}
	return nil
}

func (s *systemdService) RestartEdge() error {
	return exec.Command("systemctl", "restart", s.name).Run()
}

func (s *systemdService) EdgeHealthy() bool {
	return exec.Command("systemctl", "is-active", "--quiet", s.name).Run() == nil
}
