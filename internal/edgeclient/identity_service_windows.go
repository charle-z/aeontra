//go:build windows

package edgeclient

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// LoadWindowsServiceIdentity reads the paired identity while validating the
// private state against the fixed service identity that owns the runtime.
// It is read-only and does not reconcile ACLs under the operator token.
func LoadWindowsServiceIdentity(root, serviceIdentity string) (Identity, ed25519.PrivateKey, error) {
	serviceIdentity = strings.TrimSpace(serviceIdentity)
	if serviceIdentity == "" {
		return Identity{}, nil, errors.New("Windows service identity is required")
	}
	sid, _, _, err := windows.LookupSID("", serviceIdentity)
	if err != nil || sid == nil {
		return Identity{}, nil, errors.New("Windows service identity cannot be resolved")
	}
	return loadWindowsServiceIdentityForSID(root, sid)
}

func loadWindowsServiceIdentityForSID(root string, serviceSID *windows.SID) (Identity, ed25519.PrivateKey, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) || serviceSID == nil {
		return Identity{}, nil, errors.New("edge state root must be absolute")
	}
	if err := rejectSymlinkPath(root); err != nil {
		return Identity{}, nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Identity{}, nil, errors.New("edge state root is unsafe")
	}
	if err := validateWindowsPrivateRootForSID(root, serviceSID); err != nil {
		return Identity{}, nil, err
	}
	for _, name := range []string{identityFile, privateKeyFile} {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return Identity{}, nil, errors.New("edge private file is unsafe")
		}
		if err := validateWindowsPrivateACLForSID(path, false, serviceSID); err != nil {
			return Identity{}, nil, err
		}
	}
	return loadIdentityContents(root)
}
