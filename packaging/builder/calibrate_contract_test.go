//go:build !windows

package builder

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVPSCalibrationIsClosedBoundedAndRollbackCapable(t *testing.T) {
	script := readFixture(t, "calibrate-vps.sh")
	for _, required := range []string{
		"#!/bin/bash",
		"set -Eeuo pipefail",
		"umask 077",
		"readonly SOURCE_URL=https://github.com/charle-z/mcp-devbox.git",
		"readonly HEALTH_URL=https://mcp-devbox-charlez.duckdns.org/healthz",
		"readonly DEFAULT_QUOTA=65",
		"readonly BUILD_TIMEOUT=30m",
		"readonly -a QUOTAS=(50 65 80)",
		"readonly -a PREREQUISITE_PACKAGES=(rootlesskit uidmap slirp4netns fuse-overlayfs)",
		"dpkg-query",
		"host-prerequisites.tsv",
		"exactly one 40-character commit is required",
		"[[ \"$1\" =~ ^[a-f0-9]{40}$ ]]",
		"flock -n 9",
		"cpu.max",
		"max * 100 == quota * period",
		"setsid timeout --signal=TERM --kill-after=30s",
		"health_samples",
		"health_failures",
		"http_502",
		"CPUQuota=${DEFAULT_QUOTA}%",
		"sha256sum \"$ARCHIVE\"",
		"safe_remove_run_tree",
		"p16-buildkit-calibration-",
		"env -i HOME=/var/lib/mcp-devbox-buildkit",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"runuser -u \"$BUILDER_USER\" -- env -i",
		"env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C curl",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("calibrator missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"eval ", "sh -c", "bash -c", "docker.sock", "--privileged", "Authorization:",
		"GITHUB_TOKEN", "COOLIFY_API_TOKEN", "source $", ". $",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("calibrator contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "calibrate-vps.sh")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable locally; Ubuntu CI remains the syntax gate")
	}
	command := exec.Command(bash, "-n", "calibrate-vps.sh")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("calibrator bash syntax failed: %v: %s", err, output)
	}
}

func TestVPSCalibrationKeepsEvidenceSeparateFromBuilderWritableState(t *testing.T) {
	script := readFixture(t, "calibrate-vps.sh")
	for _, required := range []string{
		"EVIDENCE_ROOT=/var/lib/mcp-devbox-builder-calibration",
		"WORK_ROOT=/var/lib/mcp-devbox-builder-calibration-work",
		"CACHE_ROOT=/var/cache/mcp-devbox-builder-calibration",
		"safe_root_directory \"$EVIDENCE_ROOT\" 0700",
		"safe_root_directory \"$WORK_ROOT\" 0711",
		"safe_root_directory \"$CACHE_ROOT\" 0711",
		"install -d -o root -g root -m 0700 \"$RUN_ROOT\" \"$EVIDENCE\"",
		"install -d -o \"$BUILDER_USER\" -g \"$BUILDER_USER\" -m 0700 \"$RUN_WORK\" \"$RUN_CACHE\" \"$SOURCE\" \"$OUTPUT\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("calibrator lost evidence/work separation %q", required)
		}
	}
	info, err := os.Stat("calibrate-vps.sh")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("calibrator mode=%#o want=0755", info.Mode().Perm())
	}
}
