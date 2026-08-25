//go:build windows

package edgeclient

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestPrivateFileACLRetainsElevatedOperatorAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := openWindowsSecurityHandle(path, false, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyCurrentIdentityPrivateACL(handle, false); err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatal(err)
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	_ = windows.CloseHandle(handle)
	if err != nil || descriptor == nil {
		t.Fatalf("private file descriptor unavailable: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("private file DACL unavailable: %v", err)
	}
	want := map[windows.WELL_KNOWN_SID_TYPE]bool{
		windows.WinLocalSystemSid:           false,
		windows.WinBuiltinAdministratorsSid: false,
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		for kind := range want {
			if sid.IsWellKnown(kind) {
				want[kind] = true
			}
		}
	}
	for kind, present := range want {
		if !present {
			t.Fatalf("private file DACL is missing trusted principal %d", kind)
		}
	}
}

func TestLoadIdentityReconcilesLegacyWindowsOperatorACL(t *testing.T) {
	previousIsService := privateFilesIsWindowsService
	privateFilesIsWindowsService = func() (bool, error) { return true, nil }
	t.Cleanup(func() { privateFilesIsWindowsService = previousIsService })
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rootHandle, err := openWindowsSecurityHandle(root, true, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyCurrentIdentityPrivateACL(rootHandle, true); err != nil {
		_ = windows.CloseHandle(rootHandle)
		t.Fatal(err)
	}
	_ = windows.CloseHandle(rootHandle)
	identity := Identity{SchemaVersion: 1, ServerURL: "https://example.com", DeviceID: "ed_" + strings.Repeat("a", 32), Name: "windows-test"}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(root, identityFile)
	keyPath := filepath.Join(root, privateKeyFile)
	if err := os.WriteFile(identityPath, append(identityJSON, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(base64.RawURLEncoding.EncodeToString(make([]byte, 64))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{identityPath, keyPath} {
		applyLegacyWindowsPrivateFileACL(t, path)
	}
	if _, _, err := LoadIdentity(root); err != nil {
		t.Fatalf("legacy private identity was not reconciled: %v", err)
	}
	for _, path := range []string{identityPath, keyPath} {
		if !windowsPrivateFileHasWellKnownSID(t, path, windows.WinBuiltinAdministratorsSid) {
			t.Fatalf("operator ACL was not reconciled for %s", filepath.Base(path))
		}
	}
}

func TestPrivateFileACLReconciliationIsServiceOwned(t *testing.T) {
	previousIsService := privateFilesIsWindowsService
	privateFilesIsWindowsService = func() (bool, error) { return false, nil }
	t.Cleanup(func() { privateFilesIsWindowsService = previousIsService })
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	applyLegacyWindowsPrivateFileACL(t, path)
	if err := reconcilePrivateRegularFilePlatform(path); err != nil {
		t.Fatal(err)
	}
	if windowsPrivateFileHasWellKnownSID(t, path, windows.WinBuiltinAdministratorsSid) {
		t.Fatal("non-service operator rewrote the private file ACL")
	}
}

func applyLegacyWindowsPrivateFileACL(t *testing.T, path string) {
	t.Helper()
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("legacy DACL unavailable: %v", err)
	}
	handle, err := openWindowsSecurityHandle(path, false, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func windowsPrivateFileHasWellKnownSID(t *testing.T, path string, kind windows.WELL_KNOWN_SID_TYPE) bool {
	t.Helper()
	handle, err := openWindowsSecurityHandle(path, false, windows.READ_CONTROL)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	_ = windows.CloseHandle(handle)
	if err != nil || descriptor == nil {
		t.Fatalf("private file descriptor unavailable: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("private file DACL unavailable: %v", err)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		if (*windows.SID)(unsafe.Pointer(&ace.SidStart)).IsWellKnown(kind) {
			return true
		}
	}
	return false
}
