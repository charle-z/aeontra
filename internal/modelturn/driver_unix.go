//go:build !windows

package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const DefaultDriverSocketName = "model-turn-driver.sock"

func ServeDriver(ctx context.Context, socketPath string, store *Store, ready io.Writer) error {
	validated, err := prepareDriverSocketPath(socketPath)
	if err != nil {
		return err
	}
	driver, err := NewDriver(store)
	if err != nil {
		return err
	}
	listener, err := net.Listen("unix", validated)
	if err != nil {
		return fmt.Errorf("listen model turn driver unix socket: %w", err)
	}
	defer listener.Close()
	if err := os.Chmod(validated, 0o600); err != nil {
		_ = os.Remove(validated)
		return errors.New("secure model turn driver socket permissions could not be applied")
	}
	if err := verifyDriverSocket(validated); err != nil {
		_ = os.Remove(validated)
		return err
	}
	defer removeOwnedDriverSocket(validated)

	server := &http.Server{
		Handler:           driver.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if ready != nil {
		message, _ := json.Marshal(map[string]any{
			"protocol_version": DriverProtocolVersion,
			"socket_path":      validated,
			"owner_uid":        os.Geteuid(),
		})
		_, _ = ready.Write(append(message, '\n'))
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = server.Close()
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func prepareDriverSocketPath(socketPath string) (string, error) {
	clean := filepath.Clean(socketPath)
	if clean == "." || !filepath.IsAbs(clean) {
		return "", errors.New("model turn driver socket path must be absolute")
	}
	parent := filepath.Dir(clean)
	if err := prepareTurnRoot(parent); err != nil {
		return "", fmt.Errorf("model turn driver socket directory is unsafe: %w", err)
	}
	if info, err := os.Lstat(clean); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !ownedByEffectiveUID(info) {
			return "", errors.New("existing model turn driver socket is unsafe")
		}
		connection, dialErr := net.DialTimeout("unix", clean, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return "", errors.New("model turn driver socket is already active")
		}
		if err := os.Remove(clean); err != nil {
			return "", errors.New("stale model turn driver socket could not be removed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("model turn driver socket path is unavailable")
	}
	return clean, nil
}

func verifyDriverSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		return errors.New("model turn driver socket is unavailable")
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByEffectiveUID(info) {
		return errors.New("model turn driver socket is not private to the Edge user")
	}
	return nil
}

func ownedByEffectiveUID(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

func removeOwnedDriverSocket(socketPath string) {
	info, err := os.Lstat(socketPath)
	if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode()&os.ModeSymlink == 0 && ownedByEffectiveUID(info) {
		_ = os.Remove(socketPath)
	}
}
