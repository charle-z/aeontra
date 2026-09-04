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

	projectToolboxSchemaVersion  = 2
	projectToolboxRuntimeV1      = "v1"
	projectToolboxRuntimeV2      = "v2"
	projectToolboxStateDirectory = "project-toolboxes"
	projectToolboxBaseImage      = "docker.io/library/debian:bookworm-slim"
	projectToolboxLabelKey       = "mcp.devbox.toolbox"
	projectToolboxOutputLimit    = 24 << 10
)

var (
	projectToolboxIDPattern               = regexp.MustCompile(`^tb_[a-f0-9]{32}$`)
	projectToolboxImageIDPattern          = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	projectToolboxEnvKeyPattern           = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	projectToolboxServiceIDPattern        = regexp.MustCompile(`^ts_[a-f0-9]{32}$`)
	projectToolboxServiceNamePattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	projectToolboxCgroupParentPattern     = regexp.MustCompile(`^/system\.slice/p12-rootless-podman-[0-9]+-[0-9]+-[0-9]+\.service/containers$`)
	ErrProjectToolboxNotFound             = errors.New("project toolbox not found")
	ErrProjectToolboxNotOwned             = errors.New("project toolbox is not owned")
	ErrProjectToolboxContainerUnavailable = fmt.Errorf("%w: container unavailable", ErrProjectToolboxNotOwned)
	ErrProjectToolboxContainerMissing     = fmt.Errorf("%w: container missing", ErrProjectToolboxNotOwned)
	ErrProjectToolboxIdentityMismatch     = fmt.Errorf("%w: identity mismatch", ErrProjectToolboxNotOwned)
	ErrProjectToolboxMountMismatch        = fmt.Errorf("%w: mount mismatch", ErrProjectToolboxNotOwned)
	ErrProjectToolboxResourceMismatch     = fmt.Errorf("%w: resource mismatch", ErrProjectToolboxNotOwned)
	ErrProjectToolboxEnvironmentMismatch  = fmt.Errorf("%w: environment mismatch", ErrProjectToolboxNotOwned)
	ErrProjectToolboxEndpointStale        = fmt.Errorf("%w: endpoint stale", ErrProjectToolboxNotOwned)
	ErrProjectToolboxUnsafeState          = errors.New("project toolbox state is unsafe")
	ErrProjectToolboxUnavailable          = errors.New("project toolbox rootless engine is unavailable")
)

type ProjectToolboxManagerConfig struct {
	StateRoot    string
	Endpoint     *RootlessContainerEndpoint
	Runner       ContainerCommandRunner
	environment  rootlessContainerEnvironmentBuilder
	cgroupParent string
	NewID        func() (string, error)
	NewServiceID func() (string, error)
	NewHarnessID func() (string, error)
	Now          func() time.Time
}

type ProjectToolboxManager struct {
	stateRoot    string
	endpoint     *RootlessContainerEndpoint
	runner       ContainerCommandRunner
	environment  rootlessContainerEnvironmentBuilder
	cgroupParent string
	newID        func() (string, error)
	newServiceID func() (string, error)
	newHarnessID func() (string, error)
	now          func() time.Time
	artifactHash map[string]projectBrowserArtifactDigest
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

// ProjectToolboxReconcileRequest is an explicit, non-destructive recovery
// path for stale endpoint metadata and a container whose mounts no longer
// match the v2 contract. It never removes or resets the workspace.
type ProjectToolboxReconcileRequest struct {
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
	SchemaVersion                                     int    `json:"schema_version"`
	RuntimeVersion                                    string `json:"runtime_version"`
	Generation                                        uint64 `json:"generation"`
	WorkspacePath                                     string `json:"workspace_path,omitempty"`
	WorkspaceFingerprint                              string `json:"workspace_fingerprint,omitempty"`
	MountFingerprint                                  string `json:"mount_fingerprint,omitempty"`
	EndpointFingerprint                               string `json:"endpoint_fingerprint,omitempty"`
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
	environment := config.environment
	if environment == nil {
		environment = rootlessContainerClientEnvironment
	}
	cgroupParent := path.Clean(strings.TrimSpace(config.cgroupParent))
	if cgroupParent == "." {
		cgroupParent = ""
	}
	if cgroupParent != "" && !projectToolboxCgroupParentPattern.MatchString(cgroupParent) {
		return nil, ErrProjectToolboxUnsafeState
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
	return &ProjectToolboxManager{stateRoot: root, endpoint: endpoint, runner: runner, environment: environment, cgroupParent: cgroupParent, newID: newID, newServiceID: newServiceID, newHarnessID: newHarnessID, now: now, artifactHash: make(map[string]projectBrowserArtifactDigest)}, nil
}

func (manager *ProjectToolboxManager) Create(ctx context.Context, request ProjectToolboxCreateRequest) (ProjectToolboxSnapshot, bool, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, false, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	request.CPUMillis, request.MemoryMiB, request.ProcessLimit = normalizeProjectToolboxResourceLimits(request.CPUMillis, request.MemoryMiB, request.ProcessLimit)
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, false, err
	}
	if !validProjectToolboxResourceLimits(request.CPUMillis, request.MemoryMiB, request.ProcessLimit) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	if record, err := manager.loadForWorkspace(request.Workspace); err == nil {
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
	runtimeRoots, err := prepareProjectRuntimeRoots(filepath.Dir(manager.stateRoot), request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	workspaceFingerprint, err := projectRuntimeWorkspaceFingerprint(request.Workspace.Path)
	if err != nil {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	createArgs := manager.projectToolboxCreateArgs(toolboxID, containerName, imageID, request.Workspace.Path, runtimeRoots, request.CPUMillis, request.MemoryMiB, request.ProcessLimit)
	createOutput, err := manager.run(ctx, createArgs...)
	containerID := strings.TrimSpace(string(createOutput))
	if err != nil || !containerResourceIDPattern.MatchString(containerID) {
		return ProjectToolboxSnapshot{}, false, ErrProjectToolboxUnavailable
	}
	created := manager.now().UTC()
	record := projectToolboxRecord{
		SchemaVersion: projectToolboxSchemaVersion, RuntimeVersion: projectToolboxRuntimeV2, Generation: 1,
		WorkspacePath: request.Workspace.Path, WorkspaceFingerprint: workspaceFingerprint,
		MountFingerprint:    projectRuntimeMountFingerprint(request.Workspace.Path, workspaceFingerprint, runtimeRoots, projectToolboxRuntimeV2),
		EndpointFingerprint: projectRuntimeEndpointFingerprint(manager.endpoint),
		ToolboxID:           toolboxID, WorkspaceID: request.Workspace.ID, ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias,
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

func (manager *ProjectToolboxManager) projectToolboxCreateArgs(toolboxID, containerName, imageID, workspacePath string, runtimeRoots ProjectRuntimeRoots, cpuMillis, memoryMiB, processLimit int) []string {
	args := []string{"create", "--name", containerName, "--label", projectToolboxLabelKey + "=" + toolboxID,
		"--cpus", fmt.Sprintf("%.3f", float64(cpuMillis)/1000), "--memory", fmt.Sprintf("%dm", memoryMiB), "--pids-limit", fmt.Sprintf("%d", processLimit)}
	if manager.cgroupParent != "" {
		args = append(args, "--cgroup-parent", manager.cgroupParent)
	}
	return append(args,
		"--env", "MCP_DEVBOX_TOOLBOX_CONTAINER_ACCESS=disabled",
		"--env", "PATH=/runtime/tools/bin:/runtime/cargo/bin:/runtime/go/bin:/runtime/pnpm:/runtime/tools/go/bin:/runtime/tools/cargo/bin:/usr/local/bin:/usr/bin:/bin",
		"--env", "HOME=/runtime/home", "--env", "CARGO_HOME=/runtime/cargo", "--env", "RUSTUP_HOME=/runtime/rustup",
		"--env", "NPM_CONFIG_CACHE=/cache/npm", "--env", "PNPM_HOME=/runtime/pnpm", "--env", "PIP_CACHE_DIR=/cache/pip",
		"--env", "UV_CACHE_DIR=/cache/uv", "--env", "MAVEN_HOME=/runtime/maven", "--env", "GRADLE_USER_HOME=/cache/gradle",
		"--env", "GOPATH=/runtime/go", "--env", "GOBIN=/runtime/tools/bin", "--env", "GOMODCACHE=/cache/go-mod", "--env", "GOCACHE=/cache/go-build",
		"--env", "MCP_DEVBOX_RUNTIME_ROOT=/runtime", "--env", "MCP_DEVBOX_CACHE_ROOT=/cache", "--env", "MCP_DEVBOX_ARTIFACT_ROOT=/artifacts",
		"--volume", workspacePath+":/workspace:rw",
		"--volume", runtimeRoots.Runtime+":/runtime:rw", "--volume", runtimeRoots.Cache+":/cache:rw", "--volume", runtimeRoots.Artifacts+":/artifacts:rw",
		"--workdir", "/workspace", imageID, "sleep", "infinity")
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
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	record, err := manager.loadForWorkspace(request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	return manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
}

func (manager *ProjectToolboxManager) Exec(ctx context.Context, request ProjectToolboxExecRequest) (ProjectToolboxSnapshot, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil || len(request.Argv) == 0 || len(request.Argv) > 128 {
		return ProjectToolboxSnapshot{}, ErrProjectToolboxUnsafeState
	}
	record, err := manager.loadForWorkspace(request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if err := manager.cleanupRetiringContainers(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
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
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	record, err := manager.loadForWorkspace(request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if err := manager.cleanupRetiringContainers(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	snapshot, err := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if errors.Is(err, ErrProjectToolboxContainerUnavailable) {
		if recoverErr := manager.recoverOwnedContainer(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); recoverErr != nil {
			return ProjectToolboxSnapshot{}, recoverErr
		}
		snapshot, err = manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	}
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

// Reconcile proves the durable toolbox identity before replacing a stale
// container. A mount mismatch is the one case where replacing the container
// is safe and useful: label, image, resource limits and authority environment
// must all still match first. Missing/unowned containers remain fail-closed.
func (manager *ProjectToolboxManager) Reconcile(ctx context.Context, request ProjectToolboxReconcileRequest) (ProjectToolboxSnapshot, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	record, err := manager.loadForWorkspace(request.Workspace)
	if err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	if err := manager.cleanupRetiringContainers(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	snapshot, statusErr := manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
	if statusErr == nil {
		return snapshot, nil
	}
	if errors.Is(statusErr, ErrProjectToolboxContainerUnavailable) {
		if recoverErr := manager.recoverOwnedContainer(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); recoverErr == nil {
			return manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
		} else if errors.Is(recoverErr, ErrProjectToolboxMountMismatch) {
			statusErr = recoverErr
		}
	}
	relocated := false
	if errors.Is(statusErr, ErrProjectToolboxIdentityMismatch) && record.WorkspacePath != request.Workspace.Path {
		if currentFingerprint, fingerprintErr := projectRuntimeWorkspaceFingerprint(request.Workspace.Path); fingerprintErr == nil {
			// A deliberate workspace relocation can retain the same directory
			// identity. It is repairable only with an explicit Reconcile call;
			// replacement at the same path (different inode) remains blocked.
			relocated = currentFingerprint == record.WorkspaceFingerprint
		}
	}
	if !errors.Is(statusErr, ErrProjectToolboxMountMismatch) && !relocated {
		return ProjectToolboxSnapshot{}, statusErr
	}
	if err := manager.recreateAfterMountMismatch(ctx, &record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return ProjectToolboxSnapshot{}, err
	}
	return manager.status(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace)
}

// cleanupRetiringContainers removes only old containers that this exact
// toolbox created during a mount reconciliation. The label narrows discovery
// to the toolbox identity; metadata verification proves the image, resource
// policy and runtime environment before removal. Containers with another name
// or mismatched metadata are left untouched and fail closed when they use the
// retiring name shape.
func (manager *ProjectToolboxManager) cleanupRetiringContainers(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) error {
	if record.WorkspaceID != workspace.ID || record.ProjectAlias != alias || record.TargetAlias != target ||
		record.SchemaVersion != projectToolboxSchemaVersion || !projectToolboxIDPattern.MatchString(record.ToolboxID) ||
		record.ContainerName != "mcp-toolbox-"+strings.TrimPrefix(record.ToolboxID, "tb_") ||
		filepath.Clean(record.WorkspacePath) != filepath.Clean(workspace.Path) {
		return ErrProjectToolboxIdentityMismatch
	}
	workspaceFingerprint, err := projectRuntimeWorkspaceFingerprint(workspace.Path)
	if err != nil || workspaceFingerprint != record.WorkspaceFingerprint {
		return ErrProjectToolboxIdentityMismatch
	}
	output, err := manager.run(ctx, "ps", "-aq", "--filter", "label="+projectToolboxLabelKey+"="+record.ToolboxID)
	if err != nil {
		return ErrProjectToolboxUnavailable
	}
	seen := make(map[string]struct{})
	for _, candidate := range strings.Fields(string(output)) {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		if !containerResourceIDPattern.MatchString(candidate) {
			return ErrProjectToolboxUnsafeState
		}
		nameOutput, inspectErr := manager.run(ctx, "inspect", "--format", "{{.Name}}", candidate)
		if inspectErr != nil {
			return ErrProjectToolboxContainerUnavailable
		}
		name := strings.TrimPrefix(strings.TrimSpace(string(nameOutput)), "/")
		if name == record.ContainerName || !projectToolboxRetiringContainerName(name, record.ContainerName) {
			continue
		}
		retiring := record
		retiring.ContainerName = name
		if metadataErr := manager.verifyContainerMetadata(ctx, retiring); metadataErr != nil {
			return metadataErr
		}
		if _, removeErr := manager.run(ctx, "rm", "-f", candidate); removeErr != nil {
			return ErrProjectToolboxUnavailable
		}
	}
	return nil
}

func projectToolboxRetiringContainerName(name, canonical string) bool {
	prefix := canonical + "-retiring-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	generation := strings.TrimPrefix(name, prefix)
	if generation == "" {
		return false
	}
	_, err := strconv.ParseUint(generation, 10, 64)
	return err == nil
}

func (manager *ProjectToolboxManager) recreateAfterMountMismatch(ctx context.Context, record *projectToolboxRecord, alias, target string, workspace Workspace) error {
	if record == nil || record.RuntimeVersion == "" {
		return ErrProjectToolboxUnsafeState
	}
	// This metadata-only proof intentionally excludes mounts, which are the
	// reason for reconciliation. It still proves that the rootless container
	// is this toolbox and carries the expected resource/security policy.
	if err := manager.verifyContainerMetadata(ctx, *record); err != nil {
		return err
	}
	runtimeRoots, err := prepareProjectRuntimeRoots(filepath.Dir(manager.stateRoot), workspace)
	if err != nil {
		return ErrProjectToolboxUnsafeState
	}
	workspaceFingerprint, err := projectRuntimeWorkspaceFingerprint(workspace.Path)
	if err != nil {
		return ErrProjectToolboxUnsafeState
	}
	oldName := record.ContainerName
	wasRunning := false
	stateOutput, stateErr := manager.run(ctx, "inspect", "--format", "{{.State.Running}}", oldName)
	if stateErr != nil {
		return ErrProjectToolboxContainerUnavailable
	}
	wasRunning = strings.EqualFold(strings.TrimSpace(string(stateOutput)), "true") || strings.HasSuffix(strings.TrimSpace(string(stateOutput)), "|true")
	if wasRunning {
		if _, err := manager.run(ctx, "stop", oldName); err != nil {
			return ErrProjectToolboxUnavailable
		}
	}
	// Keep the old container as a rollback candidate until the replacement has
	// been created, attested, started, and durably recorded. The canonical name
	// is retained for the replacement so the record's identity invariant does
	// not need a second naming scheme.
	retiringName := fmt.Sprintf("%s-retiring-%d", oldName, record.Generation)
	if _, err := manager.run(ctx, "rename", oldName, retiringName); err != nil {
		return ErrProjectToolboxUnavailable
	}
	restoreOld := func() {
		_, _ = manager.run(context.Background(), "rm", "-f", oldName)
		if _, renameErr := manager.run(context.Background(), "rename", retiringName, oldName); renameErr == nil && wasRunning {
			_, _ = manager.run(context.Background(), "start", oldName)
		}
	}
	createOutput, err := manager.run(ctx, manager.projectToolboxCreateArgs(record.ToolboxID, record.ContainerName, record.BaseImageID, workspace.Path, runtimeRoots, record.CPUMillis, record.MemoryMiB, record.ProcessLimit)...)
	if err != nil || !containerResourceIDPattern.MatchString(strings.TrimSpace(string(createOutput))) {
		restoreOld()
		return ErrProjectToolboxUnavailable
	}
	now := manager.now().UTC()
	replacement := *record
	replacement.SchemaVersion = projectToolboxSchemaVersion
	replacement.RuntimeVersion = projectToolboxRuntimeV2
	if replacement.Generation == ^uint64(0) {
		restoreOld()
		return ErrProjectToolboxUnsafeState
	}
	replacement.Generation++
	replacement.WorkspacePath = filepath.Clean(workspace.Path)
	replacement.WorkspaceFingerprint = workspaceFingerprint
	replacement.MountFingerprint = projectRuntimeMountFingerprint(workspace.Path, workspaceFingerprint, runtimeRoots, projectToolboxRuntimeV2)
	replacement.EndpointFingerprint = projectRuntimeEndpointFingerprint(manager.endpoint)
	replacement.Services = nil
	replacement.BrowserHarnessRuns = nil
	replacement.UpdatedAt = now
	if err := manager.verifyOwnership(ctx, replacement, alias, target, workspace); err != nil {
		restoreOld()
		return err
	}
	if _, err := manager.run(ctx, "start", replacement.ContainerName); err != nil {
		restoreOld()
		return ErrProjectToolboxUnavailable
	}
	if err := manager.save(replacement); err != nil {
		restoreOld()
		return err
	}
	*record = replacement
	if _, err := manager.run(ctx, "rm", "-f", retiringName); err != nil {
		// The new record is authoritative and the old container is stopped; a
		// failed retirement is recoverable by the next scoped cleanup.
		return ErrProjectToolboxUnavailable
	}
	return nil
}

func (manager *ProjectToolboxManager) verifyContainerMetadata(ctx context.Context, record projectToolboxRecord) error {
	output, err := manager.run(ctx, "inspect", "--format", `{{index .Config.Labels "`+projectToolboxLabelKey+`"}}|{{.Image}}`, record.ContainerName)
	if err != nil {
		return ErrProjectToolboxContainerUnavailable
	}
	label, rawImageID, found := strings.Cut(strings.TrimSpace(string(output)), "|")
	imageID, normalizeErr := normalizeProjectToolboxImageID(rawImageID)
	if !found || label != record.ToolboxID || normalizeErr != nil || imageID != record.BaseImageID {
		return ErrProjectToolboxIdentityMismatch
	}
	resourceOutput, err := manager.run(ctx, "inspect", "--format", "{{.HostConfig.Memory}}|{{.HostConfig.NanoCpus}}|{{.HostConfig.PidsLimit}}", record.ContainerName)
	wantResources := fmt.Sprintf("%d|%d|%d", int64(record.MemoryMiB)*1024*1024, int64(record.CPUMillis)*1000000, record.ProcessLimit)
	if err != nil || strings.TrimSpace(string(resourceOutput)) != wantResources {
		return ErrProjectToolboxResourceMismatch
	}
	environmentOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Config.Env}}", record.ContainerName)
	var environment []string
	if err != nil || json.Unmarshal(environmentOutput, &environment) != nil || !validProjectToolboxContainerEnvironment(environment, record.RuntimeVersion) {
		return ErrProjectToolboxEnvironmentMismatch
	}
	return nil
}

func (manager *ProjectToolboxManager) ServiceStart(ctx context.Context, request ProjectToolboxServiceStartRequest) (ProjectToolboxServiceSnapshot, bool, error) {
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, false, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil ||
		!projectToolboxServiceNamePattern.MatchString(request.Name) || len(request.Argv) == 0 || len(request.Argv) > 128 {
		return ProjectToolboxServiceSnapshot{}, false, ErrProjectToolboxUnsafeState
	}
	record, err := manager.loadForWorkspace(request.Workspace)
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
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	defer release()
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
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return ProjectToolboxServiceSnapshot{}, err
	}
	defer release()
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
	release, err := manager.acquireWorkspaceLock(ctx, request.Workspace.ID)
	if err != nil {
		return false, err
	}
	defer release()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := validateProjectToolboxBinding(request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
		return false, err
	}
	record, err := manager.loadForWorkspace(request.Workspace)
	if errors.Is(err, ErrProjectToolboxNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := manager.cleanupRetiringContainers(ctx, record, request.ProjectAlias, request.TargetAlias, request.Workspace); err != nil {
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
	record, err := manager.loadForWorkspace(request.Workspace)
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
	if record.EndpointFingerprint != projectRuntimeEndpointFingerprint(manager.endpoint) {
		// A rootless engine can be recreated or move from Podman to Docker while
		// retaining the owned container. Once the container's identity, mounts,
		// resources and environment are attested, refresh only endpoint metadata.
		record.EndpointFingerprint = projectRuntimeEndpointFingerprint(manager.endpoint)
		record.Generation++
		record.UpdatedAt = manager.now().UTC()
		if err := manager.save(record); err != nil {
			return ProjectToolboxSnapshot{}, err
		}
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
	return ProjectToolboxSnapshot{ToolboxID: record.ToolboxID, State: state, BaseImage: record.BaseImage, BaseImageID: record.BaseImageID, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, CPUMillis: record.CPUMillis, MemoryMiB: record.MemoryMiB, ProcessLimit: record.ProcessLimit, ContainerAccess: false, WritableBytes: writableBytes, RootFSBytes: rootFSBytes}, nil
}

func (manager *ProjectToolboxManager) recoverOwnedContainer(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) error {
	output, err := manager.run(ctx, "ps", "-aq", "--filter", "label="+projectToolboxLabelKey+"="+record.ToolboxID)
	if err != nil {
		return ErrProjectToolboxUnavailable
	}
	candidates := strings.Fields(string(output))
	if len(candidates) == 0 {
		return ErrProjectToolboxContainerMissing
	}
	if len(candidates) != 1 || !containerResourceIDPattern.MatchString(candidates[0]) {
		return ErrProjectToolboxUnsafeState
	}
	candidate := candidates[0]
	if err := manager.verifyOwnershipReference(ctx, record, alias, target, workspace, candidate); err != nil {
		return err
	}
	nameOutput, err := manager.run(ctx, "inspect", "--format", "{{.Name}}", candidate)
	if err != nil {
		return ErrProjectToolboxContainerUnavailable
	}
	currentName := strings.TrimPrefix(strings.TrimSpace(string(nameOutput)), "/")
	if currentName == "" {
		return ErrProjectToolboxUnsafeState
	}
	if currentName != record.ContainerName {
		if _, err := manager.run(ctx, "rename", candidate, record.ContainerName); err != nil {
			return ErrProjectToolboxUnavailable
		}
	}
	return manager.verifyOwnership(ctx, record, alias, target, workspace)
}

func (manager *ProjectToolboxManager) verifyOwnership(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace) error {
	return manager.verifyOwnershipReference(ctx, record, alias, target, workspace, record.ContainerName)
}

func (manager *ProjectToolboxManager) verifyOwnershipReference(ctx context.Context, record projectToolboxRecord, alias, target string, workspace Workspace, reference string) error {
	if record.WorkspaceID != workspace.ID || record.ProjectAlias != alias || record.TargetAlias != target || record.SchemaVersion != projectToolboxSchemaVersion || !projectToolboxIDPattern.MatchString(record.ToolboxID) || record.ContainerName != "mcp-toolbox-"+strings.TrimPrefix(record.ToolboxID, "tb_") {
		return fmt.Errorf("%w: record identity or schema mismatch", ErrProjectToolboxIdentityMismatch)
	}
	if filepath.Clean(record.WorkspacePath) != filepath.Clean(workspace.Path) {
		return fmt.Errorf("%w: workspace_path expected=%s observed=%s", ErrProjectToolboxIdentityMismatch, record.WorkspacePath, workspace.Path)
	}
	workspaceFingerprint, fingerprintErr := projectRuntimeWorkspaceFingerprint(workspace.Path)
	if fingerprintErr != nil {
		return fmt.Errorf("%w: workspace fingerprint unavailable", ErrProjectToolboxIdentityMismatch)
	}
	if record.WorkspaceFingerprint != workspaceFingerprint {
		return fmt.Errorf("%w: workspace_fingerprint expected=%s observed=%s", ErrProjectToolboxIdentityMismatch, record.WorkspaceFingerprint, workspaceFingerprint)
	}
	if reference != record.ContainerName && !containerResourceIDPattern.MatchString(reference) {
		return ErrProjectToolboxUnsafeState
	}
	output, err := manager.run(ctx, "inspect", "--format", `{{index .Config.Labels "`+projectToolboxLabelKey+`"}}|{{.Image}}`, reference)
	if err != nil {
		return ErrProjectToolboxContainerUnavailable
	}
	label, rawImageID, found := strings.Cut(strings.TrimSpace(string(output)), "|")
	imageID, normalizeErr := normalizeProjectToolboxImageID(rawImageID)
	if !found || label != record.ToolboxID || normalizeErr != nil || imageID != record.BaseImageID {
		return ErrProjectToolboxIdentityMismatch
	}
	mountOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Mounts}}", reference)
	var mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	if err != nil || json.Unmarshal(mountOutput, &mounts) != nil {
		return fmt.Errorf("%w: mount metadata unreadable", ErrProjectToolboxMountMismatch)
	}
	if record.RuntimeVersion == projectToolboxRuntimeV2 {
		runtimeRoots, rootsErr := prepareProjectRuntimeRoots(filepath.Dir(manager.stateRoot), workspace)
		workspaceFingerprint, fingerprintErr := projectRuntimeWorkspaceFingerprint(workspace.Path)
		mountFingerprint := projectRuntimeMountFingerprint(workspace.Path, workspaceFingerprint, runtimeRoots, projectToolboxRuntimeV2)
		if rootsErr != nil || fingerprintErr != nil || record.MountFingerprint != mountFingerprint || !validProjectToolboxMountsV2(mounts, workspace.Path, runtimeRoots) {
			return fmt.Errorf("%w: runtime mount fingerprint=%s", ErrProjectToolboxMountMismatch, record.MountFingerprint)
		}
	} else if !validProjectToolboxMountsLegacy(mounts, workspace.Path) {
		return fmt.Errorf("%w: legacy workspace mount mismatch", ErrProjectToolboxMountMismatch)
	}
	resourceOutput, err := manager.run(ctx, "inspect", "--format", "{{.HostConfig.Memory}}|{{.HostConfig.NanoCpus}}|{{.HostConfig.PidsLimit}}", reference)
	wantResources := fmt.Sprintf("%d|%d|%d", int64(record.MemoryMiB)*1024*1024, int64(record.CPUMillis)*1000000, record.ProcessLimit)
	if err != nil || strings.TrimSpace(string(resourceOutput)) != wantResources {
		return ErrProjectToolboxResourceMismatch
	}
	environmentOutput, err := manager.run(ctx, "inspect", "--format", "{{json .Config.Env}}", reference)
	var environment []string
	if err != nil || json.Unmarshal(environmentOutput, &environment) != nil || !validProjectToolboxContainerEnvironment(environment, record.RuntimeVersion) {
		return fmt.Errorf("%w: runtime=%s", ErrProjectToolboxEnvironmentMismatch, record.RuntimeVersion)
	}
	return nil
}

func validProjectToolboxMountsLegacy(mounts []struct {
	Type, Source, Destination string
	RW                        bool
}, workspacePath string) bool {
	if len(mounts) != 1 {
		return false
	}
	mount := mounts[0]
	return mount.Type == "bind" && mount.RW && mount.Source == workspacePath && mount.Destination == "/workspace"
}

func validProjectToolboxMountsV2(mounts []struct {
	Type, Source, Destination string
	RW                        bool
}, workspacePath string, roots ProjectRuntimeRoots) bool {
	want := map[string]string{
		"/workspace": workspacePath,
		"/runtime":   roots.Runtime,
		"/cache":     roots.Cache,
		"/artifacts": roots.Artifacts,
	}
	if len(mounts) != len(want) {
		return false
	}
	for _, mount := range mounts {
		if mount.Type != "bind" || !mount.RW || want[mount.Destination] != mount.Source {
			return false
		}
		delete(want, mount.Destination)
	}
	return len(want) == 0
}

func validProjectToolboxContainerEnvironment(environment []string, runtimeVersion string) bool {
	want := map[string]string{
		"MCP_DEVBOX_TOOLBOX_CONTAINER_ACCESS": "disabled",
	}
	if runtimeVersion == projectToolboxRuntimeV2 {
		for key, value := range map[string]string{
			"PATH": "/runtime/tools/bin:/runtime/cargo/bin:/runtime/go/bin:/runtime/pnpm:/runtime/tools/go/bin:/runtime/tools/cargo/bin:/usr/local/bin:/usr/bin:/bin",
			"HOME": "/runtime/home", "CARGO_HOME": "/runtime/cargo", "RUSTUP_HOME": "/runtime/rustup",
			"NPM_CONFIG_CACHE": "/cache/npm", "PNPM_HOME": "/runtime/pnpm", "PIP_CACHE_DIR": "/cache/pip",
			"UV_CACHE_DIR": "/cache/uv", "MAVEN_HOME": "/runtime/maven", "GRADLE_USER_HOME": "/cache/gradle",
			"GOPATH": "/runtime/go", "GOBIN": "/runtime/tools/bin", "GOMODCACHE": "/cache/go-mod", "GOCACHE": "/cache/go-build",
			"MCP_DEVBOX_RUNTIME_ROOT": "/runtime", "MCP_DEVBOX_CACHE_ROOT": "/cache", "MCP_DEVBOX_ARTIFACT_ROOT": "/artifacts",
		} {
			want[key] = value
		}
	}
	forbidden := map[string]bool{"DOCKER_HOST": true, "CONTAINER_HOST": true, "DOCKER_CONFIG": true, "MCP_DEVBOX_CONTAINER_ENGINE": true, "MCP_DEVBOX_CONTAINER_LABEL": true, "COMPOSE_PROJECT_NAME": true}
	found := map[string]bool{}
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if key == "" || found[key] {
			return false
		}
		found[key] = true
		if forbidden[key] {
			return false
		}
		if expected, tracked := want[key]; tracked {
			if !ok || value != expected {
				return false
			}
		}
	}
	for key := range want {
		if !found[key] {
			return false
		}
	}
	return true
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
	environment, err := manager.environment(manager.endpoint, openCodeDefaultToolPath)
	if err != nil {
		return nil, err
	}
	return manager.runner.Run(ctx, manager.endpoint.Executable, prefixed, environment)
}

func (manager *ProjectToolboxManager) recordPath(workspaceID string) string {
	return filepath.Join(manager.stateRoot, workspaceID+".json")
}

// loadForWorkspace performs the only on-disk migration used by toolbox v2.
// Schema v1 records are retained and upgraded in place; their existing
// container is still treated as a legacy runtime until an explicit reconcile
// recreates it. No workspace contents are touched or removed.
func (manager *ProjectToolboxManager) loadForWorkspace(workspace Workspace) (projectToolboxRecord, error) {
	record, err := manager.load(workspace.ID)
	if err != nil {
		return projectToolboxRecord{}, err
	}
	workspaceFingerprint, err := projectRuntimeWorkspaceFingerprint(workspace.Path)
	if err != nil {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	changed := false
	if record.SchemaVersion < projectToolboxSchemaVersion {
		record.SchemaVersion = projectToolboxSchemaVersion
		if record.RuntimeVersion == "" {
			record.RuntimeVersion = projectToolboxRuntimeV1
		}
		if record.Generation == 0 {
			record.Generation = 1
		}
		changed = true
	}
	if record.WorkspacePath == "" {
		record.WorkspacePath = filepath.Clean(workspace.Path)
		changed = true
	}
	if record.WorkspaceFingerprint == "" {
		record.WorkspaceFingerprint = workspaceFingerprint
		changed = true
	}
	if record.MountFingerprint == "" {
		record.MountFingerprint = projectRuntimeMountFingerprint(record.WorkspacePath, record.WorkspaceFingerprint, ProjectRuntimeRoots{}, record.RuntimeVersion)
		changed = true
	}
	if record.EndpointFingerprint == "" {
		record.EndpointFingerprint = projectRuntimeEndpointFingerprint(manager.endpoint)
		changed = true
	}
	if changed {
		record.UpdatedAt = manager.now().UTC()
		if err := manager.save(record); err != nil {
			return projectToolboxRecord{}, err
		}
	}
	return record, nil
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
	// Records written before schema_version existed are schema v1. Keep them
	// readable so a controlled migration can attest the current workspace on
	// the next operation. Future versions fail closed instead of being guessed.
	if record.SchemaVersion == 0 {
		record.SchemaVersion = 1
	}
	if record.SchemaVersion < 1 || record.SchemaVersion > projectToolboxSchemaVersion {
		return projectToolboxRecord{}, ErrProjectToolboxUnsafeState
	}
	if record.SchemaVersion == 1 && record.RuntimeVersion == "" {
		record.RuntimeVersion = projectToolboxRuntimeV1
	}
	if record.RuntimeVersion != projectToolboxRuntimeV1 && record.RuntimeVersion != projectToolboxRuntimeV2 {
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
		"DOCKER_HOST", "CONTAINER_HOST", "DOCKER_CONFIG",
		"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES",
		"CONTAINERS_STORAGE_CONF":
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
