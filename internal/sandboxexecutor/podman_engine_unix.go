//go:build !windows

package sandboxexecutor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func NewPodmanEngine(binary, socket string) (Engine, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(socket) {
		return nil, errors.New("podman binary and socket paths must be absolute")
	}
	resolvedBinary, err := filepath.EvalSymlinks(filepath.Clean(binary))
	if err != nil {
		return nil, errors.New("resolving Podman binary")
	}
	binaryInfo, err := os.Stat(resolvedBinary)
	if err != nil || !binaryInfo.Mode().IsRegular() || binaryInfo.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("podman binary is not a regular executable")
	}
	cleanSocket := filepath.Clean(socket)
	resolvedSocket, err := filepath.EvalSymlinks(cleanSocket)
	if err != nil || resolvedSocket != cleanSocket {
		return nil, errors.New("rootless Podman socket path contains a symlink")
	}
	info, err := os.Lstat(cleanSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("rootless Podman endpoint is not a direct Unix socket")
	}
	uid := os.Geteuid()
	gid := os.Getegid()
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != uid {
		return nil, errors.New("rootless Podman socket is not owned by the executor identity")
	}
	expectedPrefix := fmt.Sprintf("/run/user/%d/", uid)
	if !strings.HasPrefix(cleanSocket, expectedPrefix) {
		return nil, errors.New("podman socket is outside the executor user runtime")
	}
	engine := &podmanEngine{socket: cleanSocket, binary: resolvedBinary, uid: uid, gid: gid}
	engine.run = realPodmanCommand(engine.binary, engine.socket)
	return engine, nil
}
