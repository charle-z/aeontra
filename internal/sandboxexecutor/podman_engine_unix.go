//go:build !windows

package sandboxexecutor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func NewPodmanEngine(socket string) (Engine, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("podman socket path must be absolute")
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
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", cleanSocket)
		},
		DisableCompression:  true,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	return &podmanEngine{
		socket: cleanSocket, uid: uid, gid: gid,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}
