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
	"strconv"
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

	projectToolboxStateDirectory  = "project-toolboxes"
	projectToolboxBaseImage       = "docker.io/library/debian:bookworm-slim"
	projectToolboxLabelKey        = "mcp.devbox.toolbox"
	projectToolboxOutputLimit     = 24 << 10
	projectToolboxContainerSocket = "/run/mcp-devbox/container.sock"
)

var (
	projectToolboxIDPattern          = regexp.MustCompile(`^tb_[a-f0-9]{32}$`)
	projectToolboxImageIDPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	projectToolboxEnvKeyPattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	projectToolboxServiceIDPattern   = regexp.MustCompile(`^ts_[a-f0-9]{32}$`)
	projectToolboxServiceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	ErrProjectToolboxNotFound        = errors.New("project toolbox not found")
	ErrProjectToolboxNotOwned        = errors.New("project toolbox is not owned")
	ErrProjectToolboxUnsafeState     = errors.New("project toolbox state is unsafe")
	ErrProjectToolboxUnavailable     = errors.New("project toolbox rootless engine is unavailable")
)

type ProjectToolboxManagerConfig struct {
	StateRoot    string
	Endpoint     *RootlessContainerEndpoint
	Runner       ContainerCommandRunner
	NewID        func() (string, error)
	NewServiceID func() (string, error)
	NewHarnessID func() (string, error)
	Now          func() time.Time
}

type ProjectToolboxManager struct {
	stateRoot    string
	endpoint     *RootlessContainerEndpoint
	runner       ContainerCommandRunner
	newID        func() (string, error)
	newServiceID func() (string, error)
	newHarnessID func() (string, error)
	now          func() time.Time
	mu           sync.Mutex
}

type ProjectToolboxCreateRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	CPUMillis                 int
	MemoryMiB                 int
	ProcessLimit              int
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

type ProjectToolboxRepairRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
}

type ProjectToolboxServiceStartRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	Name                      string
	Argv                      []string
	CWD                       string
	Environment               map[string]string
}

type ProjectToolboxServiceRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	ServiceID                 string
}

type ProjectToolboxServiceSnapshot struct {
	ServiceID string
	Name      string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ProjectToolboxSnapshot struct {
	ToolboxID       string
	State           ProjectToolboxState
	BaseImage       string
	BaseImageID     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Output          string
	Truncated       bool
	CPUMillis       int
	MemoryMiB       int
	ProcessLimit    int
	ContainerAccess bool
	WritableBytes   int64
	RootFSBytes     int64
}

type projectToolboxRecord struct {
	ToolboxID, WorkspaceID, ProjectAlias, TargetAlias string
	ContainerName, BaseImage, BaseImageID             string
	CreatedAt, UpdatedAt                              time.Time
	CPUMillis, MemoryMiB, ProcessLimit                int
	Services                                          []projectToolboxServiceRecord `json:"services,omitempty"`
	BrowserHarnessRuns                                []projectBrowserHarnessRecord `json:"browser_harness_runs,omitempty"`
}

type projectToolboxServiceRecord struct {
	ServiceID string    `json:"service_id"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	newServiceID := config.NewServiceID
	if newServiceID == nil {
		newServiceID = newProjectToolboxServiceID
	}
	newHarnessID := config.NewHarnessID
	if newHarnessID == nil {
		newHarnessID = newProjectBrowserHarnessID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ProjectToolboxManager{stateRoot: root, endpoint: endpoint, runner: runner, newID: newID, newServiceID: newServiceID, newHarnessID: newHarnessID, now: now}, nil
}

func (manager *ProjectToolboxManager) Create(ctx context.Context, request ProjectToolboxCreateRequest) (ProjectToolboxSnapshot, bool, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	request.CPUMillis, request.MemoryMiB, request.ProcessLimit = normalizeProjectToolboxResourceLimits(request.CPUMillis, request.MemoryMiB, request.ProcessLimit)
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, false, err
	}
	if !validProjectToolboxResourceLimits(request.CPUMillis, request.MemoryMiB, request.ProcessLimit) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	if record, err := manager.load(request.Workspace.ID); err == nil {
		if record.CPUMillis != request.CPUMillis || record.MemoryMiB != request.MemoryMiB || record.ProcessLimit != request.ProcessLimit {
			return ProjectToolboxSnapshot{}, true, ErrProjectToolboxUnsafeState
		}
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
	imageID, normalizeErr := normalizeProjectToolboxImageID(string(imageOutput))
	if err != nil || normalizeErr != nil {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	createOutput, err := manager.run(ctx, "create", "--name", containerName, "--label", projectToolboxLabelKey+"="+toolboxID,
		"--cpus", fmt.Sprintf("%.3f", float64(request.CPUMillis)/1000), "--memory", fmt.Sprintf("%dm", request.MemoryMiB), "--pids-limit", fmt.Sprintf("%d", request.ProcessLimit),
		"--volume", manager.endpoint.SocketPath+":"+projectToolboxContainerSocket+":rw",
		"--env", "DOCKER_HOST=unix://"+projectToolboxContainerSocket, "--env", "CONTAINER_HOST=unix://"+projectToolboxContainerSocket,
		"--env", "MCP_DEVBOX_CONTAINER_ENGINE="+manager.endpoint.Engine, "--env", "MCP_DEVBOX_CONTAINER_LABEL=mcp.devbox.toolbox.parent="+toolboxID,
		"--env", "COMPOSE_PROJECT_NAME="+projectToolboxComposeProject(toolboxID),
		"--volume", request.Workspace.Path+":/workspace:rw", "--workdir", "/workspace", imageID, "sleep", "infinity")
	containerID := strings.TrimSpace(string(createOutput))
	if err != nil || !containerResourceIDPattern.MatchString(containerID) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	created := manager.now().UTC()
	record := projectToolboxRecord{
		ToolboxID: toolboxID, WorkspaceID: request.Workspace.ID, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias,
		ContainerName: containerName, BaseImage: projectToolboxBaseImage, BaseImageID: imageID, CreatedAt: created, UpdatedAt: created,
		CPUMillis: request.CPUMillis, MemoryMiB: request.MemoryMiB, ProcessLimit: request.ProcessLimit,
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

func normalizeProjectToolboxImageID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) == 64 {
		value = "sha256:" + value
	}
	if !projectToolboxImageIDPattern.MatchString(value) {
		return "", ErrProjectToolboxUnavailable
	}
	return value, nil
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

func (manager *ProjectToolboxManager) Repair(ctx context.Context, request ProjectToolboxRepairRequest) (ProjectToolboxSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	snapshot, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if snapshot.State == ProjectToolboxStopped || snapshot.State == ProjectToolboxCreated {
		if _, err := manager.run(ctx, "start", record.ContainerName); err != nil {
			return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
		}
		return manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	}
	if snapshot.State != ProjectToolboxRunning {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
	}
	return snapshot, nil
}

func (manager *ProjectToolboxManager) ServiceStart(ctx context.Context, request ProjectToolboxServiceStartRequest) (ProjectToolboxServiceSnapshot, bool, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil ||
		!projectToolboxServiceNamePattern.MatchString(request.Name) || len(request.Argv) == 0 || len(request.Argv) > 128 {
		return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, false, err
	}
	if _, err := manager.ensureRunning(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxServiceSnapshot{}, false, err
	}
	for i := range record.Services {
		if record.Services[i].Name == request.Name {
			snapshot, statusErr := manager.serviceStatus(ctx, &record, i, request.Workspace)
			if statusErr != nil || snapshot.State == "running" {
				return snapshot, true, statusErr
			}
			record.Services = append(record.Services[:i], record.Services[i+1:]...)
			break
		}
	}
	serviceID, err := manager.newServiceID()
	if err != nil || !projectToolboxServiceIDPattern.MatchString(serviceID) {
		return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	cwd, err := normalizeProjectToolboxCWD(request.CWD)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, false, err
	}
	args := []string{"exec", "--detach", "--workdir", cwd}
	keys := make([]string, 0, len(request.Environment))
	for key := range request.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := request.Environment[key]
		if !projectToolboxEnvKeyPattern.MatchString(key) || projectToolboxReservedEnvironmentKey(key) || projectToolboxSecretShaped(value) {
			return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnsafeState
		}
		args = append(args, "--env", key+"="+value)
	}
	for _, argument := range request.Argv {
		if argument == "" || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || projectToolboxSecretShaped(argument) {
			return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnsafeState
		}
	}
	now := manager.now().UTC()
	record.Services = append(record.Services, projectToolboxServiceRecord{ServiceID: serviceID, Name: request.Name, State: "starting", CreatedAt: now, UpdatedAt: now})
	record.UpdatedAt = now
	if err := manager.save(record); err != nil {
		return ProjectToolboxServiceSnapshot{}, false, err
	}
	args = append(args, record.ContainerName, "/bin/sh", "-c", projectToolboxServiceStartScript, "mcp-toolbox-service-start", serviceID)
	args = append(args, request.Argv...)
	if _, err := manager.run(ctx, args...); err != nil {
		record.Services = record.Services[:len(record.Services)-1]
		record.UpdatedAt = manager.now().UTC()
		if saveErr := manager.save(record); saveErr != nil {
			return ProjectToolboxServiceSnapshot{}, false, saveErr
		}
		return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	index := len(record.Services) - 1
	snapshot, err := manager.serviceStatus(ctx, &record, index, request.Workspace)
	return snapshot, false, err
}

func (manager *ProjectToolboxManager) ServiceStatus(ctx context.Context, request ProjectToolboxServiceRequest) (ProjectToolboxServiceSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	record, index, err := manager.serviceRecord(request)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	toolbox, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	if toolbox.State == ProjectToolboxStopped || toolbox.State == ProjectToolboxCreated {
		return manager.markServiceStopped(&record, index)
	}
	if toolbox.State != ProjectToolboxRunning {
		return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnavailable
	}
	return manager.serviceStatus(ctx, &record, index, request.Workspace)
}

func (manager *ProjectToolboxManager) ServiceStop(ctx context.Context, request ProjectToolboxServiceRequest) (ProjectToolboxServiceSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	record, index, err := manager.serviceRecord(request)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	toolbox, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	if toolbox.State == ProjectToolboxStopped || toolbox.State == ProjectToolboxCreated {
		return manager.markServiceStopped(&record, index)
	}
	if toolbox.State != ProjectToolboxRunning {
		return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnavailable
	}
	if record.Services[index].State != "stopped" {
		output, runErr := manager.run(ctx, "exec", record.ContainerName, "/bin/sh", "-c", projectToolboxServiceStopScript, "mcp-toolbox-service-stop", request.ServiceID)
		if runErr != nil || strings.TrimSpace(string(output)) != "stopped" {
			return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnavailable
		}
	}
	record.Services[index].State = "stopped"
	record.Services[index].UpdatedAt = manager.now().UTC()
	record.UpdatedAt = record.Services[index].UpdatedAt
	if err := manager.save(record); err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	return toolboxServiceSnapshot(record.Services[index]), nil
}

func (manager *ProjectToolboxManager) markServiceStopped(record *projectToolboxRecord, index int) (ProjectToolboxServiceSnapshot, error) {
	if record == nil || index < 0 || index >= len(record.Services) {
		return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnsafeState
	}
	record.Services[index].State = "stopped"
	record.Services[index].UpdatedAt = manager.now().UTC()
	record.UpdatedAt = record.Services[index].UpdatedAt
	if err := manager.save(*record); err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	return toolboxServiceSnapshot(record.Services[index]), nil
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

func (manager *ProjectToolboxManager) serviceRecord(request ProjectToolboxServiceRequest) (projectToolboxRecord, int, error) {
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil || !projectToolboxServiceIDPattern.MatchString(request.ServiceID) {
		return projectToolboxRecord{}, -1, ErrProjectToolboxUnsafeState
	}
	record, err := manager.load(request.Workspace.ID)
	if err != nil {
		return projectToolboxRecord{}, -1, err
	}
	for index := range record.Services {
		if record.Services[index].ServiceID == request.ServiceID {
			return record, index, nil
		}
	}
	return projectToolboxRecord{}, -1, ErrProjectToolboxNotFound
}

func (manager *ProjectToolboxManager) serviceStatus(ctx context.Context, record *projectToolboxRecord, index int, workspace Workspace) (ProjectToolboxServiceSnapshot, error) {
	if record == nil || index < 0 || index >= len(record.Services) {
		return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnsafeState
	}
	service := &record.Services[index]
	output, err := manager.run(ctx, "exec", record.ContainerName, "/bin/sh", "-c", projectToolboxServiceStatusScript, "mcp-toolbox-service-status", service.ServiceID)
	state := strings.TrimSpace(string(output))
	if err != nil || state != "running" && state != "stopped" {
		return ProjectToolboxServiceSnapshot{}, ErrProjectToolboxUnavailable
	}
	service.State = state
	service.UpdatedAt = manager.now().UTC()
	record.UpdatedAt = service.UpdatedAt
	if err := manager.save(*record); err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	return toolboxServiceSnapshot(*service), nil
}

func (manager *ProjectToolboxManager) ensureRunning(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) (ProjectToolboxSnapshot, error) {
	snapshot, err := manager.status(ctx, record, alias, target, workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if snapshot.State == ProjectToolboxStopped || snapshot.State == ProjectToolboxCreated {
		if _, err := manager.run(ctx, "start", record.ContainerName); err != nil {
			return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
		}
		return manager.status(ctx, record, alias, target, workspace)
	}
	if snapshot.State != ProjectToolboxRunning {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnavailable
	}
	return snapshot, nil
}

func toolboxServiceSnapshot(service projectToolboxServiceRecord) ProjectToolboxServiceSnapshot {
	return ProjectToolboxServiceSnapshot(service)
}

const projectToolboxServiceStartScript = `service=$1; shift; umask 077; root=/var/lib/mcp-devbox/services/$service; mkdir -p "$root"; printf '%s ' "$$" > "$root/identity"; awk '{print $22}' "/proc/$$/stat" >> "$root/identity"; exec "$@" >> "$root/stdout.log" 2>> "$root/stderr.log"`
const projectToolboxServiceStatusScript = `root=/var/lib/mcp-devbox/services/$1; test -r "$root/identity" || { printf 'stopped\n'; exit 0; }; read pid ticks < "$root/identity"; case "$pid:$ticks" in *[!0-9:]*|:*|*:) printf 'stopped\n'; exit 0;; esac; test -r "/proc/$pid/stat" || { printf 'stopped\n'; exit 0; }; current=$(awk '{print $22}' "/proc/$pid/stat"); if test "$current" = "$ticks"; then printf 'running\n'; else printf 'stopped\n'; fi`
const projectToolboxServiceStopScript = `root=/var/lib/mcp-devbox/services/$1; test -r "$root/identity" || { printf 'stopped\n'; exit 0; }; read pid ticks < "$root/identity"; case "$pid:$ticks" in *[!0-9:]*|:*|*:) printf 'stopped\n'; exit 0;; esac; test -r "/proc/$pid/stat" || { printf 'stopped\n'; exit 0; }; current=$(awk '{print $22}' "/proc/$pid/stat"); test "$current" = "$ticks" || { printf 'stopped\n'; exit 0; }; kill -TERM "$pid" 2>/dev/null || true; i=0; while kill -0 "$pid" 2>/dev/null && test "$i" -lt 100; do sleep 0.1; i=$((i+1)); done; kill -KILL "$pid" 2>/dev/null || true; printf 'stopped\n'`

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
	writableBytes, rootFSBytes, err := manager.storage(ctx, record.ContainerName)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	return ProjectToolboxSnapshot{ToolboxID: record.ToolboxID, State: state, BaseImage: record.BaseImage, BaseImageID: record.BaseImageID, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, CPUMillis: record.CPUMillis, MemoryMiB: record.MemoryMiB, ProcessLimit: record.ProcessLimit, ContainerAccess: true, WritableBytes: writableBytes, RootFSBytes: rootFSBytes}, nil
}

func (manager *ProjectToolboxManager) verifyOwnership(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) error {
	if record.WorkspaceID != workspace.ID || record.ProjectAlias != alias || record.TargetAlias != target || !projectToolboxIDPattern.MatchString(record.ToolboxID) || record.ContainerName != "mcp-toolbox-"+strings.TrimPrefix(record.ToolboxID, "tb_") {
		return ErrProjectToolboxNotOwned
	}
	output, err := manager.run(ctx, "inspect", "--format", `{{index .Config.Labels "`+projectToolboxLabelKey+`"}}|{{.Image}}`, record.ContainerName)
	label, rawImageID, found := strings.Cut(strings.TrimSpace(string(output)), "|")
	imageID, normalizeErr := normalizeProjectToolboxImageID(rawImageID)
	if err != nil || !found || label != record.ToolboxID || normalizeErr != nil || imageID != record.BaseImageID {
		return ErrProjectToolboxNotOwned
	}
	mountOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Mounts}}", record.ContainerName)
	var mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	if err != nil || json.Unmarshal(mountOutput, &mounts) != nil || !validProjectToolboxMounts(mounts, workspace.Path, manager.endpoint.SocketPath) {
		return ErrProjectToolboxNotOwned
	}
	resourceOutput, err := manager.run(ctx, "inspect", "--format", "{{.HostConfig.Memory}}|{{.HostConfig.NanoCpus}}|{{.HostConfig.PidsLimit}}", record.ContainerName)
	wantResources := fmt.Sprintf("%d|%d|%d", int64(record.MemoryMiB)*1024*1024, int64(record.CPUMillis)*1000000, record.ProcessLimit)
	if err != nil || strings.TrimSpace(string(resourceOutput)) != wantResources {
		return ErrProjectToolboxNotOwned
	}
	environmentOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Config.Env}}", record.ContainerName)
	var environment []string
	if err != nil || json.Unmarshal(environmentOutput, &environment) != nil || !validProjectToolboxContainerEnvironment(environment, manager.endpoint.Engine, record.ToolboxID) {
		return ErrProjectToolboxNotOwned
	}
	return nil
}

func validProjectToolboxMounts(mounts []struct {
	Type, Source, Destination string
	RW                        bool
}, workspacePath, socketPath string) bool {
	if len(mounts) != 2 {
		return false
	}
	want := map[string]string{"/workspace": workspacePath, projectToolboxContainerSocket: socketPath}
	seen := map[string]bool{}
	for _, mount := range mounts {
		if mount.Type != "bind" || !mount.RW || want[mount.Destination] != mount.Source || seen[mount.Destination] {
			return false
		}
		seen[mount.Destination] = true
	}
	return seen["/workspace"] && seen[projectToolboxContainerSocket]
}

func validProjectToolboxContainerEnvironment(environment []string, engine, toolboxID string) bool {
	want := map[string]string{
		"DOCKER_HOST":                 "unix://" + projectToolboxContainerSocket,
		"CONTAINER_HOST":              "unix://" + projectToolboxContainerSocket,
		"MCP_DEVBOX_CONTAINER_ENGINE": engine,
		"MCP_DEVBOX_CONTAINER_LABEL":  "mcp.devbox.toolbox.parent=" + toolboxID,
		"COMPOSE_PROJECT_NAME":        projectToolboxComposeProject(toolboxID),
	}
	found := map[string]bool{}
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if expected, tracked := want[key]; tracked {
			if !ok || found[key] || value != expected {
				return false
			}
			found[key] = true
		}
	}
	return len(found) == len(want)
}

func projectToolboxComposeProject(toolboxID string) string {
	return "mcp-tb-" + strings.TrimPrefix(toolboxID, "tb_")[:16]
}

func (manager *ProjectToolboxManager) storage(ctx context.Context, containerName string) (int64, int64, error) {
	output, err := manager.run(ctx, "inspect", "--size", "--format", "{{.SizeRw}}|{{.SizeRootFs}}", containerName)
	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if err != nil || len(parts) != 2 {
		return 0, 0, ErrProjectToolboxUnavailable
	}
	writableBytes, writableErr := strconv.ParseInt(parts[0], 10, 64)
	rootFSBytes, rootErr := strconv.ParseInt(parts[1], 10, 64)
	if writableErr != nil || rootErr != nil || writableBytes < 0 || rootFSBytes <= 0 {
		return 0, 0, ErrProjectToolboxUnavailable
	}
	return writableBytes, rootFSBytes, nil
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
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() < 2 || info.Size() > 64<<10 {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	var record projectToolboxRecord
	if json.Unmarshal(data, &record) != nil || record.WorkspaceID != workspaceID || !projectToolboxIDPattern.MatchString(record.ToolboxID) || !projectToolboxImageIDPattern.MatchString(record.BaseImageID) || record.BaseImage != projectToolboxBaseImage || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || !validProjectToolboxResourceLimits(record.CPUMillis, record.MemoryMiB, record.ProcessLimit) {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	if len(record.Services) > 64 || len(record.BrowserHarnessRuns) > 128 {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for _, service := range record.Services {
		if !projectToolboxServiceIDPattern.MatchString(service.ServiceID) || !projectToolboxServiceNamePattern.MatchString(service.Name) ||
			(service.State != "starting" && service.State != "running" && service.State != "stopped") || service.CreatedAt.IsZero() || service.UpdatedAt.Before(service.CreatedAt) || seenIDs[service.ServiceID] || seenNames[service.Name] {
			return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
		}
		seenIDs[service.ServiceID], seenNames[service.Name] = true, true
	}
	if !validProjectBrowserHarnessRecords(record.BrowserHarnessRuns) {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	return record, nil
}

func validProjectToolboxResourceLimits(cpuMillis, memoryMiB, processLimit int) bool {
	return cpuMillis >= 250 && cpuMillis <= 32000 && memoryMiB >= 512 && memoryMiB <= 65536 && processLimit >= 128 && processLimit <= 8192
}

func normalizeProjectToolboxResourceLimits(cpuMillis, memoryMiB, processLimit int) (int, int, int) {
	if cpuMillis == 0 {
		cpuMillis = 4000
	}
	if memoryMiB == 0 {
		memoryMiB = 8192
	}
	if processLimit == 0 {
		processLimit = 2048
	}
	return cpuMillis, memoryMiB, processLimit
}

func (manager *ProjectToolboxManager) save(record projectToolboxRecord) error {
	data, err := json.Marshal(record)
	if err != nil || len(data) > 64<<10 {
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

func newProjectToolboxServiceID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate toolbox service id: %w", err)
	}
	return "ts_" + hex.EncodeToString(buffer), nil
}
