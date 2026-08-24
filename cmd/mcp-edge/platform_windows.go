//go:build windows

package main

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func ensureWorkcellUser() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return errors.New("Windows workcell service identity is unavailable")
	}
	defer token.Close()
	if token.IsElevated() {
		return errors.New("Windows workcell refuses to run with an elevated token")
	}
	return nil
}

func ensureWindowsServiceIdentity(expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return errors.New("Windows service identity is required")
	}
	var expectedSID *windows.SID
	if strings.HasPrefix(expected, "S-") {
		var err error
		expectedSID, err = windows.StringToSid(expected)
		if err != nil {
			return errors.New("Windows service identity is invalid")
		}
	} else {
		account, err := windows.UTF16PtrFromString(expected)
		if err != nil {
			return errors.New("Windows service identity is invalid")
		}
		var sidSize, domainSize uint32
		var use uint32
		if err := windows.LookupAccountName(nil, account, nil, &sidSize, nil, &domainSize, &use); err != windows.ERROR_INSUFFICIENT_BUFFER || sidSize == 0 {
			return errors.New("Windows service identity cannot be resolved")
		}
		sidBuffer := make([]byte, sidSize)
		domainBuffer := make([]uint16, domainSize)
		expectedSID = (*windows.SID)(unsafe.Pointer(&sidBuffer[0]))
		if err := windows.LookupAccountName(nil, account, expectedSID, &sidSize, &domainBuffer[0], &domainSize, &use); err != nil {
			return errors.New("Windows service identity cannot be resolved")
		}
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return errors.New("Windows service identity is unavailable")
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil || !user.User.Sid.Equals(expectedSID) {
		return errors.New("Windows Edge must run as the configured service identity")
	}
	return nil
}
