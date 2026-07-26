//go:build !windows

package builder

import (
	"strings"
	"testing"
)

func TestVPSBootstrapIsExactCommitSystemdDurableAndRollbackBounded(t *testing.T) {
	script := readFixture(t, "bootstrap-vps.sh")
	for _, required := range []string{
		"#!/bin/sh",
		"set -eu",
		"umask 077",
		"SOURCE_URL=https://github.com/charle-z/mcp-devbox.git",
		"UNIT=mcp-devbox-builder-bootstrap.service",
		"[ \"$#\" -eq 1 ]",
		"commit must be one lowercase 40-character SHA",
		"systemd-run",
		"--wait",
		"--collect",
		"RuntimeMaxSec=4h",
		"flock -n 9",
		"ROOT_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"PATH=$ROOT_PATH",
		"for tool in awk cat chmod cmp env flock git grep id install mktemp readlink rm stat systemctl systemd-run",
		"exec env -i PATH=\"$ROOT_PATH\" LANG=C LC_ALL=C systemd-run",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"install-prerequisites.sh",
		"run_fixed \"$REPO/packaging/builder/install-prerequisites.sh\"",
		"stage-official-v0.31.2.sh",
		"install-preverified.sh",
		"calibrate-vps.sh",
		"remove.sh",
		"mcp-devbox-buildkit-runc.apparmor",
		"APPARMOR_PROFILE_PATH=/etc/apparmor.d/mcp-devbox-buildkit-runc",
		"APPARMOR_ENABLED=/sys/module/apparmor/parameters/enabled",
		"\"$APPARMOR_PARSER\" -r \"$APPARMOR_PROFILE_PATH\"",
		"systemctl cat \"$BUILDER_UNIT\"",
		"/proc/self/cgroup",
		"*\"/$UNIT\"",
		"trap 'exit 143' TERM",
		"existing builder candidate differs",
		"partial or unmanaged builder installation exists",
		"cmp -s \"$installed\" \"$staged\"",
		"bootstrap completed; evidence is under /var/lib/mcp-devbox-builder-calibration",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"eval ", "sh -c", "bash -c", "docker.sock", "--privileged", "GITHUB_TOKEN",
		"COOLIFY_API_TOKEN", "Authorization:", "curl |", "wget |", "latest",
		"apparmor_restrict_unprivileged_userns=0", "sysctl -w",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "bootstrap-vps.sh")
	assertShellSyntax(t, "bootstrap-vps.sh")
}
