//go:build !windows

package edgeclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

type ProjectToolboxState string

const (
	ProjectToolboxCreated ProjectToolboxState = "created"
	ProjectToolboxRunning ProjectToolboxState = "running"
	ProjectToolboxStopped ProjectToolboxState = "stopped"
	ProjectToolboxUnknown ProjectToolboxState = "unknown"

	projectToolboxStateDirectory = "project-toolboxes"
	projectToolboxBaseImage      = "docker.io/library/debian:bookworm-slim"
	projectToolboxLabelKey       = "mcp.devbox.toolbox"
	projectToolboxOutputLimit    = 24 << 10
)

var (
	projectToolboxIDPattern      = regexp.MustCompile(`^tb_[a-f0-9]{32}$`)
	projectToolboxImageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	projectToolboxEnvKeyPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	ErrProjectToolboxNotFound    = errors.New("project toolbox not found")
	ErrProjectToolboxNotOwned    = errors.New("project toolbox is not owned")
	ErrProjectToolboxUnsafeState = errors.New("project toolbox state is unsafe")
	ErrProjectToolboxUnavailable = errors.New("project toolbox rootless engine is unavailable")
)

type ProjectToolboxManagerConfig struct {
	StateRoot string
	Endpoint  *RootlessContainerEndpoint
	Runner    ContainerCommandRunner
	NewID     func() (string, error)
	Now       func() time.Time
}

type ProjectToolboxManager struct {
	stateRoot string
	endpoint  *RootlessContainerEndpoint
	runner    ContainerCommandRunner
	newID     func() (string, error)
	now       func() time.Time
	mu        sync.Mutex
}

type ProjectToolboxCreateRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
}

type ProjectToolboxStatusRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
}

type ProjectToolboxExecRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	Argv                      []string
	CWD                       string
	Environment               map[string]string
}

type ProjectToolboxCleanupRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
}

type ProjectToolboxSnapshot struct {
	ToolboxID   string
	State       ProjectToolboxState
	BaseImage   string
	BaseImageID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Output      string
	Truncated   bool
}

type projectToolboxRecord struct {
	ToolboxID, WorkspaceID, ProjectAlias, TargetAlias string
	ContainerName, BaseImage, BaseImageID             string
	CreatedAt, UpdatedAt                              time.Time
}

func OpenProjectToolboxManager(config ProjectToolboxManagerConfig) (*ProjectToolboxManager, error) {
	stateRoot := filepath.Clean(strings.TrimSpace(config.StateRoot))
	if !filepath.IsAbs(stateRoot) || stateRoot == string(filepath.Separator) || stateRoot == "." {
		return nil, ErrProjectToolboxUnsafeState
	}
	endpoint := config.Endpoint
	if endpoint == nil || endpoint.Engine != "podman" && endpoint.Engine != "docker" || !filepath.IsAbs(endpoint.Executable) || !filepath.IsAbs(endpoint.SocketPath) {
		return nil, ErrProjectToolboxUnavailable
	}
	root := filepath.Join(stateRoot, projectToolboxStateDirectory)
	if err := os.MkdirAll(root, 0o700); err != nil || os.Chmod(root, 0o700) != nil || rejectSymlinkPath(root) != nil {
		return nil, ErrProjectToolboxUnsafeState
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUIDPortable(info) {
		return nil, ErrProjectToolboxUnsafeState
	}
	runner := config.Runner
	if runner == nil {
		runner = execContainerCommandRunner{}
	}
	newID := config.NewID
	if newID == nil {
		newID = newProjectToolboxID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ProjectToolboxManager{stateRoot: root, endpoint: endpoint, runner: runner, newID: newID, now: now}, nil
}

func (manager *ProjectToolboxManager) Create(ctx context.Context, request ProjectToolboxCreateRequest) (ProjectToolboxSnapshot, bool, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, false, err
	}
	if record, err := manager.load(request.Workspace.ID); err == nil {
		snapshot, statusErr := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
		if statusErr == nil && (snapshot.State == ProjectToolboxStopped || snapshot.State == ProjectToolboxCreated) {
			if _, startErr := manager.run(ctx, "start", record.ContainerName); startErr != nil {
				return ProjectToolboxSnapshot{}, true, ErrProjectToolboxUnavailable
			}
			snapshot, statusErr = manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
		}
		return snapshot, true, statusErr
	} else if !errors.Is(err, ErrProjectToolboxNotFound) {
		return ProjectToolboxSnapshot{}, false, err
	}
	toolboxID, err := manager.newID()
	if err != nil || !projectToolboxIDPattern.MatchString(toolboxID) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	containerName := "mcp-toolbox-" + strings.TrimPrefix(toolboxID, "tb_")
	if _, err := manager.run(ctx, "pull", projectToolboxBaseImage); err != nil {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	imageOutput, err := manager.run(ctx, "image", "inspect", "--format", "{{.Id}}", projectToolboxBaseImage)
	imageID := strings.TrimSpace(string(imageOutput))
	if err != nil || !projectToolboxImageIDPattern.MatchString(imageID) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	createOutput, err := manager.run(ctx, "create", "--name", containerName, "--label", projectToolboxLabelKey+"="+toolboxID,
		"--volume", request.Workspace.Path+":/workspace:rw", "--workdir", "/workspace", imageID, "sleep", "infinity")
	containerID := strings.TrimSpace(string(createOutput))
	if err != nil || !containerResourceIDPattern.MatchString(containerID) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	created := manager.now().UTC()
	record := projectToolboxRecord{
		ToolboxID: toolboxID, WorkspaceID: request.Workspace.ID, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias,
		ContainerName: containerName, BaseImage: projectToolboxBaseImage, BaseImageID: imageID, CreatedAt: created, UpdatedAt: created,
	}
	if err := manager.save(record); err != nil {
		_, _ = manager.run(context.Background(), "rm", "-f", containerName)
		return ProjectToolboxSnapshot{}, false, err
	}
	if _, err := manager.run(ctx, "start", containerName); err != nil {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	snapshot, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	return snapshot, false, err
}

func (manager *ProjectToolboxManager) Status(ctx context.Context, request ProjectToolboxStatusRequest) (ProjectToolboxSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	return manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
}

func (manager *ProjectToolboxManager) Exec(ctx context.Context, request ProjectToolboxExecRequest) (ProjectToolboxSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil || len(request.Argv) == 0 || len(request.Argv) > 128 {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	snapshot, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if snapshot.State == ProjectToolboxStopped {
		if _, err := manager.run(ctx, "start", record.ContainerName); err != nil {
			return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
		}
	} else if snapshot.State != ProjectToolboxRunning {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
	}
	args := []string{"exec"}
	cwd, err := normalizeProjectToolboxCWD(request.CWD)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	args = append(args, "--workdir", cwd)
	keys := make([]string, 0, len(request.Environment))
	for key := range request.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := request.Environment[key]
		if !projectToolboxEnvKeyPattern.MatchString(key) || projectToolboxReservedEnvironmentKey(key) || projectToolboxSecretShaped(value) {
			return ProjectToolboxSnapshot{}, ErrProjectToolboxUnsafeState
		}
		args = append(args, "--env", key+"="+value)
	}
	for _, argument := range request.Argv {
		if argument == "" || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || projectToolboxSecretShaped(argument) {
			return ProjectToolboxSnapshot{}, ErrProjectToolboxUnsafeState
		}
	}
	args = append(args, record.ContainerName)
	args = append(args, request.Argv...)
	output, err := manager.run(ctx, args...)
	if err != nil {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
	}
	truncated := len(output) > projectToolboxOutputLimit
	if truncated {
		output = output[:projectToolboxOutputLimit]
	}
	redacted, _ := policy.Redact(string(output))
	record.UpdatedAt = manager.now().UTC()
	if err := manager.save(record); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	snapshot.State = ProjectToolboxRunning
	snapshot.UpdatedAt = record.UpdatedAt
	snapshot.Output = redacted
	snapshot.Truncated = truncated
	return snapshot, nil
}

func (manager *ProjectToolboxManager) Cleanup(ctx context.Context, request ProjectToolboxCleanupRequest) (bool, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return false, err
	}
	record, err := manager.load(request.Workspace.ID)
	if errors.Is(err, ErrProjectToolboxNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := manager.verifyOwnership(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return false, err
	}
	if _, err := manager.run(ctx, "rm", "-f", record.ContainerName); err != nil {
		return false, ErrProjectToolboxUnavailable
	}
	if err := os.Remove(manager.recordPath(request.Workspace.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, ErrProjectToolboxUnsafeState
	}
	return true, nil
}

func (manager *ProjectToolboxManager) status(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) (ProjectToolboxSnapshot, error) {
	if err := manager.verifyOwnership(ctx, record, alias, target, workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	output, err := manager.run(ctx, "inspect", "--format", "{{.State.Status}}|{{.State.Running}}", record.ContainerName)
	if err != nil {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
	}
	state := ProjectToolboxUnknown
	switch strings.TrimSpace(string(output)) {
	case "running|true":
		state = ProjectToolboxRunning
	case "created|false":
		state = ProjectToolboxCreated
	case "exited|false", "stopped|false":
		state = ProjectToolboxStopped
	}
	return ProjectToolboxSnapshot{ToolboxID: record.ToolboxID, State: state, BaseImage: record.BaseImage, BaseImageID: record.BaseImageID, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}, nil
}

func (manager *ProjectToolboxManager) verifyOwnership(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) error {
	if record.WorkspaceID != workspace.ID || record.ProjectAlias != alias || record.TargetAlias != target || !projectToolboxIDPattern.MatchString(record.ToolboxID) || record.ContainerName != "mcp-toolbox-"+strings.TrimPrefix(record.ToolboxID, "tb_") {
		return ErrProjectToolboxNotOwned
	}
	output, err := manager.run(ctx, "inspect", "--format", `{{index .Config.Labels "`+projectToolboxLabelKey+`"}}|{{.Image}}`, record.ContainerName)
	if err != nil || strings.TrimSpace(string(output)) != record.ToolboxID+"|"+record.BaseImageID {
		return ErrProjectToolboxNotOwned
	}
	mountOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Mounts}}", record.ContainerName)
	var mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	if err != nil || json.Unmarshal(mountOutput, &mounts) != nil || len(mounts) != 1 || mounts[0].Type != "bind" || mounts[0].Source != workspace.Path || mounts[0].Destination != "/workspace" || !mounts[0].RW {
		return ErrProjectToolboxNotOwned
	}
	return nil
}

func (manager *ProjectToolboxManager) run(ctx context.Context, args ...string) ([]byte, error) {
	prefixed := append(rootlessEnginePrefix(manager.endpoint), args...)
	return manager.runner.Run(ctx, manager.endpoint.Executable, prefixed, rootlessContainerClientEnvironment(manager.endpoint, openCodeDefaultToolPath))
}

func (manager *ProjectToolboxManager) recordPath(workspaceID string) string {
	return filepath.Join(manager.stateRoot, workspaceID+".json")
}

func (manager *ProjectToolboxManager) load(workspaceID string) (projectToolboxRecord, error) {
	if !workspaceIDPattern.MatchString(workspaceID) {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	file := manager.recordPath(workspaceID)
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return projectToolboxRecord{}, ErrProjectToolboxNotFound
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() < 2 || info.Size() > 16<<10 {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	var record projectToolboxRecord
	if json.Unmarshal(data, &record) != nil || record.WorkspaceID != workspaceID || !projectToolboxIDPattern.MatchString(record.ToolboxID) || !projectToolboxImageIDPattern.MatchString(record.BaseImageID) || record.BaseImage != projectToolboxBaseImage || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	return record, nil
}

func (manager *ProjectToolboxManager) save(record projectToolboxRecord) error {
	data, err := json.Marshal(record)
	if err != nil || len(data) > 16<<10 {
		return ErrProjectToolboxUnsafeState
	}
	temporary, err := os.CreateTemp(manager.stateRoot, ".toolbox-*.tmp")
	if err != nil {
		return ErrProjectToolboxUnsafeState
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return ErrProjectToolboxUnsafeState
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return ErrProjectToolboxUnsafeState
	}
	if err := temporary.Sync(); err != nil || temporary.Close() != nil {
		return ErrProjectToolboxUnsafeState
	}
	if err := os.Rename(temporaryName, manager.recordPath(record.WorkspaceID)); err != nil {
		return ErrProjectToolboxUnsafeState
	}
	directory, err := os.Open(manager.stateRoot)
	if err != nil {
		return ErrProjectToolboxUnsafeState
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil || closeErr != nil {
		return ErrProjectToolboxUnsafeState
	}
	return nil
}

func validateProjectToolboxBinding(alias, target string, workspace Workspace) error {
	if !projectAliasPattern.MatchString(alias) || !projectTargetPattern.MatchString(target) || !workspaceIDPattern.MatchString(workspace.ID) || workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev || !filepath.IsAbs(workspace.Path) || strings.ContainsAny(workspace.Path, ":\x00\r\n") {
		return ErrProjectToolboxUnsafeState
	}
	info, err := os.Lstat(workspace.Path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
		return ErrProjectToolboxUnsafeState
	}
	return nil
}

func normalizeProjectToolboxCWD(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return "/workspace", nil
	}
	if path.IsAbs(value) || strings.ContainsAny(value, "\\\x00") {
		return "", ErrProjectToolboxUnsafeState
	}
	value = path.Clean(value)
	if value == ".." || strings.HasPrefix(value, "../") {
		return "", ErrProjectToolboxUnsafeState
	}
	return "/workspace/" + value, nil
}

func projectToolboxSecretShaped(value string) bool {
	redacted, changed := policy.Redact(value)
	return changed || redacted != value
}

func projectToolboxReservedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR",
		"DOCKER_HOST", "CONTAINER_HOST", "DOCKER_CONFIG":
		return true
	}
	if strings.HasPrefix(upper, "XDG_") || strings.HasPrefix(upper, "MCP_DEVBOX_") {
		return true
	}
	for _, fragment := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE_KEY", "API_KEY"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func newProjectToolboxID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate toolbox id: %w", err)
	}
	return "tb_" + hex.EncodeToString(buffer), nil
}
