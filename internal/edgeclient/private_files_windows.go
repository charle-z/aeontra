//go:build windows

package edgeclient

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

var privateFilesIsWindowsService = svc.IsWindowsService

func securePrivateRoot(path string, created bool) error {
	if !created {
		return validateWindowsPrivateACL(path, true)
	}
	handle, err := openWindowsSecurityHandle(path, true, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return applyCurrentIdentityPrivateACL(handle, true)
}

func validatePrivateRootPlatform(path string, _ os.FileInfo) error {
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		return errors.New("edge state root owner is unavailable")
	}
	defer token.Close()
	return validateWindowsPrivateRootForSID(path, sid)
}

func validateWindowsPrivateRootForSID(path string, sid *windows.SID) error {
	if !IsWindowsLocalPath(path) {
		return errors.New("edge state root namespace is unsafe")
	}
	handle, identity, err := openAndInspectWindowsDirectory(path)
	if err != nil {
		return errors.New("edge state root namespace is unsafe")
	}
	_ = windows.CloseHandle(handle)
	requested, requestedOK := normalizeWindowsComparablePath(path)
	resolved, resolvedOK := normalizeWindowsComparablePath(identity.FinalPath)
	if !requestedOK || !resolvedOK || !strings.EqualFold(trimWindowsTrailingSeparators(requested), trimWindowsTrailingSeparators(resolved)) {
		return errors.New("edge state root namespace changed")
	}
	return validateWindowsPrivateACLForSID(path, true, sid)
}

func securePrivateFile(file *os.File) error {
	if file == nil {
		return errors.New("edge private file is unavailable")
	}
	if file.Fd() == ^uintptr(0) {
		return errors.New("edge private file is unavailable")
	}
	handle, err := openWindowsSecurityHandle(file.Name(), false, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return applyCurrentIdentityPrivateACL(handle, false)
}

func reconcilePrivateRegularFilePlatform(path string) error {
	isService, err := privateFilesIsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return nil
	}
	handle, err := openWindowsSecurityHandle(path, false, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return applyCurrentIdentityPrivateACL(handle, false)
}

func validatePrivateFilePlatform(path string, _ os.FileInfo) error {
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		return errors.New("edge private path owner is unavailable")
	}
	defer token.Close()
	return validateWindowsPrivateACLForSID(path, false, sid)
}

func validateWindowsPrivateACL(path string, directory bool) error {
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		return errors.New("edge private path owner is unavailable")
	}
	defer token.Close()
	return validateWindowsPrivateACLForSID(path, directory, sid)
}

func validateWindowsPrivateACLForSID(path string, directory bool, sid *windows.SID) error {
	if sid == nil {
		return errors.New("edge private path owner is unavailable")
	}
	handle, err := openWindowsSecurityHandle(path, directory, windows.READ_CONTROL)
	if err != nil {
		return errors.New("edge private path ACL is unavailable")
	}
	defer windows.CloseHandle(handle)
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || descriptor == nil {
		return errors.New("edge private path ACL is unavailable")
	}
	if err := validateWindowsSecurityDescriptor(descriptor, sid, true); err != nil {
		return errors.New("edge private path ACL is unsafe")
	}
	return nil
}

func applyCurrentIdentityPrivateACL(handle windows.Handle, directory bool) error {
	token, sid, err := currentWindowsTokenSID()
	if err != nil {
		return err
	}
	defer token.Close()
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	sidText := sid.String()
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;" + inheritance + ";FA;;;" + sidText + ")(A;" + inheritance + ";FA;;;SY)(A;" + inheritance + ";FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("edge private ACL is unavailable")
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func currentWindowsTokenSID() (windows.Token, *windows.SID, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return 0, nil, err
	}
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		token.Close()
		return 0, nil, errors.New("windows token user is unavailable")
	}
	return token, user.User.Sid, nil
}

func openWindowsSecurityHandle(path string, directory bool, access uint32) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	return windows.CreateFile(pathPtr, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
}
