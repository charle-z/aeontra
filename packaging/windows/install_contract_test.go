package windows

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsScriptsAllowOnlyManagedCustomInstallRoot(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	uninstallBytes, err := os.ReadFile("uninstall-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install, uninstall := string(installBytes), string(uninstallBytes)
	for _, required := range []string{
		"Assert-ManagedInstallRoot $InstallRoot",
		"InstallRoot must use a ready fixed local drive.",
		"InstallRoot must end in Aeontra\\Edge.",
		"StateRoot is fixed to managed ProgramData",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer missing %q", required)
		}
	}
	for _, required := range []string{
		"Resolve-ManagedInstallRoot $InstallRoot",
		"InstallRoot must use a ready fixed local drive.",
		"InstallRoot must end in Aeontra\\Edge.",
	} {
		if !strings.Contains(uninstall, required) {
			t.Errorf("uninstaller missing %q", required)
		}
	}
	if strings.Contains(install, "Install/state roots are fixed to Windows known folders") {
		t.Fatal("installer still rejects every non-default fixed-drive install root")
	}
}
