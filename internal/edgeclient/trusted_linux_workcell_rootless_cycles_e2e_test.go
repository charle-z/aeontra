//go:build p12_e2e && !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type p12RootlessCycleReport struct {
	SchemaVersion             int  `json:"schema_version"`
	Cycle                     int  `json:"cycle"`
	EndpointValidated         bool `json:"endpoint_validated"`
	RootfulSocketInaccessible bool `json:"rootful_socket_inaccessible"`
	CleanStart                bool `json:"clean_start"`
	SecondCycleCleanStart     bool `json:"second_cycle_clean_start"`
	ImageBuilt                bool `json:"image_built"`
	WorkspaceBindValidated    bool `json:"workspace_bind_validated"`
	PodCreated                bool `json:"pod_created"`
	ComposeRan                bool `json:"compose_ran"`
	PostgreSQLHealthy         bool `json:"postgresql_healthy"`
	PostgreSQLReady           bool `json:"postgresql_ready"`
	ChromiumReady             bool `json:"chromium_ready"`
	CancellationRan           bool `json:"cancellation_ran"`
	ProcessesCleaned          bool `json:"processes_cleaned"`
	ContainersCleaned         bool `json:"containers_cleaned"`
	PodsCleaned               bool `json:"pods_cleaned"`
	NetworksCleaned           bool `json:"networks_cleaned"`
	VolumesCleaned            bool `json:"volumes_cleaned"`
	RootlessSocketOwnedByUser bool `json:"rootless_socket_owned_by_user"`
	RootlessSocketPermissions bool `json:"rootless_socket_permissions"`
}

func p12RootlessRuntimeIDs(t *testing.T) []string {
	t.Helper()
	ids := []string{
		strings.TrimSpace(os.Getenv("P12_RUNTIME_ID_CYCLE_1")),
		strings.TrimSpace(os.Getenv("P12_RUNTIME_ID_CYCLE_2")),
	}
	for index, id := range ids {
		if !remoteRuntimeIDPattern.MatchString(id) {
			t.Fatalf("rootless cycle %d runtime identity is invalid", index+1)
		}
	}
	if ids[0] == ids[1] {
		t.Fatal("rootless cycle runtime identities must be distinct")
	}
	return ids
}

func p12RootlessEndpoint(t *testing.T) *RootlessContainerEndpoint {
	t.Helper()
	endpoint, err := DiscoverRootlessContainerEndpoint(os.Geteuid(), openCodeDefaultToolPath)
	if err != nil || endpoint == nil || endpoint.Engine != "podman" {
		t.Fatalf("rootless Podman endpoint unavailable: endpoint=%+v err=%v", endpoint, err)
	}
	runtimeRoot := filepath.Join("/run/user", fmt.Sprintf("%d", os.Geteuid()))
	if err := validateRootlessContainerSocket(endpoint.SocketPath, runtimeRoot, os.Geteuid()); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func p12RuntimeSuffix(runtimeID string) string {
	return strings.TrimPrefix(runtimeID, "mr_")[:12]
}

func p12RootfulSocketsInaccessible() bool {
	for _, path := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		connection, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return false
		}
	}
	return true
}

func p12AssertNoRuntimeResources(t *testing.T, endpoint *RootlessContainerEndpoint, runtimeID string) {
	t.Helper()
	label := rootlessRuntimeLabelKey + "=" + runtimeID
	for _, resource := range []string{"container", "pod", "network", "volume"} {
		if ids := p12ResourceIDs(t, endpoint, resource, label); len(ids) != 0 {
			t.Fatalf("rootless %s resources inherited by runtime cycle", resource)
		}
	}
}

func p12CleanupRuntime(endpoint *RootlessContainerEndpoint, runtimeID, image string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cleanupErr := CleanupRootlessContainerResources(ctx, endpoint, runtimeID, openCodeDefaultToolPath, nil)
	if image != "" {
		prefix := rootlessEnginePrefix(endpoint)
		_, _ = execContainerCommandRunner{}.Run(ctx, endpoint.Executable, append(prefix, "image", "rm", "--force", image), p12RootlessEnv(endpoint))
	}
	return cleanupErr
}

func p12RunRootlessCycle(t *testing.T, endpoint *RootlessContainerEndpoint, runtimeID string, cycle int, secondCycleCleanStart bool) p12RootlessCycleReport {
	t.Helper()
	stage := "clean_start"
	defer func() {
		if t.Failed() {
			fmt.Printf("P12 rootless category=stage_%s\n", stage)
		}
	}()
	p12AssertNoRuntimeResources(t, endpoint, runtimeID)
	if !p12RootfulSocketsInaccessible() {
		t.Fatal("rootful container socket is accessible to the rootless test user")
	}

	label := rootlessRuntimeLabelKey + "=" + runtimeID
	prefix := rootlessEnginePrefix(endpoint)
	suffix := p12RuntimeSuffix(runtimeID)
	workspace := t.TempDir()
	image := "localhost/p12-trusted-workcell-" + suffix + ":cycle"
	cleaned := false
	cleanup := func() error {
		return p12CleanupRuntime(endpoint, runtimeID, image)
	}
	defer func() {
		if !cleaned {
			if err := cleanup(); err != nil {
				t.Errorf("rootless deferred cleanup failed")
			}
		}
	}()

	containerfile := "FROM docker.io/library/alpine:3.20\nCOPY fixture.txt /fixture.txt\nCMD [\"/bin/sh\",\"-c\",\"test -f /fixture.txt && echo p12-image-ready\"]\n"
	if err := os.WriteFile(filepath.Join(workspace, "Containerfile"), []byte(containerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "fixture.txt"), []byte("trusted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage = "image_build"
	p12Engine(t, endpoint, append(prefix, "build", "--label", label, "--tag", image, workspace)...)

	stage = "pod_create"
	podName := "p12-pod-" + suffix
	p12Engine(t, endpoint, append(prefix, "pod", "create", "--name", podName, "--label", label)...)
	stage = "network_volume_create"
	network := "p12-net-" + suffix
	volume := "p12-vol-" + suffix
	p12Engine(t, endpoint, append(prefix, "network", "create", "--label", label, network)...)
	p12Engine(t, endpoint, append(prefix, "volume", "create", "--label", label, volume)...)

	stage = "workspace_bind"
	containerName := "p12-workspace-" + suffix
	workspaceMount := workspace + ":/workspace:ro"
	workspaceOutput := p12Engine(t, endpoint, append(prefix,
		"run", "--name", containerName, "--label", label, "--network", network,
		"--volume", volume+":/data", "--volume", workspaceMount,
		image, "/bin/sh", "-c", "test -f /workspace/fixture.txt && if touch /workspace/p12-denied 2>/dev/null; then exit 1; fi; echo workspace-ready",
	)...)
	workspaceBindValidated := strings.TrimSpace(workspaceOutput) == "workspace-ready"
	if !workspaceBindValidated {
		t.Fatal("workspace bind validation failed")
	}
	type mountEvidence struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}
	mountOutput := p12Engine(t, endpoint, append(prefix, "inspect", "--format", "{{json .Mounts}}", containerName)...)
	var mounts []mountEvidence
	if err := json.Unmarshal([]byte(mountOutput), &mounts); err != nil {
		t.Fatal("workspace bind inventory was not valid JSON")
	}
	projectBinds := 0
	for _, mount := range mounts {
		if mount.Type != "bind" {
			continue
		}
		projectBinds++
		if filepath.Clean(mount.Source) != filepath.Clean(workspace) || mount.Destination != "/workspace" {
			t.Fatal("unexpected project bind exposed to rootless container")
		}
	}
	if projectBinds != 1 {
		t.Fatal("workspace was not the sole project bind")
	}

	stage = "compose"
	composeRan := p12ComposeCycleE2E(t, endpoint, image, runtimeID, suffix)
	stage = "postgres"
	postgresHealthy, postgresReady := p12PostgreSQLCycleE2E(t, endpoint, runtimeID, suffix)
	stage = "chromium"
	chromiumReady := p12ChromiumE2E(t)
	if !chromiumReady {
		t.Fatal("Chromium readiness marker was not rendered")
	}
	stage = "cancellation"
	cancellationRan := p12RootlessCancellationCycleE2E(t, endpoint, image, runtimeID, suffix)
	if !cancellationRan {
		t.Fatal("rootless process-group cancellation was not observed")
	}

	stage = "cleanup"
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	p12AssertNoRuntimeResources(t, endpoint, runtimeID)

	return p12RootlessCycleReport{
		SchemaVersion: 1, Cycle: cycle, EndpointValidated: true,
		RootfulSocketInaccessible: true, CleanStart: true, SecondCycleCleanStart: secondCycleCleanStart,
		ImageBuilt: true, WorkspaceBindValidated: workspaceBindValidated, PodCreated: true,
		ComposeRan: composeRan, PostgreSQLHealthy: postgresHealthy, PostgreSQLReady: postgresReady,
		ChromiumReady: chromiumReady, CancellationRan: cancellationRan, ProcessesCleaned: cancellationRan,
		ContainersCleaned: true, PodsCleaned: true, NetworksCleaned: true, VolumesCleaned: true,
		RootlessSocketOwnedByUser: true, RootlessSocketPermissions: true,
	}
}

func p12ComposeCycleE2E(t *testing.T, endpoint *RootlessContainerEndpoint, image, runtimeID, suffix string) bool {
	t.Helper()
	compose, err := exec.LookPath("podman-compose")
	if err != nil {
		t.Fatal("podman-compose is required")
	}
	label := rootlessRuntimeLabelKey + "=" + runtimeID
	before := p12ResourceIDs(t, endpoint, "container", label)
	root := t.TempDir()
	body := fmt.Sprintf("services:\n  fixture:\n    image: %s\n    command: [\"/bin/sh\", \"-c\", \"echo compose-ready >/data/result; sleep 300\"]\n    labels:\n      %s: %s\n    volumes:\n      - data:/data\nnetworks:\n  default:\n    labels:\n      %s: %s\nvolumes:\n  data:\n    labels:\n      %s: %s\n", image, rootlessRuntimeLabelKey, runtimeID, rootlessRuntimeLabelKey, runtimeID, rootlessRuntimeLabelKey, runtimeID)
	path := filepath.Join(root, "compose.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	project := "p12c" + suffix
	down := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, compose, "--podman-path", endpoint.Executable, "-p", project, "-f", path, "down", "--volumes")
		command.Env = append(os.Environ(), p12RootlessEnv(endpoint)...)
		output, runErr := command.CombinedOutput()
		if len(output) > 64<<10 {
			return errors.New("podman-compose cleanup output exceeded its limit")
		}
		return runErr
	}
	downComplete := false
	defer func() {
		if !downComplete {
			if err := down(); err != nil {
				t.Errorf("podman-compose deferred cleanup failed")
			}
		}
	}()

	command := exec.CommandContext(t.Context(), compose, "--podman-path", endpoint.Executable, "-p", project, "-f", path, "up", "-d")
	command.Env = append(os.Environ(), p12RootlessEnv(endpoint)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("podman-compose failed: %v output=%s", err, p12BoundedDiagnostic(string(output)))
	}
	var composeContainer string
	containerDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(containerDeadline) {
		after := p12ResourceIDs(t, endpoint, "container", label)
		for _, id := range after {
			if !slices.Contains(before, id) {
				composeContainer = id
				break
			}
		}
		if composeContainer != "" {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal("podman-compose container readiness cancelled")
		case <-timer.C:
		}
	}
	if composeContainer == "" {
		t.Fatal("podman-compose did not create a labelled service container")
	}
	prefix := rootlessEnginePrefix(endpoint)
	ready := false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		result, readErr := execContainerCommandRunner{}.Run(readCtx, endpoint.Executable, append(prefix, "exec", composeContainer, "cat", "/data/result"), p12RootlessEnv(endpoint))
		cancel()
		if readErr == nil && strings.TrimSpace(string(result)) == "compose-ready" {
			ready = true
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal("podman-compose readiness cancelled")
		case <-timer.C:
		}
	}
	if !ready {
		t.Fatal("podman-compose service did not persist its readiness marker")
	}
	if err := down(); err != nil {
		t.Fatal("podman-compose cleanup failed")
	}
	downComplete = true
	return true
}

const p12PostgresFixtureImagePrefix = "localhost/p12-postgres-fixture:"

func validP12PostgresImageReference(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != len(p12PostgresFixtureImagePrefix)+64 || !strings.HasPrefix(value, p12PostgresFixtureImagePrefix) {
		return false
	}
	for _, character := range value[len(p12PostgresFixtureImagePrefix):] {
		if character >= '0' && character <= '9' {
			continue
		}
		if character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}

func p12PostgresImage(t *testing.T) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("P12_POSTGRES_IMAGE"))
	if !validP12PostgresImageReference(value) {
		t.Fatal("PostgreSQL fixture image identity is invalid")
	}
	return value
}

func p12PostgreSQLCycleE2E(t *testing.T, endpoint *RootlessContainerEndpoint, runtimeID, suffix string) (bool, bool) {
	t.Helper()
	prefix := rootlessEnginePrefix(endpoint)
	label := rootlessRuntimeLabelKey + "=" + runtimeID
	image := p12PostgresImage(t)
	actualImageID := strings.TrimSpace(p12Engine(t, endpoint, append(prefix, "image", "inspect", "--format", "{{.Id}}", image)...))
	actualImageID = strings.TrimPrefix(actualImageID, "sha256:")
	expectedImageID := strings.TrimPrefix(image, p12PostgresFixtureImagePrefix)
	if actualImageID != expectedImageID {
		t.Fatal("PostgreSQL fixture image identity changed after rootless load")
	}
	network := "p12-pg-net-" + suffix
	p12Engine(t, endpoint, append(prefix, "network", "create", "--label", label, network)...)
	volume := "p12-pg-vol-" + suffix
	p12Engine(t, endpoint, append(prefix, "volume", "create", "--label", label, volume)...)
	name := "p12-postgres-" + suffix
	p12Engine(t, endpoint, append(prefix,
		"run", "-d", "--name", name, "--label", label, "--network", network,
		"--volume", volume+":/var/lib/postgresql/data",
		"--env", "POSTGRES_PASSWORD=***REDACTED-SECRET***", "--env", "POSTGRES_DB=fixture",
		"--health-cmd", "pg_isready -U postgres -d fixture", "--health-interval", "1s", "--health-timeout", "2s", "--health-retries", "60",
		image,
	)...)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		healthCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, healthErr := execContainerCommandRunner{}.Run(healthCtx, endpoint.Executable, append(prefix, "healthcheck", "run", name), p12RootlessEnv(endpoint))
		cancel()
		if healthErr == nil {
			value := p12Engine(t, endpoint, append(prefix, "exec", name, "psql", "-U", "postgres", "-d", "fixture", "-tAc", "SELECT 1")...)
			if strings.TrimSpace(value) != "1" {
				t.Fatal("PostgreSQL readiness query failed")
			}
			return true, true
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal("PostgreSQL healthcheck cancelled")
		case <-timer.C:
		}
	}
	t.Fatal("PostgreSQL healthcheck timed out")
	return false, false
}

func p12RootlessCancellationCycleE2E(t *testing.T, endpoint *RootlessContainerEndpoint, image, runtimeID, suffix string) bool {
	t.Helper()
	prefix := rootlessEnginePrefix(endpoint)
	label := rootlessRuntimeLabelKey + "=" + runtimeID
	name := "p12-cancel-" + suffix
	p12Engine(t, endpoint, append(prefix, "run", "-d", "--name", name, "--label", label, image, "/bin/sh", "-c", "sleep 300")...)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, _ = execContainerCommandRunner{}.Run(ctx, endpoint.Executable, append(prefix, "wait", name), p12RootlessEnv(endpoint))
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false
	}
	p12Engine(t, endpoint, append(prefix, "rm", "-f", name)...)
	return true
}

func TestTrustedLinuxWorkcellRootlessCleanupE2E(t *testing.T) {
	requireP12E2E(t, "P12_ROOTLESS_CLEANUP_E2E")
	endpoint := p12RootlessEndpoint(t)
	for _, runtimeID := range p12RootlessRuntimeIDs(t) {
		if err := p12CleanupRuntime(endpoint, runtimeID, ""); err != nil {
			t.Fatal(err)
		}
		p12AssertNoRuntimeResources(t, endpoint, runtimeID)
	}
}
