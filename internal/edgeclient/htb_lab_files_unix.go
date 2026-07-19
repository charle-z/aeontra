//go:build !windows

package edgeclient

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func readHTBLabArtifact(workspace, relative string, limit int64) ([]byte, error) {
	rootFD, err := openHTBLabRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	fileFD, err := openHTBLabRelative(rootFD, relative, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("lab credential artifact is unavailable")
	}
	file := os.NewFile(uintptr(fileFD), "htb-lab-artifact")
	if file == nil {
		_ = unix.Close(fileFD)
		return nil, errors.New("lab credential artifact is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("lab credential artifact is unsafe")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("lab credential artifact is unavailable")
	}
	return body, nil
}

func writeHTBLabOutput(workspace, relative string, content []byte) error {
	rootFD, err := openHTBLabRoot(workspace)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	components, err := splitHTBLabRelative(relative)
	if err != nil || len(components) < 2 {
		return errors.New("lab SSH output path is invalid")
	}
	parentFD, err := walkHTBLabDirectories(rootFD, components[:len(components)-1])
	if err != nil {
		return errors.New("lab SSH output parent is unsafe")
	}
	defer unix.Close(parentFD)
	name := components[len(components)-1]
	var existing unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if existing.Mode&unix.S_IFMT != unix.S_IFREG {
			return errors.New("lab SSH output target is unsafe")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return errors.New("lab SSH output target is unavailable")
	}
	temporary, err := randomModelJournalID(".lab-output-")
	if err != nil {
		return errors.New("lab SSH output id generation failed")
	}
	fd, err := unix.Openat(parentFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("lab SSH output could not be staged")
	}
	file := os.NewFile(uintptr(fd), temporary)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("lab SSH output could not be staged")
	}
	staged := true
	defer func() {
		_ = file.Close()
		if staged {
			_ = unix.Unlinkat(parentFD, temporary, 0)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return errors.New("lab SSH output could not be staged")
	}
	if err := file.Sync(); err != nil {
		return errors.New("lab SSH output could not be staged")
	}
	if err := file.Close(); err != nil {
		return errors.New("lab SSH output could not be staged")
	}
	if err := unix.Renameat(parentFD, temporary, parentFD, name); err != nil {
		return errors.New("lab SSH output could not be saved")
	}
	staged = false
	return nil
}

func openHTBLabRoot(workspace string) (int, error) {
	clean := filepath.Clean(workspace)
	if !filepath.IsAbs(clean) {
		return -1, errors.New("lab workspace path is invalid")
	}
	fd, err := unix.Open(clean, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, errors.New("lab workspace is unavailable")
	}
	return fd, nil
}

func openHTBLabRelative(rootFD int, relative string, flags int, mode uint32) (int, error) {
	components, err := splitHTBLabRelative(relative)
	if err != nil {
		return -1, err
	}
	if len(components) == 1 {
		return unix.Openat(rootFD, components[0], flags, mode)
	}
	parentFD, err := walkHTBLabDirectories(rootFD, components[:len(components)-1])
	if err != nil {
		return -1, err
	}
	defer unix.Close(parentFD)
	return unix.Openat(parentFD, components[len(components)-1], flags, mode)
}

func walkHTBLabDirectories(rootFD int, components []string) (int, error) {
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, err
	}
	for _, component := range components {
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func splitHTBLabRelative(relative string) ([]string, error) {
	clean := filepath.Clean(strings.TrimSpace(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return nil, errors.New("lab path is invalid")
	}
	components := strings.Split(filepath.ToSlash(clean), "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, errors.New("lab path is invalid")
		}
	}
	return components, nil
}
