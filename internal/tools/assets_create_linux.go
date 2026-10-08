//go:build linux

package tools

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func assetDirectoryIdentity(info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return "", errors.New("asset directory identity unavailable")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

// Pin root and parent descriptors, refuse every symlink and use exclusive-create.
// A failed write leaves explicit recoverable state; uncertain bytes are not deleted.
func createAssetBeneath(root, path, rootIdentity, parentIdentity string, body []byte) error {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("asset repository unavailable")
	}
	defer rootHandle.Close()
	rootInfo, err := rootHandle.Stat(".")
	if err != nil {
		return errors.New("asset repository unavailable")
	}
	identity, err := assetDirectoryIdentity(rootInfo)
	if err != nil || identity != rootIdentity {
		return errors.New("asset repository identity changed")
	}
	// Openat2 independently rejects symlink components rather than following an
	// in-jail link whose destination could be a secret-shaped directory.
	rootFile, err := rootHandle.Open(".")
	if err != nil {
		return errors.New("asset repository unavailable")
	}
	defer rootFile.Close()
	parentFD, err := unix.Openat2(int(rootFile.Fd()), filepath.Dir(path), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return errors.New("asset destination parent unavailable")
	}
	parent := os.NewFile(uintptr(parentFD), filepath.Dir(path))
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil {
		return errors.New("asset destination parent unavailable")
	}
	identity, err = assetDirectoryIdentity(parentInfo)
	if err != nil || identity != parentIdentity {
		return errors.New("asset destination parent identity changed")
	}
	fd, err := unix.Openat2(parentFD, filepath.Base(path), &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0644, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return errors.New("asset destination create refused; no overwrite performed")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	n, writeErr := file.Write(body)
	syncErr := file.Sync()
	writtenInfo, statErr := file.Stat()
	closeErr := file.Close()
	if writeErr != nil || n != len(body) || syncErr != nil || statErr != nil || closeErr != nil {
		return fmt.Errorf("asset materialization incomplete: new file may remain at reviewed path %q (%d bytes written); inspect before retry", filepath.ToSlash(path), n)
	}
	observed, observedInfo, err := openRegularBeneath(root, path)
	if err != nil {
		return fmt.Errorf("asset materialization incomplete: new file was written but reviewed path %q could not be verified; inspect before retry", filepath.ToSlash(path))
	}
	defer observed.Close()
	if !os.SameFile(writtenInfo, observedInfo) {
		return fmt.Errorf("asset materialization incomplete: reviewed path %q changed after write; inspect before retry", filepath.ToSlash(path))
	}
	observedBytes, err := io.ReadAll(io.LimitReader(observed, int64(len(body))+1))
	if err != nil || sha256.Sum256(observedBytes) != sha256.Sum256(body) {
		return fmt.Errorf("asset materialization incomplete: bytes at reviewed path %q changed after write; inspect before retry", filepath.ToSlash(path))
	}
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("asset materialization incomplete: reviewed path %q was written but directory durability is unconfirmed; inspect before retry", filepath.ToSlash(path))
	}
	return nil
}
