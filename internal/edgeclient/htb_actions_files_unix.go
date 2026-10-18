//go:build !windows

package edgeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const htbArtifactStatusLimit = int64(4 << 20)

func inspectHTBArtifact(workspace, relative string) (HTBArtifactStatus, error) {
	status := HTBArtifactStatus{Path: relative}
	rootFD, err := openHTBLabRoot(workspace)
	if err != nil {
		return status, err
	}
	defer unix.Close(rootFD)
	fileFD, err := openHTBLabRelative(rootFD, relative, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return status, nil
	}
	if err != nil {
		return status, errors.New("HTB artifact metadata is unavailable")
	}
	file := os.NewFile(uintptr(fileFD), "htb-lab-status-artifact")
	if file == nil {
		_ = unix.Close(fileFD)
		return status, errors.New("HTB artifact metadata is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > htbArtifactStatusLimit {
		return status, errors.New("HTB artifact metadata is unsafe")
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(file, htbArtifactStatusLimit+1))
	if err != nil || read != info.Size() {
		return status, errors.New("HTB artifact metadata is unavailable")
	}
	status.Exists = true
	status.NonEmpty = info.Size() > 0
	status.Bytes = info.Size()
	status.SHA256 = hex.EncodeToString(hash.Sum(nil))
	status.Mode = info.Mode().Perm().String()
	return status, nil
}
