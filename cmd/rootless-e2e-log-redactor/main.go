package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	maxLines = 200
	maxBytes = 16 << 10
)

var (
	safeTestLine              = regexp.MustCompile(`^(=== RUN   TestTrustedLinuxWorkcellRootless(?:Cleanup|Restart)?E2E|--- (?:PASS|FAIL): TestTrustedLinuxWorkcellRootless(?:Cleanup|Restart)?E2E \([0-9.]+s\)|PASS|FAIL)$`)
	safeStageLine             = regexp.MustCompile(`^P12 rootless category=stage_(?:clean_start|image_build|pod_create|network_volume_create|workspace_bind|compose|postgres|chromium|cancellation|cleanup)$`)
	safePostgresOperationLine = regexp.MustCompile(`^P12 rootless category=postgres_(?:image_inspect|network_create|volume_create|container_run|readiness_query)$`)
)

func category(line string) string {
	lower := strings.ToLower(line)
	checks := []struct {
		needle string
		code   string
	}{
		{"rootless podman endpoint unavailable", "endpoint_discovery"},
		{"rootless endpoint unavailable after restart", "endpoint_restart"},
		{"runtime-labelled resources survived rootless service restart", "restart_orphans"},
		{"rootless resources remain", "cleanup_resources"},
		{"rootless pod inventory failed", "pod_inventory"},
		{"rootless container inventory failed", "container_inventory"},
		{"rootless network inventory failed", "network_inventory"},
		{"rootless volume inventory failed", "volume_inventory"},
		{"rootless pod cleanup failed", "pod_cleanup"},
		{"rootless container cleanup failed", "container_cleanup"},
		{"rootless network cleanup failed", "network_cleanup"},
		{"rootless volume cleanup failed", "volume_cleanup"},
		{"rootless container engine returned an unsafe resource identifier", "unsafe_resource_id"},
		{"podman-compose cleanup failed", "compose_cleanup"},
		{"podman-compose failed", "compose"},
		{"chromium smoke failed", "chromium"},
		{"p12_chromium_bin is required", "chromium_binary"},
		{"address already in use", "port_collision"},
		{"name is in use", "resource_collision"},
		{"already exists", "resource_collision"},
		{"connection refused", "readiness"},
		{"cannot connect", "readiness"},
		{"dial unix", "socket_connect"},
		{"permission denied", "permission"},
		{"operation not permitted", "permission"},
		{"newuidmap", "user_namespace"},
		{"newgidmap", "user_namespace"},
		{"database is locked", "storage_lock"},
		{"manifest unknown", "image_pull"},
		{"pull access denied", "image_pull"},
		{"no such network", "postgres_network"},
		{"network not found", "postgres_network"},
		{"no such image", "postgres_image"},
		{"image not known", "postgres_image"},
		{"no such file", "not_found"},
		{"not found", "not_found"},
		{"postgresql readiness query", "postgresql_health"},
		{"workspace bind validation", "workspace_bind"},
		{"workspace bind inventory", "workspace_bind"},
		{"unexpected project bind", "workspace_bind"},
		{"workspace was not the sole project bind", "workspace_bind"},
		{"chromium readiness marker", "chromium"},
		{"rootless process-group cancellation", "process_group_cancel"},
		{"postgresql healthcheck", "postgresql_health"},
		{"deadline exceeded", "timeout"},
		{"timed out", "timeout"},
		{"timeout", "timeout"},
		{"rootless cycle runtime identity is invalid", "runtime_identity"},
		{"rootless cycle runtime identities must be distinct", "runtime_identity"},
		{"rootful container socket is accessible", "rootful_socket"},
		{"rootless container socket", "endpoint_socket"},
		{"resources inherited by runtime cycle", "inherited_state"},
		{"deferred cleanup failed", "cleanup"},
		{"podman-compose container readiness", "compose"},
		{"podman-compose readiness cancelled", "compose"},
		{"podman-compose did not create", "compose"},
		{"podman-compose service did not persist", "compose"},
		{"rootless command failed", "podman_command"},
		{"child heartbeat was not recorded", "process_group_start"},
		{"cancelled process group did not stop", "process_group_cancel"},
		{"cancelled process unexpectedly succeeded", "process_group_cancel"},
	}
	for _, check := range checks {
		if strings.Contains(lower, check.needle) {
			return check.code
		}
	}
	return ""
}

func normalize(line string) string {
	line = strings.TrimSpace(line)
	if safeTestLine.MatchString(line) || safeStageLine.MatchString(line) || safePostgresOperationLine.MatchString(line) {
		return line
	}
	if code := category(line); code != "" {
		return "P12 rootless category=" + code
	}
	return ""
}

func redact(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 16<<10), 1<<20)
	lines := 0
	written := 0
	last := ""
	for scanner.Scan() {
		line := normalize(scanner.Text())
		if line == "" || line == last {
			continue
		}
		if lines >= maxLines || written+len(line)+1 > maxBytes {
			break
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
		last = line
		lines++
		written += len(line) + 1
	}
	return scanner.Err()
}

func main() {
	if err := redact(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "P12 rootless category=redactor_failure")
		os.Exit(1)
	}
}
