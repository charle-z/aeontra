//go:build !windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/edgelifecycle"
)

const edgeRepairService = "mcp-devbox-edge-repair.service"
const doctorToolPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var doctorVerifyBundle = verifyInstalledEdgeBundle
var doctorInspectLayout = edgelifecycle.InspectLayout
var doctorLoadIdentity = edgeclient.LoadIdentity
var doctorCurrentUser = user.Current
var doctorDiscoverRootless = func(uid int) (*edgeclient.RootlessContainerEndpoint, error) {
	return edgeclient.DiscoverRootlessContainerEndpoint(uid, doctorToolPath)
}
var doctorServiceActive = func(service string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", service).Run() == nil
}
var doctorStartRepair = func() error {
	return exec.Command("systemctl", "start", edgeRepairService).Run()
}
var doctorRecoverMigration = edgelifecycle.RecoverLegacyStateMigration
var doctorPlanMigration = edgelifecycle.PlanLegacyStateMigration
var doctorApplyMigration = edgelifecycle.ApplyLegacyStateMigration

func doctorCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repair := fs.Bool("repair", false, "run the fixed signed-installation repair")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("doctor accepts only --repair")
	}
	if *repair {
		if err := repairEdgeInstallation(); err != nil {
			return err
		}
	}
	report, healthy, err := inspectEdgeHealth()
	if report != "" {
		fmt.Fprintln(stdout, report)
	}
	if err != nil {
		return err
	}
	if !healthy {
		return errors.New("edge doctor found a degraded installation")
	}
	return nil
}

func repairEdgeInstallation() error {
	config, err := localLifecycleConfig()
	if err != nil {
		return err
	}
	if _, err := doctorRecoverMigration(config); err != nil {
		return err
	}
	plan, err := doctorPlanMigration(config)
	if err != nil {
		return err
	}
	if plan.Needed {
		if _, err := doctorApplyMigration(config, plan, edgelifecycle.MigrationHooks{}); err != nil {
			return err
		}
	}
	if err := doctorStartRepair(); err != nil {
		return errors.New("fixed Edge repair service failed")
	}
	return nil
}

func inspectEdgeHealth() (string, bool, error) {
	config, err := localLifecycleConfig()
	if err != nil {
		return "", false, err
	}
	if err := doctorVerifyBundle(installedBundleRoot); err != nil {
		return "edge_doctor status=blocked bundle=invalid layout=unknown identity=unknown service=unknown rootless=unknown", false, errors.New("edge doctor found an invalid signed bundle")
	}
	layout, err := doctorInspectLayout(config.Inventory)
	if err != nil || len(layout.Blockers) != 0 {
		return "edge_doctor status=blocked bundle=valid layout=blocked identity=unknown service=unknown rootless=unknown", false, errors.New("edge doctor found an unsafe layout")
	}
	currentUser, err := doctorCurrentUser()
	if err != nil || !onboardingUserPattern.MatchString(strings.TrimSpace(currentUser.Username)) {
		return "edge_doctor status=blocked bundle=valid layout=valid identity=unknown service=unknown rootless=unknown", false, errors.New("edge doctor user identity unavailable")
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil || uid < 1 {
		return "edge_doctor status=blocked bundle=valid layout=valid identity=unknown service=unknown rootless=unknown", false, errors.New("edge doctor user identity unavailable")
	}
	endpoint, err := doctorDiscoverRootless(uid)
	if err != nil {
		return "edge_doctor status=blocked bundle=valid layout=valid identity=unknown service=unknown rootless=unsafe", false, errors.New("edge doctor found an unsafe rootless endpoint")
	}
	rootless := "missing"
	if endpoint != nil {
		rootless = endpoint.Engine
	}

	identity, _, identityErr := doctorLoadIdentity(filepath.Join(config.Inventory.HomeDir, ".local", "state", "mcp-edge"))
	if identityErr != nil {
		identityPath := filepath.Join(config.Inventory.HomeDir, ".local", "state", "mcp-edge", "identity.json")
		if _, statErr := os.Lstat(identityPath); errors.Is(statErr, os.ErrNotExist) {
			return fmt.Sprintf("edge_doctor status=setup_required bundle=valid layout=valid identity=missing service=inactive rootless=%s", rootless), true, nil
		}
		return fmt.Sprintf("edge_doctor status=blocked bundle=valid layout=valid identity=invalid service=unknown rootless=%s", rootless), false, errors.New("edge doctor found an invalid identity")
	}
	service := "mcp-devbox-opencode-edge@" + currentUser.Username + ".service"
	active := doctorServiceActive(service)
	status := "degraded"
	serviceState := "inactive"
	healthy := endpoint != nil && active
	if healthy {
		status = "ready"
		serviceState = "active"
	}
	return fmt.Sprintf("edge_doctor status=%s bundle=valid layout=valid identity=valid alias=%s service=%s rootless=%s", status, identity.Name, serviceState, rootless), healthy, nil
}
