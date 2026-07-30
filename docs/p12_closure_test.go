package docs_test

import (
	"os"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

func TestP12TrustedLinuxWorkcellClosureIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-18-p12.md")
	for _, required := range []string{
		"P12 closure candidate — Trusted Linux Workcell",
		"p12-trusted-linux-workcell",
		"03e83bb",
		"901ff9b",
		"bae1904",
		"07b128f",
		"8c1a836",
		"TRUSTED LINUX WORKCELL",
		"trusted_host_shared_network",
		"one opt-in profile only",
		"default mode",
		"optional local context",
		"does not implement target isolation",
		"/usr/share/seclists",
		"/usr/share/wordlists",
		"root-owned",
		"read-only",
		"/var/run/docker.sock",
		"/run/docker.sock",
		"mcp.devbox.runtime=<runtime_id>",
		"profiles/htb-linux-v1.md",
		"writeups",
		"current-state.md",
		"3600 seconds",
		"tool_count=85",
		"sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941",
		"real Parrot machine remains untouched",
		"release candidate record, not production evidence",
	} {
		if !strings.Contains(strings.ToLower(baseline), strings.ToLower(required)) {
			t.Errorf("P12 baseline missing %q", required)
		}
	}

	guide := read("linux-workcell.md")
	for _, required := range []string{
		"# Trusted Linux Workcell",
		"linux-workcell",
		"dev",
		"htb-linux",
		"TRUSTED LINUX WORKCELL",
		"trusted_host_shared_network",
		"/home/charles/workspaces",
		"/home/charles/htb-machines",
		"workspace inventory",
		"Rootless Docker or Podman",
		"Doctor local",
		"Pairing",
		"Primer smoke",
		"Cancelación",
		"Reanudación",
		"Update",
		"Rollback",
		"Revocación",
		"Desinstalación",
	} {
		if !strings.Contains(strings.ToLower(guide), strings.ToLower(required)) {
			t.Errorf("trusted Linux workcell guide missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(guide), "linux workcell "+"mvp") || strings.Contains(strings.ToLower(baseline), "linux workcell "+"mvp") {
		t.Fatal("P12 documentation regressed to the forbidden product name")
	}

	profiles := read("../internal/edgeclient/workspace_profiles.go")
	for _, required := range []string{
		`WorkspaceProfileSandbox`,
		`WorkspaceProfileLinuxWorkcell`,
		`WorkspaceModeDev`,
		`WorkspaceModeHTBLinux`,
	} {
		if !strings.Contains(profiles, required) {
			t.Errorf("workspace profile contract missing %q", required)
		}
	}
	for _, forbidden := range []string{"WorkspaceProfileHTB", "WorkspaceProfileDev", "htb-workcell"} {
		if strings.Contains(profiles, forbidden) {
			t.Errorf("separate workcell profile reintroduced: %q", forbidden)
		}
	}

	sandbox := read("../internal/edgeclient/opencode_launcher.go")
	if !strings.Contains(sandbox, "!spec.UnshareAll || spec.ShareNetwork || !spec.ClearEnv") {
		t.Fatal("sandbox no-network regression guard is missing")
	}
	workcell := read("../internal/edgeclient/opencode_linux_workcell.go")
	for _, required := range []string{
		`"--share-net"`,
		`LinuxWorkcellNetworkPosture`,
		`"/usr/share/seclists"`,
		`"/usr/share/wordlists"`,
		`safeLinuxWorkcellReadonlyDirectory(target, "/usr/share", 0)`,
	} {
		if !strings.Contains(workcell, required) {
			t.Errorf("Linux workcell policy missing %q", required)
		}
	}

	rootless := read("../internal/edgeclient/rootless_containers.go")
	for _, required := range []string{
		`/run/user`,
		`/var/run/docker.sock`,
		`/run/docker.sock`,
		`rootlessRuntimeLabelKey`,
		`container`,
		`network`,
		`volume`,
	} {
		if !strings.Contains(rootless, required) {
			t.Errorf("rootless container contract missing %q", required)
		}
	}

	registry := read("../internal/edgeclient/workspaces.go")
	for _, required := range []string{"isWindowsMount(path)", "rejectSymlinkPath(path)", "requireCurrentOwner(info)"} {
		if !strings.Contains(registry, required) {
			t.Errorf("workspace registry guard missing %q", required)
		}
	}
	workcellPaths := read("../internal/edgeclient/workcell.go")
	for _, required := range []string{`path == "/mnt"`, `strings.HasPrefix(path, "/mnt/")`} {
		if !strings.Contains(workcellPaths, required) {
			t.Errorf("Windows mount guard missing %q", required)
		}
	}

	template := read("../profiles/htb-linux-v1.md")
	for _, required := range []string{"No consultar writeups", "Nunca inventes una flag", "Máquina recién publicada", "Anti-loop"} {
		if !strings.Contains(strings.ToLower(template), strings.ToLower(required)) {
			t.Errorf("HTB template missing %q", required)
		}
	}

	for _, document := range []string{baseline, guide, read("edge-workcells.md"), read("../README.md")} {
		lower := strings.ToLower(document)
		for _, forbidden := range []string{
			"target-isolated linux workcell",
			"target isolation is enforced",
			"general sudo is granted",
			"flags are sent to the vps",
			"credentials are sent to the vps",
		} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("forbidden P12 claim present: %q", forbidden)
			}
		}
	}

	runtime, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ToolCount != 103 || runtime.CatalogHash != "sha256:1781e419163f57f6d0f5c595fd263a7a3aa36dd5a71b29670d54f9c11233fdfd" {
		t.Fatalf("current additive catalog identity = %d %s", runtime.ToolCount, runtime.CatalogHash)
	}
}
