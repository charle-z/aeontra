//go:build !windows

package edgeclient

import (
	"errors"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/development"
)

// DevelopmentRequirementsFromToolchainReadiness converts repository-local
// manifest observations into capability requirements. It never claims that the
// current execution environment satisfies those requirements.
func DevelopmentRequirementsFromToolchainReadiness(readiness ToolchainReadiness) ([]development.Requirement, error) {
	switch readiness.Status {
	case ToolchainSupported, ToolchainEdgeRequired:
	case ToolchainPinConflict:
		return nil, errors.New("development toolchain pins conflict")
	default:
		return nil, errors.New("development toolchain readiness is invalid")
	}
	names := make([]string, 0, len(readiness.Findings))
	for _, finding := range readiness.Findings {
		if finding.Status == ToolchainPinConflict {
			return nil, errors.New("development toolchain pins conflict")
		}
		if finding.Status != ToolchainSupported && finding.Status != ToolchainEdgeRequired {
			return nil, errors.New("development toolchain finding is invalid")
		}
		base, ok := developmentCapabilityForTool(finding.Tool)
		if !ok {
			return nil, errors.New("development toolchain requirement is not representable")
		}
		pin := strings.TrimSpace(finding.Pin)
		if pin == "" {
			names = append(names, base)
			continue
		}
		if exact := exactDevelopmentToolchainVersion(pin); exact != "" {
			requirement, err := development.VersionRequirement(base, exact)
			if err != nil {
				return nil, errors.New("development toolchain version requirement is invalid")
			}
			names = append(names, string(requirement.ID))
			continue
		}
		// A supported broad constraint was already proven by the fixed L3
		// baseline detector. Encode that concrete baseline rather than
		// pretending every version of the tool would satisfy the range.
		if finding.Status == ToolchainSupported {
			if baseline := developmentSupportedBaselineVersion(finding.Tool); baseline != "" {
				requirement, err := development.VersionRequirement(base, baseline)
				if err != nil {
					return nil, errors.New("development baseline requirement is invalid")
				}
				names = append(names, string(requirement.ID))
				continue
			}
		}
		// Floating channels and arbitrary ranges that require another
		// environment need the managed version resolver; do not degrade them
		// to an unversioned capability.
		return nil, errors.New("development toolchain version constraint requires managed resolution")
	}
	return development.Requirements(names...)
}

// DevelopmentWorkcellAttestation maps one already-prepared trusted development
// workcell and sanitized local tool inventory into a server-owned capability
// attestation. HTB mode is deliberately excluded from development routing.
func DevelopmentWorkcellAttestation(environmentID string, generation uint64, preparation LinuxWorkcellPreparation, inventory []LinuxToolInventoryEntry) (development.EnvironmentAttestation, error) {
	if preparation.Workspace.Profile != WorkspaceProfileLinuxWorkcell ||
		preparation.Workspace.Mode != WorkspaceModeDev ||
		preparation.Workspace.ID == "" ||
		strings.TrimSpace(preparation.Workspace.Path) == "" {
		return development.EnvironmentAttestation{}, errors.New("development workcell preparation is invalid")
	}
	capabilities, err := developmentCapabilitiesFromLinuxInventory(inventory)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	names := []string{
		"exec.argv",
		"filesystem.workspace-rw",
		"isolation.bubblewrap",
		"network.host-shared",
	}
	for _, id := range capabilities.IDs() {
		names = append(names, string(id))
	}
	set, err := development.NewCapabilitySet(names...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation(environmentID, development.ClassWorkcell, generation, set)
}

// DevelopmentRootlessRuntimeAttestation represents the optional rootless
// container authority exposed to a prepared Codex workcell. The ordinary
// direct workcell attestation above intentionally does not inherit it.
func DevelopmentRootlessRuntimeAttestation(environmentID string, generation uint64, preparation LinuxWorkcellPreparation, inventory []LinuxToolInventoryEntry) (development.EnvironmentAttestation, error) {
	if preparation.Workspace.Profile != WorkspaceProfileLinuxWorkcell ||
		preparation.Workspace.Mode != WorkspaceModeDev ||
		preparation.Workspace.ID == "" ||
		strings.TrimSpace(preparation.Workspace.Path) == "" ||
		preparation.RootlessContainer == nil {
		return development.EnvironmentAttestation{}, errors.New("development rootless runtime preparation is invalid")
	}
	capabilities, err := developmentCapabilitiesFromLinuxInventory(inventory)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	names := []string{
		"exec.argv",
		"filesystem.workspace-rw",
		"isolation.bubblewrap",
		"network.host-shared",
		"container.engine.rootless",
	}
	switch preparation.RootlessContainer.Engine {
	case "docker":
		names = append(names, "container.docker.rootless")
		if strings.TrimSpace(preparation.ContainerProxySocket) != "" {
			names = append(names, "filesystem.workspace-bind")
		}
	case "podman":
		names = append(names, "container.podman.rootless")
	default:
		return development.EnvironmentAttestation{}, errors.New("development rootless runtime engine is invalid")
	}
	for _, id := range capabilities.IDs() {
		names = append(names, string(id))
	}
	set, err := development.NewCapabilitySet(names...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation(environmentID, development.ClassRootlessRuntime, generation, set)
}

// DevelopmentToolboxAttestation maps a live, identity-validated toolbox
// snapshot into generic capabilities. Installed project-specific toolchains are
// not inferred from the Debian base image; they require later explicit receipts.
// DevelopmentCodexRootlessRuntimeAttestation additionally proves the signed
// Docker client/Buildx bundle before advertising those capabilities. Pre-v7
// bundles remain valid rootless environments but do not claim Buildx.
func DevelopmentCodexRootlessRuntimeAttestation(environmentID string, generation uint64, preparation LinuxWorkcellPreparation, inventory []LinuxToolInventoryEntry, codexPath string) (development.EnvironmentAttestation, error) {
	base, err := DevelopmentRootlessRuntimeAttestation(environmentID, generation, preparation, inventory)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	containerTools, err := codexManagedContainerTools(codexPath)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	if containerTools == "" || preparation.RootlessContainer == nil || preparation.RootlessContainer.Engine != "docker" {
		return base, nil
	}
	names := make([]string, 0, len(base.Capabilities.IDs())+2)
	for _, id := range base.Capabilities.IDs() {
		names = append(names, string(id))
	}
	names = append(names, "container.docker.client", "container.buildx")
	set, err := development.NewCapabilitySet(names...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation(environmentID, development.ClassRootlessRuntime, generation, set)
}

func DevelopmentToolboxAttestation(environmentID string, snapshot ProjectToolboxSnapshot) (development.EnvironmentAttestation, error) {
	if snapshot.State != ProjectToolboxRunning ||
		snapshot.Generation == 0 ||
		!projectToolboxIDPattern.MatchString(snapshot.ToolboxID) ||
		snapshot.BaseImage != projectToolboxBaseImage ||
		!projectToolboxImageIDPattern.MatchString(snapshot.BaseImageID) ||
		snapshot.CPUMillis < 1 || snapshot.MemoryMiB < 1 || snapshot.ProcessLimit < 1 ||
		(snapshot.Lifecycle != projectToolboxPersistent && snapshot.Lifecycle != projectToolboxDisposable) {
		return development.EnvironmentAttestation{}, errors.New("development toolbox snapshot is invalid")
	}
	names := []string{
		"dependency.install",
		"exec.argv",
		"filesystem.workspace-rw",
		"network.host-shared",
		"service.local",
		"toolbox.debian",
	}
	if snapshot.ContainerAccess {
		names = append(names, "container.engine.rootless")
	}
	set, err := development.NewCapabilitySet(names...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation(environmentID, development.ClassToolbox, snapshot.Generation, set)
}

func developmentCapabilitiesFromLinuxInventory(inventory []LinuxToolInventoryEntry) (development.CapabilitySet, error) {
	if err := ValidateLinuxToolInventory(inventory); err != nil {
		return development.CapabilitySet{}, err
	}
	names := make([]string, 0, len(inventory))
	for _, entry := range inventory {
		if !entry.Available {
			continue
		}
		if name, ok := developmentCapabilityForInventoryTool(entry.Name); ok {
			names = append(names, name)
			if exact := exactDevelopmentToolchainVersion(entry.Version); exact != "" {
				versioned, err := development.VersionCapabilityIDs(name, exact)
				if err != nil {
					return development.CapabilitySet{}, err
				}
				for _, id := range versioned[1:] {
					names = append(names, string(id))
				}
			}
		}
	}
	return development.NewCapabilitySet(names...)
}

func developmentCapabilityForInventoryTool(tool string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "python":
		return "toolchain.python", true
	case "gcc":
		return "toolchain.c", true
	case "g++":
		return "toolchain.cpp", true
	case "make":
		return "build.make", true
	case "cmake":
		return "toolchain.cmake", true
	case "go":
		return "toolchain.go", true
	case "node":
		return "toolchain.node", true
	case "npm":
		return "toolchain.npm", true
	case "pnpm":
		return "toolchain.pnpm", true
	case "rust":
		return "toolchain.rust", true
	case "cargo":
		return "toolchain.cargo", true
	case "java":
		return "runtime.java", true
	case "javac":
		return "toolchain.java", true
	case "shell":
		return "shell.posix", true
	case "git":
		return "git.cli", true
	default:
		return "", false
	}
}

func developmentCapabilityForTool(tool string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "go":
		return "toolchain.go", true
	case "rust":
		return "toolchain.rust", true
	case "python":
		return "toolchain.python", true
	case "node":
		return "toolchain.node", true
	case "npm":
		return "toolchain.npm", true
	case "pnpm":
		return "toolchain.pnpm", true
	case "java":
		return "toolchain.java", true
	case "cmake":
		return "toolchain.cmake", true
	case "c":
		return "toolchain.c", true
	case "cpp":
		return "toolchain.cpp", true
	case "make":
		return "build.make", true
	default:
		return "", false
	}
}

func developmentSupportedBaselineVersion(tool string) string {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "go":
		return "1.26"
	case "rust":
		return "1.96"
	case "python":
		return "3.14"
	case "node":
		return "24"
	case "npm":
		return "12"
	default:
		return ""
	}
}

// Unlike the legacy readiness comparator, exact capabilities retain a zero
// patch component: 1.95.0 is not an assertion about every 1.95 patch release.
func exactDevelopmentToolchainVersion(raw string) string {
	value := strings.TrimSpace(strings.Trim(raw, "\"'"))
	if !toolchainNumericVersionPattern.MatchString(value) {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(value), "v")
}
