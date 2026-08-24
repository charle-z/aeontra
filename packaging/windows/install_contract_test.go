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

func TestWindowsInstallerPreservesQuotedServiceArgumentsForSCM(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install := string(installBytes)
	for _, required := range []string{
		`$quotedBinary = '\"' + $targetBinary + '\" windows-agent`,
		`--state \"' + $StateRoot + '\"`,
		`--root \"' + $WorkspaceRoot + '\"`,
		`--service-identity \"' + $serviceIdentity + '\"`,
		`--pair-request \"' + $pairRequest + '\"`,
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer does not preserve SCM argument %q", required)
		}
	}
}

func TestWindowsInstallerUsesLocaleIndependentAclIdentities(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install := string(installBytes)
	for _, required := range []string{
		"[Security.Principal.SecurityIdentifier]::new('S-1-5-18')",
		"[Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer missing locale-independent identity %q", required)
		}
	}
	for _, forbidden := range []string{
		"[Security.Principal.NTAccount]::new('NT AUTHORITY\\SYSTEM')",
		"[Security.Principal.NTAccount]::new('BUILTIN\\Administrators')",
	} {
		if strings.Contains(install, forbidden) {
			t.Errorf("installer retains locale-dependent identity %q", forbidden)
		}
	}
}
