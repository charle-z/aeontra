//go:build !windows

package builder

import (
	"strings"
	"testing"
)

func TestBuilderPrerequisitesAreClosedDebianUbuntuAndNonInteractive(t *testing.T) {
	script := readFixture(t, "install-prerequisites.sh")
	for _, required := range []string{
		"#!/bin/sh",
		"set -eu",
		"umask 077",
		"ROOT_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"SUPPORTED_OS_IDS=\"ubuntu debian\"",
		"[ \"$#\" -eq 0 ]",
		"/etc/os-release",
		"unsupported host operating system",
		"DEBIAN_FRONTEND=noninteractive",
		"apt-get update",
		"packages=\"rootlesskit uidmap slirp4netns fuse-overlayfs\"",
		"packages=\"$packages apparmor\"",
		"/sys/module/apparmor/parameters/enabled",
		"/usr/sbin/apparmor_parser",
		"/usr/bin/rootlesskit",
		"/usr/bin/newuidmap",
		"/usr/bin/newgidmap",
		"/usr/bin/slirp4netns",
		"/usr/bin/fuse-overlayfs",
		"dpkg-query -W -f='${Version}'",
		"mcp-devbox-builder prerequisites: ready",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("prerequisite installer missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"eval ", "sh -c", "bash -c", "source /etc/os-release", ". /etc/os-release",
		"docker.sock", "--privileged", "GITHUB_TOKEN", "COOLIFY_API_TOKEN",
		"Authorization:", "curl ", "wget ", "latest", "${1", "$1/",
		"apparmor_restrict_unprivileged_userns=0", "sysctl -w",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("prerequisite installer contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "install-prerequisites.sh")
	assertShellSyntax(t, "install-prerequisites.sh")
}
