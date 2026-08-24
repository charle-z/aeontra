package edgeclient

import (
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type WorkspaceProfile string

type WorkspaceMode string

const (
	WorkspaceProfileSandbox         WorkspaceProfile = "sandbox"
	WorkspaceProfileLinuxWorkcell   WorkspaceProfile = "linux-workcell"
	WorkspaceProfileWindowsWorkcell WorkspaceProfile = "windows-workcell"

	WorkspaceModeDev      WorkspaceMode = "dev"
	WorkspaceModeHTBLinux WorkspaceMode = "htb-linux"

	LinuxWorkcellNetworkPosture   = "trusted_host_shared_network"
	WindowsWorkcellNetworkPosture = "trusted_windows_host_shared_network"
)

const workspaceSelect = "SELECT w.workspace_id,w.path,w.created_at,w.updated_at,c.profile,c.mode,c.machine_name,c.target_ip,c.difficulty,c.os,c.vpn_interface,c.authorization_revision FROM workspaces w JOIN workspace_configs c ON c.workspace_id=w.workspace_id"

type WorkspaceRoots struct {
	Dev        string
	HTBLinux   string
	WindowsDev string
}

type WorkspaceConfiguration struct {
	Mode         WorkspaceMode
	MachineName  string
	TargetIP     string
	Difficulty   string
	OS           string
	VPNInterface string
}

func OpenWorkspaceRegistryWithRoots(stateRoot string, roots WorkspaceRoots) (*WorkspaceRegistry, error) {
	roots, err := normalizeWorkspaceRoots(roots)
	if err != nil {
		return nil, err
	}
	if err := validateWorkspaceStateSeparation(stateRoot, roots); err != nil {
		return nil, err
	}
	registry, err := OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return nil, err
	}
	registry.roots = roots
	return registry, nil
}

var (
	machineNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	vpnInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,31}$`)
)

func DefaultWorkspaceRoots() (WorkspaceRoots, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || !filepath.IsAbs(home) {
		return WorkspaceRoots{}, errors.New("local Linux home is unavailable")
	}
	return normalizeWorkspaceRoots(WorkspaceRoots{
		Dev:        filepath.Join(home, "workspaces"),
		HTBLinux:   filepath.Join(home, "htb-machines"),
		WindowsDev: defaultWindowsWorkspaceRoot(home),
	})
}

func defaultWorkspaceRoots() WorkspaceRoots {
	roots, err := DefaultWorkspaceRoots()
	if err != nil {
		return WorkspaceRoots{}
	}
	return roots
}

func normalizeWorkspaceRoots(roots WorkspaceRoots) (WorkspaceRoots, error) {
	roots.Dev = filepath.Clean(strings.TrimSpace(roots.Dev))
	roots.HTBLinux = filepath.Clean(strings.TrimSpace(roots.HTBLinux))
	if strings.TrimSpace(roots.WindowsDev) != "" {
		roots.WindowsDev = filepath.Clean(strings.TrimSpace(roots.WindowsDev))
		if !filepath.IsAbs(roots.WindowsDev) {
			return WorkspaceRoots{}, errors.New("windows workcell root is unsafe")
		}
	}
	if !filepath.IsAbs(roots.Dev) || !filepath.IsAbs(roots.HTBLinux) || roots.Dev == roots.HTBLinux || isWindowsMount(roots.Dev) || isWindowsMount(roots.HTBLinux) {
		return WorkspaceRoots{}, errors.New("linux workcell roots are unsafe")
	}
	return roots, nil
}

func validateLinuxWorkcellPath(path string, roots WorkspaceRoots) (string, error) {
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return "", err
	}
	roots, err = normalizeWorkspaceRoots(roots)
	if err != nil {
		return "", err
	}
	if (!pathInside(roots.Dev, validated) || filepath.Clean(validated) == roots.Dev) && (!pathInside(roots.HTBLinux, validated) || filepath.Clean(validated) == roots.HTBLinux) {
		return "", errors.New("linux-workcell path must stay under a registered Linux root")
	}
	return validated, nil
}

func validateWorkspaceConfiguration(workspace Workspace, roots WorkspaceRoots, config WorkspaceConfiguration) (WorkspaceConfiguration, error) {
	if workspace.Profile == WorkspaceProfileWindowsWorkcell {
		if config.Mode != "" && config.Mode != WorkspaceModeDev || roots.WindowsDev == "" || !pathInside(roots.WindowsDev, workspace.Path) || filepath.Clean(workspace.Path) == filepath.Clean(roots.WindowsDev) {
			return WorkspaceConfiguration{}, errors.New("windows workcell requires dev mode under its registered root")
		}
		return WorkspaceConfiguration{Mode: WorkspaceModeDev}, nil
	}
	if workspace.Profile != WorkspaceProfileLinuxWorkcell {
		return WorkspaceConfiguration{}, errors.New("workspace profile is not configurable")
	}
	config.Mode = WorkspaceMode(strings.TrimSpace(string(config.Mode)))
	if config.Mode == "" {
		config.Mode = WorkspaceModeDev
	}
	switch config.Mode {
	case WorkspaceModeDev:
		if !pathInside(roots.Dev, workspace.Path) || filepath.Clean(workspace.Path) == filepath.Clean(roots.Dev) {
			return WorkspaceConfiguration{}, errors.New("dev mode requires a workspace under the local development root")
		}
		return WorkspaceConfiguration{Mode: WorkspaceModeDev}, nil
	case WorkspaceModeHTBLinux:
		if !pathInside(roots.HTBLinux, workspace.Path) || filepath.Clean(workspace.Path) == filepath.Clean(roots.HTBLinux) {
			return WorkspaceConfiguration{}, errors.New("htb-linux mode requires a workspace under the local HTB root")
		}
		config.MachineName = strings.TrimSpace(config.MachineName)
		config.TargetIP = strings.TrimSpace(config.TargetIP)
		config.Difficulty = strings.ToUpper(strings.TrimSpace(config.Difficulty))
		config.OS = strings.ToUpper(strings.TrimSpace(config.OS))
		config.VPNInterface = strings.TrimSpace(config.VPNInterface)
		if !machineNamePattern.MatchString(config.MachineName) {
			return WorkspaceConfiguration{}, errors.New("HTB machine name is invalid")
		}
		if strings.Contains(config.TargetIP, "/") {
			return WorkspaceConfiguration{}, errors.New("HTB target must be one IPv4 address")
		}
		ip := net.ParseIP(config.TargetIP)
		if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
			return WorkspaceConfiguration{}, errors.New("HTB target must be one IPv4 address")
		}
		config.TargetIP = ip.To4().String()
		if config.Difficulty != "EASY" && config.Difficulty != "MEDIUM" && config.Difficulty != "HARD" {
			return WorkspaceConfiguration{}, errors.New("HTB difficulty is invalid")
		}
		if config.OS != "LINUX" {
			return WorkspaceConfiguration{}, errors.New("HTB operating system must be LINUX")
		}
		if !vpnInterfacePattern.MatchString(config.VPNInterface) {
			return WorkspaceConfiguration{}, errors.New("HTB VPN interface is invalid")
		}
		return config, nil
	default:
		return WorkspaceConfiguration{}, errors.New("workspace mode is invalid")
	}
}

func (r *WorkspaceRegistry) AddProfile(path string, profile WorkspaceProfile) (Workspace, bool, error) {
	if r == nil || r.db == nil {
		return Workspace{}, false, errors.New("workspace registry is unavailable")
	}
	if profile != WorkspaceProfileSandbox && profile != WorkspaceProfileLinuxWorkcell && profile != WorkspaceProfileWindowsWorkcell {
		return Workspace{}, false, errors.New("workspace profile is invalid")
	}
	validated := ""
	var err error
	if profile == WorkspaceProfileLinuxWorkcell {
		validated, err = validateLinuxWorkcellPath(path, r.roots)
	} else if profile == WorkspaceProfileWindowsWorkcell {
		validated, err = validateWindowsWorkcellPath(path, r.roots.WindowsDev)
	} else {
		validated, err = ValidateRegisteredWorkspace(path)
	}
	if err != nil {
		return Workspace{}, false, err
	}
	if existing, lookupErr := r.byPath(validated); lookupErr == nil {
		if existing.Profile != profile {
			return Workspace{}, false, errors.New("workspace is already registered with another profile")
		}
		return existing, false, nil
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return Workspace{}, false, errors.New("workspace registry lookup failed")
	}
	id, err := newWorkspaceID()
	if err != nil {
		return Workspace{}, false, errors.New("workspace id generation failed")
	}
	now := r.now().UTC()
	tx, err := r.db.Begin()
	if err != nil {
		return Workspace{}, false, errors.New("workspace registration failed")
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO workspaces(workspace_id,path,created_at,updated_at) VALUES(?,?,?,?)`, id, validated, now.UnixNano(), now.UnixNano()); err != nil {
		return Workspace{}, false, errors.New("workspace registration failed")
	}
	if _, err := tx.Exec(`INSERT INTO workspace_configs(workspace_id,profile,mode,machine_name,target_ip,difficulty,os,vpn_interface,authorization_revision) VALUES(?,?,?,?,?,?,?,?,0)`, id, profile, WorkspaceModeDev, "", "", "", "", ""); err != nil {
		return Workspace{}, false, errors.New("workspace registration failed")
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, false, errors.New("workspace registration failed")
	}
	return r.decorateWorkspace(Workspace{ID: id, Path: validated, Profile: profile, Mode: WorkspaceModeDev, NetworkPosture: networkPosture(profile), CreatedAt: now, UpdatedAt: now}), true, nil
}

func (r *WorkspaceRegistry) Get(id string) (Workspace, error) {
	if r == nil || r.db == nil || !workspaceIDPattern.MatchString(id) {
		return Workspace{}, errors.New("workspace id is invalid")
	}
	workspace, err := scanWorkspace(r.db.QueryRow(workspaceSelect+` WHERE w.workspace_id=?`, id))
	if err != nil {
		return Workspace{}, errors.New("workspace not found")
	}
	workspace = r.decorateWorkspace(workspace)
	if err := r.revalidate(workspace); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (r *WorkspaceRegistry) Configure(id string, config WorkspaceConfiguration) (Workspace, error) {
	if r == nil || r.db == nil || !workspaceIDPattern.MatchString(id) {
		return Workspace{}, errors.New("workspace id is invalid")
	}
	workspace, err := scanWorkspace(r.db.QueryRow(workspaceSelect+` WHERE w.workspace_id=?`, id))
	if err != nil {
		return Workspace{}, errors.New("workspace not found")
	}
	switch workspace.Profile {
	case WorkspaceProfileLinuxWorkcell:
		if _, err := validateLinuxWorkcellPath(workspace.Path, r.roots); err != nil {
			return Workspace{}, err
		}
	case WorkspaceProfileWindowsWorkcell:
		if _, err := validateWindowsWorkcellPath(workspace.Path, r.roots.WindowsDev); err != nil {
			return Workspace{}, err
		}
	default:
		return Workspace{}, errors.New("workspace profile is not configurable")
	}
	config, err = validateWorkspaceConfiguration(workspace, r.roots, config)
	if err != nil {
		return Workspace{}, err
	}
	if workspace.Mode == config.Mode && workspace.MachineName == config.MachineName && workspace.TargetIP == config.TargetIP && workspace.Difficulty == config.Difficulty && workspace.OS == config.OS && workspace.VPNInterface == config.VPNInterface && (workspace.Mode != WorkspaceModeHTBLinux || workspace.AuthorizationRevision > 0) {
		if workspace.Mode == WorkspaceModeHTBLinux {
			if err := writeWorkspaceAuthorizationRevision(workspace.Path, workspace.AuthorizationRevision); err != nil {
				return Workspace{}, err
			}
		}
		return workspace, nil
	}
	revision := workspace.AuthorizationRevision
	if config.Mode == WorkspaceModeHTBLinux {
		if revision == 0 || workspace.TargetIP != config.TargetIP {
			revision++
		}
	} else {
		revision = 0
	}
	now := r.now().UTC()
	tx, err := r.db.Begin()
	if err != nil {
		return Workspace{}, errors.New("workspace configuration failed")
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE workspace_configs SET mode=?,machine_name=?,target_ip=?,difficulty=?,os=?,vpn_interface=?,authorization_revision=? WHERE workspace_id=?`, config.Mode, config.MachineName, config.TargetIP, config.Difficulty, config.OS, config.VPNInterface, revision, id)
	if err != nil {
		return Workspace{}, errors.New("workspace configuration failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Workspace{}, errors.New("workspace not found")
	}
	if _, err := tx.Exec(`UPDATE workspaces SET updated_at=? WHERE workspace_id=?`, now.UnixNano(), id); err != nil {
		return Workspace{}, errors.New("workspace configuration failed")
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, errors.New("workspace configuration failed")
	}
	configured, err := r.Get(id)
	if err != nil {
		return Workspace{}, err
	}
	if configured.Mode == WorkspaceModeHTBLinux {
		if err := writeWorkspaceAuthorizationRevision(configured.Path, configured.AuthorizationRevision); err != nil {
			return Workspace{}, err
		}
	}
	return configured, nil
}

func (r *WorkspaceRegistry) Retarget(id, target, vpnInterface string) (Workspace, error) {
	workspace, err := r.Get(id)
	if err != nil {
		return Workspace{}, err
	}
	if workspace.Mode != WorkspaceModeHTBLinux {
		return Workspace{}, errors.New("workspace is not an authorized HTB lab")
	}
	return r.Configure(id, WorkspaceConfiguration{
		Mode: workspace.Mode, MachineName: workspace.MachineName, TargetIP: target,
		Difficulty: workspace.Difficulty, OS: workspace.OS, VPNInterface: vpnInterface,
	})
}

func (r *WorkspaceRegistry) revalidate(workspace Workspace) error {
	switch workspace.Profile {
	case WorkspaceProfileSandbox:
		_, err := ValidateRegisteredWorkspace(workspace.Path)
		return err
	case WorkspaceProfileLinuxWorkcell:
		if _, err := validateLinuxWorkcellPath(workspace.Path, r.roots); err != nil {
			return err
		}
		_, err := validateWorkspaceConfiguration(workspace, r.roots, WorkspaceConfiguration{
			Mode: workspace.Mode, MachineName: workspace.MachineName, TargetIP: workspace.TargetIP,
			Difficulty: workspace.Difficulty, OS: workspace.OS, VPNInterface: workspace.VPNInterface,
		})
		return err
	case WorkspaceProfileWindowsWorkcell:
		if _, err := validateWindowsWorkcellPath(workspace.Path, r.roots.WindowsDev); err != nil {
			return err
		}
		_, err := validateWorkspaceConfiguration(workspace, r.roots, WorkspaceConfiguration{Mode: workspace.Mode})
		return err
	default:
		return errors.New("workspace profile is invalid")
	}
}

func networkPosture(profile WorkspaceProfile) string {
	if profile == WorkspaceProfileLinuxWorkcell {
		return LinuxWorkcellNetworkPosture
	}
	if profile == WorkspaceProfileWindowsWorkcell {
		return WindowsWorkcellNetworkPosture
	}
	return "isolated_no_network"
}
