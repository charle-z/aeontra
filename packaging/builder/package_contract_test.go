//go:build !windows

package builder

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildspike"
)

func TestBuilderServiceEnforcesPrivateRootlessCgroupBoundary(t *testing.T) {
	unit := readFixture(t, "mcp-devbox-buildkit.service")
	for _, required := range []string{
		"User=mcp-build",
		"Group=mcp-build",
		"CPUQuota=65%",
		"MemoryHigh=1280M",
		"MemoryMax=1792M",
		"TasksMax=512",
		"IOWeight=25",
		"KillMode=control-group",
		"Delegate=yes",
		"Environment=PATH=/usr/local/lib/mcp-devbox-builder:/usr/bin:/bin",
		"ExecStart=/usr/bin/rootlesskit",
		"--net=slirp4netns",
		"--disable-host-loopback",
		"ProtectSystem=strict",
		"ProtectControlGroups=yes",
		"ProtectProc=invisible",
		"ReadWritePaths=/run/mcp-devbox-buildkit /var/lib/mcp-devbox-buildkit /var/cache/mcp-devbox-buildkit",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("unit missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/var/run/docker.sock", "/run/docker.sock", "tcp://", "--privileged",
		"--oci-worker-no-process-sandbox", "network.host", "security.insecure",
		"ProtectControlGroups=no", "KillMode=process", "ProcSubset=pid",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("unit contains forbidden %q", forbidden)
		}
	}
	if strings.Contains(unit, "RestrictNamespaces=~cgroup ipc net") {
		t.Fatal("unit disables the network namespace required by rootlesskit")
	}
}

func TestBuildkitConfigMatchesReviewedGenerator(t *testing.T) {
	fixture := readFixtureBytes(t, "buildkitd.toml")
	generated, err := buildspike.RenderBuildkitConfig(buildspike.DefaultConfig(1001))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(fixture), bytes.TrimSpace(generated)) {
		t.Fatalf("packaged config drifted from generator\nfixture:\n%s\ngenerated:\n%s", fixture, generated)
	}
}

func TestOfficialBuildkitStageIsPinnedAtomicAndNonInteractive(t *testing.T) {
	script := readFixture(t, "stage-official-v0.31.2.sh")
	for _, required := range []string{
		"VERSION=v0.31.2",
		"BASE_URL=https://github.com/moby/buildkit/releases/download/$VERSION",
		"ARCHIVE_SHA256=fbabdb72433a35f5bb646e4cd424bf8567e5d055710cf55840f7af2020640791",
		"SBOM_SHA256=affbda658a8a8e9ee3bc1d8280ba538d1522adc8a3eb7daaf964904c94628a4f",
		"SIGSTORE_SHA256=b22dca34df188f484547a57758bfc90c658a96cf49c1c09548b435d46d259e90",
		"[ \"$#\" -eq 0 ]",
		"--proto '=https'",
		"--tlsv1.2",
		"bin/buildkitd bin/buildctl bin/buildkit-runc",
		"sha256sum buildkitd buildctl buildkit-runc > SHA256SUMS",
		"cmp -s \"$TARGET/SHA256SUMS\" \"$NEXT/SHA256SUMS\"",
		"mv \"$NEXT\" \"$TARGET\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("official stage script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"eval ", "sh -c", "bash -c", "latest", "${1", "$1/", "docker.sock",
		"--strip-components", "tar --extract $", "chmod 777",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("official stage script contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "stage-official-v0.31.2.sh")
	assertShellSyntax(t, "stage-official-v0.31.2.sh")
}

func TestBuilderInstallIsOfflineFixedInputAndRollbackCapable(t *testing.T) {
	script := readFixture(t, "install-preverified.sh")
	for _, required := range []string{
		"[ \"$#\" -eq 0 ]",
		"/var/lib/mcp-devbox-builder-staging",
		"sha256sum --check --strict SHA256SUMS",
		"useradd --system --user-group --create-home --add-subids-for-system",
		"cut -d: -f3",
		"/usr/bin/fuse-overlayfs",
		"for file in buildkitd buildctl buildkit-runc SHA256SUMS",
		"exactly three entries",
		"buildkit-runc checksum entry is invalid",
		"grep -Eq '^mcp-build:[0-9]+:[0-9]+$' /etc/subuid",
		"grep -Eq '^mcp-build:[0-9]+:[0-9]+$' /etc/subgid",
		"trap rollback EXIT HUP INT TERM",
		"systemctl enable --now \"$UNIT_NAME\"",
		"runuser -u mcp-build -- \"$INSTALL_ROOT/buildctl\"",
		"debug workers",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"curl ", "wget ", "git clone", "docker.sock", "eval ", "sh -c", "bash -c",
		"chmod 777", "--privileged", "http://", "https://",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("installer contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "install-preverified.sh")
	assertShellSyntax(t, "install-preverified.sh")
}

func TestBuilderRemovePreservesStateCacheIdentityAndStaging(t *testing.T) {
	script := readFixture(t, "remove.sh")
	for _, required := range []string{
		"[ \"$#\" -eq 0 ]",
		"systemctl disable --now",
		"$INSTALL_ROOT/buildkit-runc",
		"state, cache, user and preverified staging were preserved",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("remove script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"userdel", "rm -rf /var/lib/mcp-devbox-buildkit", "rm -rf /var/cache/mcp-devbox-buildkit",
		"rm -rf /var/lib/mcp-devbox-builder-staging", "curl ", "wget ", "eval ",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("remove script contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "remove.sh")
	assertShellSyntax(t, "remove.sh")
}

func assertExecutable(t *testing.T, name string) {
	t.Helper()
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %s", name, info.Mode())
	}
}

func assertShellSyntax(t *testing.T, name string) {
	t.Helper()
	path, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-n", path)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s shell syntax failed: %v: %s", name, err, output)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	return string(readFixtureBytes(t, name))
}

func readFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
