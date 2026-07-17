//go:build linux

package main

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type fileIdentity struct {
	device uint64
	inode  uint64
	mode   uint32
}

func identityFromFileInfo(info os.FileInfo) (fileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return fileIdentity{}, errors.New("filesystem identity is unavailable")
	}
	return fileIdentity{device: uint64(stat.Dev), inode: stat.Ino, mode: stat.Mode}, nil
}

func identityFromStat(stat *unix.Stat_t) fileIdentity {
	return fileIdentity{device: uint64(stat.Dev), inode: stat.Ino, mode: stat.Mode}
}

func verifyRepositoryDescriptors(root, repoID string, expectedRoot, expectedRepo, expectedManifest fileIdentity) error {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("repository registry root cannot be opened safely")
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil || identityFromStat(&rootStat) != expectedRoot {
		return errors.New("repository registry root identity changed")
	}
	repoFD, err := unix.Openat(rootFD, repoID, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("registered repository cannot be opened safely")
	}
	defer unix.Close(repoFD)
	var repoStat unix.Stat_t
	if err := unix.Fstat(repoFD, &repoStat); err != nil || identityFromStat(&repoStat) != expectedRepo {
		return errors.New("registered repository identity changed")
	}
	manifestFD, err := unix.Openat(repoFD, "package.json", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("registered repository manifest cannot be opened safely")
	}
	defer unix.Close(manifestFD)
	var manifestStat unix.Stat_t
	if err := unix.Fstat(manifestFD, &manifestStat); err != nil || manifestStat.Mode&unix.S_IFMT != unix.S_IFREG || manifestStat.Mode&0o022 != 0 || identityFromStat(&manifestStat) != expectedManifest {
		return errors.New("registered repository manifest identity is unsafe")
	}
	return nil
}
