package edgelifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PathKind string

const (
	PathMissing          PathKind = "missing"
	PathDirectory        PathKind = "directory"
	PathUnknownDirectory PathKind = "unknown_directory"
	PathFile             PathKind = "file"
	PathSymlink          PathKind = "symlink"
	PathEdgeState        PathKind = "edge_state"
	PathRepository       PathKind = "repository"
	PathSignedRelease    PathKind = "signed_release"
)

type MigrationDisposition string

const (
	MigrationNone              MigrationDisposition = "none"
	MigrationLegacyToPreferred MigrationDisposition = "legacy_to_preferred"
	MigrationBlocked           MigrationDisposition = "blocked"
)

type BlockerCode string

const (
	BlockerPreferredStateSymlink      BlockerCode = "preferred_state_symlink"
	BlockerLegacyStateSymlink         BlockerCode = "legacy_state_symlink"
	BlockerPreferredStateAncestorLink BlockerCode = "preferred_state_ancestor_symlink"
	BlockerLegacyStateAncestorLink    BlockerCode = "legacy_state_ancestor_symlink"
	BlockerPreferredIdentityLink      BlockerCode = "preferred_identity_marker_symlink"
	BlockerLegacyIdentityLink         BlockerCode = "legacy_identity_marker_symlink"
	BlockerPreferredStateOccupied     BlockerCode = "preferred_state_occupied"
	BlockerStateIdentityConflict      BlockerCode = "state_identity_conflict"
	BlockerDevelopmentRootSymlink     BlockerCode = "development_root_symlink"
	BlockerDevelopmentRootAncestor    BlockerCode = "development_root_ancestor_symlink"
	BlockerLabRootSymlink             BlockerCode = "lab_root_symlink"
	BlockerLabRootAncestor            BlockerCode = "lab_root_ancestor_symlink"
	BlockerDevelopmentRootNotDir      BlockerCode = "development_root_not_directory"
	BlockerLabRootNotDir              BlockerCode = "lab_root_not_directory"
	BlockerCurrentReleaseNotSymlink   BlockerCode = "current_release_not_symlink"
	BlockerCurrentReleaseAncestor     BlockerCode = "current_release_ancestor_symlink"
	BlockerCurrentReleaseOutsideRoot  BlockerCode = "current_release_outside_root"
	BlockerCurrentReleaseTargetAbsent BlockerCode = "current_release_target_absent"
	BlockerSystemdRootSymlink         BlockerCode = "systemd_root_symlink"
	BlockerSystemdRootNotDirectory    BlockerCode = "systemd_root_not_directory"
)

type Blocker struct {
	Code    BlockerCode `json:"code"`
	Subject string      `json:"subject"`
}

type PathStatus struct {
	Path                  string   `json:"path"`
	Kind                  PathKind `json:"kind"`
	Exists                bool     `json:"exists"`
	SymlinkAncestor       bool     `json:"symlink_ancestor,omitempty"`
	IdentityPresent       bool     `json:"identity_present,omitempty"`
	IdentityMarkerSymlink bool     `json:"identity_marker_symlink,omitempty"`
	Target                string   `json:"target,omitempty"`
}

type ServiceStatus struct {
	Name string   `json:"name"`
	Kind PathKind `json:"kind"`
}

type LayoutReport struct {
	PreferredState  PathStatus           `json:"preferred_state"`
	LegacyState     PathStatus           `json:"legacy_state"`
	DevelopmentRoot PathStatus           `json:"development_root"`
	LabRoot         PathStatus           `json:"lab_root"`
	CurrentRelease  PathStatus           `json:"current_release"`
	SystemdRoot     PathStatus           `json:"systemd_root"`
	Services        []ServiceStatus      `json:"services"`
	Historical      []PathStatus         `json:"historical"`
	StateMigration  MigrationDisposition `json:"state_migration"`
	Blockers        []Blocker            `json:"blockers"`
}

type InventoryConfig struct {
	HomeDir         string
	InstallRoot     string
	SystemdRoot     string
	HistoricalPaths []string
}

func InspectLayout(config InventoryConfig) (LayoutReport, error) {
	home, installRoot, historical, err := normalizeInventoryConfig(config)
	if err != nil {
		return LayoutReport{}, err
	}

	preferredPath := filepath.Join(home, ".local", "state", "mcp-edge")
	legacyPath := filepath.Join(home, ".config", "mcp-devbox-edge")
	developmentPath := filepath.Join(home, "workspaces")
	labPath := filepath.Join(home, "htb-machines")

	preferred, err := inspectPath(preferredPath, false)
	if err != nil {
		return LayoutReport{}, fmt.Errorf("inspect preferred Edge state: %w", err)
	}
	legacy, err := inspectPath(legacyPath, false)
	if err != nil {
		return LayoutReport{}, fmt.Errorf("inspect legacy Edge state: %w", err)
	}
	development, err := inspectPath(developmentPath, false)
	if err != nil {
		return LayoutReport{}, fmt.Errorf("inspect development root: %w", err)
	}
	lab, err := inspectPath(labPath, false)
	if err != nil {
		return LayoutReport{}, fmt.Errorf("inspect lab root: %w", err)
	}
	current, currentBlockers, err := inspectCurrentRelease(installRoot)
	if err != nil {
		return LayoutReport{}, err
	}
	systemdRoot, services, systemdBlockers, err := inspectKnownServices(config.SystemdRoot)
	if err != nil {
		return LayoutReport{}, err
	}

	report := LayoutReport{
		PreferredState:  preferred,
		LegacyState:     legacy,
		DevelopmentRoot: development,
		LabRoot:         lab,
		CurrentRelease:  current,
		SystemdRoot:     systemdRoot,
		Services:        services,
		Historical:      make([]PathStatus, 0, len(historical)),
		StateMigration:  MigrationNone,
		Blockers:        append(append([]Blocker(nil), currentBlockers...), systemdBlockers...),
	}

	for _, candidate := range historical {
		status, err := inspectPath(candidate, true)
		if err != nil {
			return LayoutReport{}, fmt.Errorf("inspect historical candidate: %w", err)
		}
		report.Historical = append(report.Historical, status)
	}

	report.Blockers = append(report.Blockers, stateAndRootBlockers(report)...)
	report.StateMigration = migrationDisposition(report)
	sort.SliceStable(report.Blockers, func(i, j int) bool {
		if report.Blockers[i].Code == report.Blockers[j].Code {
			return report.Blockers[i].Subject < report.Blockers[j].Subject
		}
		return report.Blockers[i].Code < report.Blockers[j].Code
	})
	return report, nil
}

func normalizeInventoryConfig(config InventoryConfig) (string, string, []string, error) {
	home := filepath.Clean(strings.TrimSpace(config.HomeDir))
	installRoot := filepath.Clean(strings.TrimSpace(config.InstallRoot))
	if home == "." || !filepath.IsAbs(home) {
		return "", "", nil, errors.New("Edge home must be an absolute path")
	}
	if installRoot == "." || !filepath.IsAbs(installRoot) {
		return "", "", nil, errors.New("Edge install root must be an absolute path")
	}
	if home == string(os.PathSeparator) {
		return "", "", nil, errors.New("Edge home must not be filesystem root")
	}

	historical := make([]string, 0, len(config.HistoricalPaths))
	seen := map[string]bool{}
	for _, raw := range config.HistoricalPaths {
		candidate := filepath.Clean(strings.TrimSpace(raw))
		if candidate == "." || !filepath.IsAbs(candidate) || !pathWithin(home, candidate) || candidate == home {
			return "", "", nil, errors.New("historical candidate must be a direct or nested path below Edge home")
		}
		if !seen[candidate] {
			seen[candidate] = true
			historical = append(historical, candidate)
		}
	}
	return home, installRoot, historical, nil
}

func inspectPath(path string, historical bool) (PathStatus, error) {
	status := PathStatus{Path: path, Kind: PathMissing}
	ancestorLink, err := hasSymlinkAncestor(path)
	if err != nil {
		return PathStatus{}, err
	}
	status.SymlinkAncestor = ancestorLink

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return PathStatus{}, err
	}
	status.Exists = true
	if info.Mode()&os.ModeSymlink != 0 {
		status.Kind = PathSymlink
		if target, readErr := os.Readlink(path); readErr == nil {
			status.Target = target
		}
		return status, nil
	}
	if !info.IsDir() {
		status.Kind = PathFile
		return status, nil
	}

	identityPath := filepath.Join(path, "identity.json")
	identity := markerRegular(identityPath)
	status.IdentityMarkerSymlink = markerSymlink(identityPath)
	status.IdentityPresent = identity
	switch {
	case identity:
		status.Kind = PathEdgeState
	case markerDirectory(filepath.Join(path, ".git")):
		status.Kind = PathRepository
	case markerRegular(filepath.Join(path, "manifest.json")) && markerRegular(filepath.Join(path, "manifest.sig")):
		status.Kind = PathSignedRelease
	case historical:
		status.Kind = PathUnknownDirectory
	default:
		status.Kind = PathDirectory
	}
	return status, nil
}

func inspectCurrentRelease(installRoot string) (PathStatus, []Blocker, error) {
	currentPath := filepath.Join(installRoot, "current")
	status, err := inspectPath(currentPath, false)
	if err != nil {
		return PathStatus{}, nil, fmt.Errorf("inspect active Edge release: %w", err)
	}
	if !status.Exists {
		return status, nil, nil
	}
	if status.SymlinkAncestor {
		return status, []Blocker{{Code: BlockerCurrentReleaseAncestor, Subject: "current_release"}}, nil
	}
	if status.Kind != PathSymlink {
		return status, []Blocker{{Code: BlockerCurrentReleaseNotSymlink, Subject: "current_release"}}, nil
	}

	resolved := status.Target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(currentPath), resolved)
	}
	resolved = filepath.Clean(resolved)
	releasesRoot := filepath.Join(installRoot, "releases")
	if resolved == releasesRoot || !pathWithin(releasesRoot, resolved) {
		return status, []Blocker{{Code: BlockerCurrentReleaseOutsideRoot, Subject: "current_release"}}, nil
	}
	targetInfo, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return status, []Blocker{{Code: BlockerCurrentReleaseTargetAbsent, Subject: "current_release"}}, nil
	}
	if err != nil {
		return PathStatus{}, nil, fmt.Errorf("inspect active Edge release target: %w", err)
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
		return status, []Blocker{{Code: BlockerCurrentReleaseTargetAbsent, Subject: "current_release"}}, nil
	}
	return status, nil, nil
}

func stateAndRootBlockers(report LayoutReport) []Blocker {
	var blockers []Blocker
	if report.PreferredState.Kind == PathSymlink {
		blockers = append(blockers, Blocker{Code: BlockerPreferredStateSymlink, Subject: "preferred_state"})
	}
	if report.PreferredState.SymlinkAncestor {
		blockers = append(blockers, Blocker{Code: BlockerPreferredStateAncestorLink, Subject: "preferred_state"})
	}
	if report.PreferredState.IdentityMarkerSymlink {
		blockers = append(blockers, Blocker{Code: BlockerPreferredIdentityLink, Subject: "preferred_state"})
	}
	if report.LegacyState.Kind == PathSymlink {
		blockers = append(blockers, Blocker{Code: BlockerLegacyStateSymlink, Subject: "legacy_state"})
	}
	if report.LegacyState.SymlinkAncestor {
		blockers = append(blockers, Blocker{Code: BlockerLegacyStateAncestorLink, Subject: "legacy_state"})
	}
	if report.LegacyState.IdentityMarkerSymlink {
		blockers = append(blockers, Blocker{Code: BlockerLegacyIdentityLink, Subject: "legacy_state"})
	}
	if report.PreferredState.IdentityPresent && report.LegacyState.IdentityPresent {
		blockers = append(blockers, Blocker{Code: BlockerStateIdentityConflict, Subject: "edge_identity"})
	}
	if report.LegacyState.IdentityPresent && report.PreferredState.Exists && !report.PreferredState.IdentityPresent {
		blockers = append(blockers, Blocker{Code: BlockerPreferredStateOccupied, Subject: "preferred_state"})
	}
	blockers = append(blockers, rootBlocker(report.DevelopmentRoot, BlockerDevelopmentRootSymlink, BlockerDevelopmentRootAncestor, BlockerDevelopmentRootNotDir, "development_root")...)
	blockers = append(blockers, rootBlocker(report.LabRoot, BlockerLabRootSymlink, BlockerLabRootAncestor, BlockerLabRootNotDir, "lab_root")...)
	return blockers
}

func rootBlocker(status PathStatus, symlinkCode, ancestorCode, notDirectoryCode BlockerCode, subject string) []Blocker {
	var blockers []Blocker
	if status.SymlinkAncestor {
		blockers = append(blockers, Blocker{Code: ancestorCode, Subject: subject})
	}
	if !status.Exists {
		return blockers
	}
	if status.Kind == PathSymlink {
		return append(blockers, Blocker{Code: symlinkCode, Subject: subject})
	}
	if status.Kind != PathDirectory && status.Kind != PathRepository {
		return append(blockers, Blocker{Code: notDirectoryCode, Subject: subject})
	}
	return blockers
}

func migrationDisposition(report LayoutReport) MigrationDisposition {
	for _, blocker := range report.Blockers {
		switch blocker.Code {
		case BlockerPreferredStateSymlink, BlockerLegacyStateSymlink,
			BlockerPreferredStateAncestorLink, BlockerLegacyStateAncestorLink,
			BlockerPreferredIdentityLink, BlockerLegacyIdentityLink,
			BlockerPreferredStateOccupied, BlockerStateIdentityConflict:
			return MigrationBlocked
		}
	}
	if report.LegacyState.IdentityPresent && !report.PreferredState.IdentityPresent {
		return MigrationLegacyToPreferred
	}
	return MigrationNone
}

func markerRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func markerDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func markerSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func hasSymlinkAncestor(path string) (bool, error) {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	volume := filepath.VolumeName(parent)
	root := volume + string(os.PathSeparator)
	if parent == root {
		return false, nil
	}
	relative := strings.TrimPrefix(parent, root)
	current := root
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
