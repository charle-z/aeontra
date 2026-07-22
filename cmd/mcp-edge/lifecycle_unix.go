//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edgelifecycle"
)

var inspectEdgeLifecycle = edgelifecycle.InspectLayout
var planEdgeStateMigration = edgelifecycle.PlanLegacyStateMigration
var applyEdgeStateMigration = edgelifecycle.ApplyLegacyStateMigration
var recoverEdgeStateMigration = edgelifecycle.RecoverLegacyStateMigration
var finalizeEdgeStateMigration = edgelifecycle.FinalizeLegacyStateMigration
var rollbackEdgeStateMigration = edgelifecycle.RollbackPreparedLegacyStateMigration

func lifecycleCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("lifecycle requires a closed state operation")
	}
	config, err := localLifecycleConfig()
	if err != nil {
		return err
	}
	switch args[0] {
	case "inspect":
		if len(args) != 1 {
			return errors.New("lifecycle inspect accepts no arguments")
		}
		report, err := inspectEdgeLifecycle(config.Inventory)
		if err != nil {
			return errors.New("Edge lifecycle inventory failed")
		}
		fmt.Fprint(stdout, formatLifecycleInventory(report))
		return nil
	case "migrate-state":
		if len(args) != 1 {
			return errors.New("lifecycle migrate-state accepts no arguments")
		}
		plan, err := planEdgeStateMigration(config)
		if err != nil {
			return err
		}
		result, err := applyEdgeStateMigration(config, plan, edgelifecycle.MigrationHooks{})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "edge_lifecycle migration=%s\n", result.Status)
		return nil
	case "prepare-state-migration":
		if len(args) != 1 {
			return errors.New("lifecycle prepare-state-migration accepts no arguments")
		}
		plan, err := planEdgeStateMigration(config)
		if err != nil {
			return err
		}
		result, err := applyEdgeStateMigration(config, plan, edgelifecycle.MigrationHooks{RetainVerifiedJournal: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "edge_lifecycle migration=%s\n", result.Status)
		return nil
	case "finalize-state-migration":
		if len(args) != 1 {
			return errors.New("lifecycle finalize-state-migration accepts no arguments")
		}
		result, err := finalizeEdgeStateMigration(config)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "edge_lifecycle finalization=%s\n", result.Status)
		return nil
	case "rollback-state-migration":
		if len(args) != 1 {
			return errors.New("lifecycle rollback-state-migration accepts no arguments")
		}
		result, err := rollbackEdgeStateMigration(config)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "edge_lifecycle rollback=%s\n", result.Status)
		return nil
	case "recover-state":
		if len(args) != 1 {
			return errors.New("lifecycle recover-state accepts no arguments")
		}
		result, err := recoverEdgeStateMigration(config)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "edge_lifecycle recovery=%s\n", result.Status)
		return nil
	default:
		return errors.New("unknown lifecycle command")
	}
}

func localLifecycleConfig() (edgelifecycle.StateMigrationConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) || home == string(os.PathSeparator) {
		return edgelifecycle.StateMigrationConfig{}, errors.New("Edge home is unavailable")
	}
	return edgelifecycle.StateMigrationConfig{
		Inventory: edgelifecycle.InventoryConfig{
			HomeDir:         home,
			InstallRoot:     installedBundleRoot,
			SystemdRoot:     "/etc/systemd/system",
			HistoricalPaths: []string{filepath.Join(home, "p12")},
		},
		ExpectedUID: os.Geteuid(),
		ExpectedGID: os.Getegid(),
	}, nil
}

func formatLifecycleInventory(report edgelifecycle.LayoutReport) string {
	blockers := make([]string, 0, len(report.Blockers))
	for _, blocker := range report.Blockers {
		blockers = append(blockers, string(blocker.Code))
	}
	sort.Strings(blockers)
	historical := make([]string, 0, len(report.Historical))
	for _, item := range report.Historical {
		historical = append(historical, string(item.Kind))
	}
	return fmt.Sprintf(
		"edge_lifecycle preferred=%s legacy=%s development=%s labs=%s current=%s migration=%s historical=%s blockers=%s\n",
		report.PreferredState.Kind,
		report.LegacyState.Kind,
		report.DevelopmentRoot.Kind,
		report.LabRoot.Kind,
		report.CurrentRelease.Kind,
		report.StateMigration,
		joinLifecycleValues(historical),
		joinLifecycleValues(blockers),
	)
}

func joinLifecycleValues(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ",")
}
