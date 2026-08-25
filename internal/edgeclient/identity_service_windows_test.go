//go:build windows

package edgeclient

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLoadWindowsServiceIdentityAcceptsInstallerACLAndRejectsAnotherService(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
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
	serviceSID, err := windows.StringToSid("S-1-5-80-12345-23456-34567-45678-56789")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{identityPath, keyPath} {
		applyInstallerPrivateACL(t, path, false, serviceSID)
	}
	applyInstallerPrivateACL(t, root, true, serviceSID)

	loaded, _, err := loadWindowsServiceIdentityForSID(root, serviceSID)
	if err != nil {
		t.Fatalf("installer-owned identity rejected: %v", err)
	}
	if loaded.DeviceID != identity.DeviceID || loaded.Name != identity.Name {
		t.Fatalf("loaded identity = %#v", loaded)
	}
	otherSID, err := windows.StringToSid("S-1-5-80-98765-87654-76543-65432-54321")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadWindowsServiceIdentityForSID(root, otherSID); err == nil {
		t.Fatal("identity ACL owned by another service was accepted")
	}
}

func applyInstallerPrivateACL(t *testing.T, path string, directory bool, serviceSID *windows.SID) {
	t.Helper()
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:BAD:P(A;" + inheritance + ";FA;;;" + serviceSID.String() + ")(A;" + inheritance + ";FA;;;SY)(A;" + inheritance + ";FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		t.Fatalf("installer owner unavailable: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("installer DACL unavailable: %v", err)
	}
	handle, err := openWindowsSecurityHandle(path, directory, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}
