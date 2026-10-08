package windows

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsScriptsAllowManagedRootsOnAnyFixedLocalDrive(t *testing.T) {
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
		"Assert-ManagedDataRoot $StateRoot 'State' 'StateRoot'",
		"Assert-ManagedDataRoot $WorkspaceRoot 'Workspaces' 'WorkspaceRoot'",
		"InstallRoot must use a ready fixed local drive.",
		`$Label must use a ready fixed local drive.`,
		"InstallRoot must end in Aeontra\\Edge.",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer missing %q", required)
		}
	}
	for _, required := range []string{
		"Resolve-ManagedInstallRoot $InstallRoot",
		"Resolve-ManagedDataRoot $StateRoot 'State' 'StateRoot'",
		"Resolve-ManagedDataRoot $WorkspaceRoot 'Workspaces' 'WorkspaceRoot'",
		"InstallRoot must use a ready fixed local drive.",
		"InstallRoot must end in Aeontra\\Edge.",
	} {
		if !strings.Contains(uninstall, required) {
			t.Errorf("uninstaller missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"StateRoot is fixed to managed ProgramData",
		"WorkspaceRoot must remain under ProgramData",
		"Resolve-ManagedRoot $StateRoot $programData",
	} {
		if strings.Contains(install+uninstall, forbidden) {
			t.Fatalf("Windows root contract still contains %q", forbidden)
		}
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

func TestWindowsInstallerBuildsControlPathsAfterRootValidation(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install := string(installBytes)
	validation := strings.Index(install, "$null = Assert-ManagedDataRoot $WorkspaceRoot 'Workspaces' 'WorkspaceRoot'")
	pairRequest := strings.LastIndex(install, "$pairRequest = Join-Path $StateRoot 'pair-request.json'")
	serviceConfig := strings.LastIndex(install, "$serviceConfig = Join-Path $InstallRoot 'service-config.json'")
	if validation < 0 || pairRequest < validation || serviceConfig < validation {
		t.Fatal("installer derives control files before roots are canonical and validated")
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

func TestWindowsInstallerGrantsOnlyOperatorReadAccessToWorkspaces(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install := string(installBytes)
	for _, required := range []string{
		"[Security.Principal.WindowsIdentity]::GetCurrent().User",
		"[Security.AccessControl.FileSystemRights]::ReadAndExecute",
		"Set-PrivateDirectoryAcl $WorkspaceRoot ([Security.AccessControl.FileSystemRights]::Modify) $true",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer missing operator workspace read contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"Set-PrivateDirectoryAcl $InstallRoot ([Security.AccessControl.FileSystemRights]::ReadAndExecute) $true",
		"Set-PrivateDirectoryAcl $StateRoot ([Security.AccessControl.FileSystemRights]::FullControl) $true",
	} {
		if strings.Contains(install, forbidden) {
			t.Errorf("installer exposes private root through %q", forbidden)
		}
	}
}

func TestWindowsInstallerReportsBoundedServiceStartupStage(t *testing.T) {
	installBytes, err := os.ReadFile("install-edge.ps1")
	if err != nil {
		t.Fatal(err)
	}
	install := string(installBytes)
	for _, required := range []string{
		"ServiceSpecificExitCode",
		"10 { 'configuration' }",
		"11 { 'service-authority' }",
		"12 { 'workspace' }",
		"13 { 'pairing' }",
		"14 { 'transport' }",
		"15 { 'workspace-registry' }",
		"16 { 'project-process-state' }",
		"17 { 'runtime' }",
		"AeontraEdge failed during $serviceStage startup (service code $serviceCode).",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("installer missing bounded startup diagnostic %q", required)
		}
	}
}
