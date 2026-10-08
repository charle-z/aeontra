package parrot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}

func readRepositoryFile(t *testing.T, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestParrotOnboardingScriptIsSyntacticallyValid(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	path := filepath.Join(repositoryRoot(t), "packaging", "parrot", "onboarding-preflight.sh")
	command := exec.Command(bash, "-n", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, output)
	}
}

func TestParrotOnboardingContractIncludesRealProductionRequirements(t *testing.T) {
	script := readRepositoryFile(t, "packaging/parrot/onboarding-preflight.sh")
	for _, expected := range []string{
		"--unshare-all",
		"--share-net",
		"/mnt/c",
		"/mnt/d",
		"/runtime/rootless-container.sock",
		"MCP_DEVBOX_REQUIRE_ROOTLESS:-1",
		"v24.18.0",
		"/usr/local/libexec/mcp-devbox/node",
		"1.18.1",
		"$BUNDLE_ROOT/manifest.json",
		"$BUNDLE_ROOT/manifest.sig",
		"$BUNDLE_ROOT/libexec/mcp-autopilot-worker",
		"$BUNDLE_ROOT/libexec/mcp-bundle-updater",
		"$BUNDLE_ROOT/systemd/mcp-devbox-edge@.service",
		"signed Codex harness is incomplete",
		"parrot-onboarding-preflight-ok",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("onboarding script missing %q", expected)
		}
	}
	if strings.Contains(script, "/var/run/docker.sock --bind") || strings.Contains(script, "/run/docker.sock --bind") {
		t.Fatal("onboarding script exposes a rootful container socket")
	}
	if strings.Contains(script, `if [ -x "$CODEX" ] && [ -r "$CODEX_PIN" ]`) {
		t.Fatal("hybrid v4 bundle must not select Codex solely because the compatibility binary exists")
	}

	unit := readRepositoryFile(t, "packaging/systemd/mcp-devbox-edge@.service")
	for _, expected := range []string{
		"User=%i",
		"/usr/local/bin/mcp-edge codex",
		"--bundle-root /opt/mcp-devbox/current",
		"--codex /opt/mcp-devbox/current/codex/codex",
		"--codex-pin /opt/mcp-devbox/current/codex/pin.json",
		"AF_NETLINK",
		"NoNewPrivileges=yes",
		"RestrictNamespaces=false",
		"KillMode=process",
		"ReadWritePaths=/home/%i/.local/state/mcp-edge /home/%i/workspaces /home/%i/htb-machines",
	} {
		if !strings.Contains(unit, expected) {
			t.Fatalf("Codex Edge unit missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"Conflicts=mcp-devbox-edge.service mcp-devbox-opencode-edge.service",
		"After=mcp-devbox-edge.service mcp-devbox-opencode-edge.service",
		"--state /home/%i/.local/state/mcp-edge",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("Codex Edge unit must not manage unpackaged legacy units through %q", forbidden)
		}
	}
	if strings.Contains(unit, "/var/run/docker.sock") || strings.Contains(unit, "/run/docker.sock") {
		t.Fatal("Codex Edge unit exposes a rootful container socket")
	}
	bridge := readRepositoryFile(t, "packaging/systemd/mcp-devbox-opencode-edge-bridge@.service")
	for _, expected := range []string{"/usr/local/bin/mcp-edge opencode", "--driver /opt/mcp-devbox/current/libexec/model-turn-driver", "--provider /opt/mcp-devbox/opencode-provider", "KillMode=process"} {
		if !strings.Contains(bridge, expected) {
			t.Fatalf("bridge Edge unit missing %q", expected)
		}
	}
	if strings.Contains(bridge, " mcp-edge codex") {
		t.Fatal("bridge Edge unit must retain OpenCode until the v4-aware updater is installed")
	}
	if strings.Contains(bridge, "--state /home/%i/.local/state/mcp-edge") {
		t.Fatal("bridge Edge unit must derive state from the package-selected HOME")
	}
}

func TestEdgeServiceUnitsAllowBubblewrapOpenat2(t *testing.T) {
	for _, name := range []string{
		"mcp-devbox-edge.service",
		"mcp-devbox-edge@.service",
		"mcp-devbox-opencode-edge@.service",
		"mcp-devbox-opencode-edge-bridge@.service",
	} {
		t.Run(name, func(t *testing.T) {
			unit := "\n" + strings.ReplaceAll(readRepositoryFile(t, filepath.Join("packaging", "systemd", name)), "\r\n", "\n")
			for _, directive := range []string{
				"RestrictSUIDSGID=no",
				"CapabilityBoundingSet=",
				"AmbientCapabilities=",
				"ProtectSystem=strict",
			} {
				if strings.Count(unit, "\n"+directive+"\n") != 1 {
					t.Fatalf("Edge unit must contain exactly one %q", directive)
				}
			}
			if strings.Contains(unit, "\nRestrictSUIDSGID=yes\n") || strings.Contains(unit, "\nRestrictSUIDSGID=true\n") {
				t.Fatal("Edge unit must not block Bubblewrap openat2")
			}
			if !strings.Contains(unit, "\nNoNewPrivileges=yes\n") && !strings.Contains(unit, "\nNoNewPrivileges=true\n") {
				t.Fatal("Edge unit must continue to deny new privileges")
			}
		})
	}
}
