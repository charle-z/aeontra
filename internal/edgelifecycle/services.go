package edgelifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var knownEdgeServiceUnits = []string{
	"mcp-devbox-edge.service",
	"mcp-devbox-opencode-edge.service",
	"mcp-devbox-opencode-edge@.service",
	"mcp-devbox-edge-onboard@.path",
	"mcp-devbox-edge-repair.service",
}

func inspectKnownServices(rawRoot string) (PathStatus, []ServiceStatus, []Blocker, error) {
	rawRoot = strings.TrimSpace(rawRoot)
	if rawRoot == "" {
		return PathStatus{}, nil, nil, nil
	}
	root := filepath.Clean(rawRoot)
	if !filepath.IsAbs(root) || root == string(os.PathSeparator) {
		return PathStatus{}, nil, nil, errors.New("systemd root must be an absolute non-root path")
	}
	status, err := inspectPath(root, false)
	if err != nil {
		return PathStatus{}, nil, nil, fmt.Errorf("inspect systemd root: %w", err)
	}
	var blockers []Blocker
	if status.SymlinkAncestor || status.Kind == PathSymlink {
		blockers = append(blockers, Blocker{Code: BlockerSystemdRootSymlink, Subject: "systemd_root"})
	}
	if status.Exists && status.Kind != PathDirectory {
		blockers = append(blockers, Blocker{Code: BlockerSystemdRootNotDirectory, Subject: "systemd_root"})
	}
	if len(blockers) > 0 {
		return status, nil, blockers, nil
	}

	services := make([]ServiceStatus, 0, len(knownEdgeServiceUnits))
	for _, name := range knownEdgeServiceUnits {
		unit, err := inspectPath(filepath.Join(root, name), false)
		if err != nil {
			return PathStatus{}, nil, nil, fmt.Errorf("inspect known Edge service: %w", err)
		}
		services = append(services, ServiceStatus{Name: name, Kind: unit.Kind})
	}
	return status, services, blockers, nil
}
