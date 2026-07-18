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

var safeTestLine = regexp.MustCompile(`^(=== RUN   TestTrustedLinuxWorkcellRootless(?:E2E|RestartE2E)|--- (?:PASS|FAIL): TestTrustedLinuxWorkcellRootless(?:E2E|RestartE2E) \([0-9.]+s\)|PASS|FAIL)$`)

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
		{"no such file", "not_found"},
		{"not found", "not_found"},
		{"deadline exceeded", "timeout"},
		{"timed out", "timeout"},
		{"timeout", "timeout"},
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
	if safeTestLine.MatchString(line) {
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
