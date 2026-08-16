//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	rootlessContainerSocketTarget = "/runtime/rootless-container.sock"
	rootlessRuntimeLabelKey       = "mcp.devbox.runtime"
)

type RootlessContainerEndpoint struct {
	Engine     string
	SocketPath string
	Executable string
}

type ContainerCommandRunner interface {
	Run(context.Context, string, []string, []string) ([]byte, error)
}

type execContainerCommandRunner struct{}

var containerResourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func DiscoverRootlessContainerEndpoint(uid int, toolPath string) (*RootlessContainerEndpoint, error) {
	if uid < 1 {
		return nil, errors.New("rootless container endpoint requires a non-root user")
	}
	if strings.TrimSpace(toolPath) == "" {
		toolPath = openCodeDefaultToolPath
	}
	if err := validateOpenCodeToolPath(toolPath); err != nil {
		return nil, err
	}
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(uid))
	candidates := []struct {
		engine     string
		executable string
		path       string
	}{
		{engine: "docker", executable: "docker", path: filepath.Join(runtimeRoot, "docker.sock")},
		{engine: "podman", executable: "podman", path: filepath.Join(runtimeRoot, "podman", "podman.sock")},
	}
	for _, candidate := range candidates {
		executable, ok := findSafeLinuxTool(candidate.executable, toolPath)
		if !ok {
			continue
		}
		if err := validateRootlessContainerSocket(candidate.path, runtimeRoot, uid); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		return &RootlessContainerEndpoint{Engine: candidate.engine, SocketPath: candidate.path, Executable: executable}, nil
	}
	return nil, nil
}

func validateRootlessContainerSocket(path, runtimeRoot string, uid int) error {
	path = filepath.Clean(path)
	runtimeRoot = filepath.Clean(runtimeRoot)
	if !pathInside(runtimeRoot, path) || path == runtimeRoot || path == "/var/run/docker.sock" || path == "/run/docker.sock" {
		return errors.New("rootless container socket path is invalid")
	}
	if err := rejectSymlinkPath(filepath.Dir(path)); err != nil {
		return errors.New("rootless container socket parent is unsafe")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || !ownedByUID(parent, uid) || parent.Mode().Perm()&0o002 != 0 {
		return errors.New("rootless container socket parent is unsafe")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByUID(info, uid) || info.Mode().Perm()&0o007 != 0 {
		return errors.New("rootless container socket is unsafe")
	}
	return nil
}

func CleanupRootlessContainerResources(ctx context.Context, endpoint *RootlessContainerEndpoint, runtimeID, toolPath string, runner ContainerCommandRunner) error {
	if endpoint == nil {
		return nil
	}
	if !remoteRuntimeIDPattern.MatchString(runtimeID) || endpoint.Engine != "docker" && endpoint.Engine != "podman" {
		return errors.New("rootless container cleanup contract is invalid")
	}
	if strings.TrimSpace(toolPath) == "" {
		toolPath = openCodeDefaultToolPath
	}
	if runner == nil {
		runner = execContainerCommandRunner{}
	}
	label := rootlessRuntimeLabelKey + "=" + runtimeID
	environment := rootlessContainerClientEnvironment(endpoint, toolPath)
	resources := []string{"container"}
	if endpoint.Engine == "podman" {
		resources = []string{"pod", "container"}
	}
	resources = append(resources, "network", "volume")
	for _, resource := range resources {
		ids, err := listRootlessContainerResources(ctx, endpoint, resource, label, environment, runner)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			continue
		}
		args := rootlessEnginePrefix(endpoint)
		switch resource {
		case "container":
			args = append(args, "rm", "-f")
		case "pod":
			args = append(args, "pod", "rm", "-f")
		case "network", "volume":
			args = append(args, resource, "rm")
		}
		args = append(args, ids...)
		cleanupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, runErr := runner.Run(cleanupCtx, endpoint.Executable, args, environment)
		cancel()
		if runErr != nil {
			return fmt.Errorf("rootless %s cleanup failed", resource)
		}
	}
	return nil
}

func listRootlessContainerResources(ctx context.Context, endpoint *RootlessContainerEndpoint, resource, label string, environment []string, runner ContainerCommandRunner) ([]string, error) {
	args := rootlessEnginePrefix(endpoint)
	switch resource {
	case "container":
		args = append(args, "ps", "-aq", "--filter", "label="+label)
	case "pod":
		args = append(args, "pod", "ps", "-q", "--filter", "label="+label)
	case "network", "volume":
		args = append(args, resource, "ls", "-q", "--filter", "label="+label)
	default:
		return nil, errors.New("rootless container resource type is invalid")
	}
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	output, err := runner.Run(listCtx, endpoint.Executable, args, environment)
	cancel()
	if err != nil || len(output) > 64<<10 {
		return nil, fmt.Errorf("rootless %s inventory failed", resource)
	}
	seen := make(map[string]struct{})
	for _, field := range strings.Fields(string(output)) {
		if !containerResourceIDPattern.MatchString(field) {
			return nil, errors.New("rootless container engine returned an unsafe resource identifier")
		}
		seen[field] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func rootlessEnginePrefix(endpoint *RootlessContainerEndpoint) []string {
	uri := "unix://" + endpoint.SocketPath
	if endpoint.Engine == "docker" {
		return []string{"--host", uri}
	}
	return []string{"--url", uri}
}

func (execContainerCommandRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append([]string(nil), environment...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = processGroupAttributes()
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	err := command.Run()
	if stdout.Len()+stderr.Len() > 64<<10 {
		return nil, errors.New("rootless container command output exceeded its limit")
	}
	return stdout.Bytes(), err
}
