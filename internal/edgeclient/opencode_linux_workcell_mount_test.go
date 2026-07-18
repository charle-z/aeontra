//go:build !windows

package edgeclient

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxWorkcellValidatorRejectsUnexpectedReadonlyHostMount(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	spec, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	leak := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(leak, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := spec.Sandbox
	bad.Mounts = append(append([]openCodeSandboxMount(nil), bad.Mounts...), openCodeSandboxMount{Source: leak, Target: "/leak", Kind: "bind"})
	if err := validateLinuxWorkcellSandboxSpec(bad, fixture.state, runtimeDir, workspace, fixture.provider, resolved, openCodeDefaultToolPath, lease, nil); err == nil {
		t.Fatal("unexpected readonly host mount was accepted")
	}
}
