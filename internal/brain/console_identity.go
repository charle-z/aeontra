package brain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	consoleIdentityBytes = 32
	consoleNodeIDPrefix  = "bn_"
)

// ConfigureConsoleIdentity loads or creates the persistent private key used only
// to derive stable opaque console node IDs. The raw key never leaves this store.
func (s *Store) ConfigureConsoleIdentity(path string) error {
	if s == nil {
		return errors.New("brain: store is unavailable")
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" || !filepath.IsAbs(path) {
		return errors.New("brain: console identity path must be absolute")
	}
	directory := filepath.Dir(path)
	if err := rejectSymlinkAncestors(directory); err != nil {
		return err
	}
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return errors.New("brain: console identity directory is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("brain: console identity directory is unavailable")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New("brain: console identity directory is unavailable")
	}
	if err := rejectSymlinkAncestors(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("brain: console identity directory is unsafe")
	}

	key, err := loadOrCreateConsoleIdentity(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.consoleKey = append(s.consoleKey[:0], key...)
	s.mu.Unlock()
	return nil
}

func loadOrCreateConsoleIdentity(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, consoleIdentityBytes)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, errors.New("brain: console identity generation failed")
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), ".console-node.key.tmp-")
		if err != nil {
			return nil, errors.New("brain: console identity creation failed")
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return nil, errors.New("brain: console identity persistence failed")
		}
		if _, err := temporary.Write(key); err != nil {
			_ = temporary.Close()
			return nil, errors.New("brain: console identity persistence failed")
		}
		if err := temporary.Sync(); err != nil {
			_ = temporary.Close()
			return nil, errors.New("brain: console identity persistence failed")
		}
		if err := temporary.Close(); err != nil {
			return nil, errors.New("brain: console identity persistence failed")
		}
		if err := os.Link(temporaryPath, path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, errors.New("brain: console identity publication failed")
		}
		if err := syncConsoleIdentityDirectory(filepath.Dir(path)); err != nil {
			return nil, err
		}
		return readConsoleIdentity(path)
	}
	if err != nil {
		return nil, errors.New("brain: console identity file is unavailable")
	}
	return readConsoleIdentityWithInfo(path, info)
}

func readConsoleIdentity(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("brain: console identity file is unavailable")
	}
	return readConsoleIdentityWithInfo(path, info)
}

func readConsoleIdentityWithInfo(path string, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() != consoleIdentityBytes {
		return nil, errors.New("brain: console identity file is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("brain: console identity file is unavailable")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || openedInfo.Mode().Perm() != 0o600 || openedInfo.Size() != consoleIdentityBytes {
		return nil, errors.New("brain: console identity file changed during access")
	}
	key, err := io.ReadAll(io.LimitReader(file, consoleIdentityBytes+1))
	if err != nil || len(key) != consoleIdentityBytes {
		return nil, errors.New("brain: console identity file is unavailable")
	}
	return key, nil
}

func syncConsoleIdentityDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return errors.New("brain: console identity directory is unavailable")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("brain: console identity directory sync failed")
	}
	return nil
}

func (s *Store) consoleNodeID(slug string) (string, error) {
	if s == nil {
		return "", errors.New("brain: store is unavailable")
	}
	s.mu.Lock()
	key := append([]byte(nil), s.consoleKey...)
	s.mu.Unlock()
	if len(key) != consoleIdentityBytes {
		return "", errors.New("brain: console identity is unavailable")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprint(mac, "mcp-devbox:brain-console-node:v1\x00", slug)
	digest := mac.Sum(nil)
	return consoleNodeIDPrefix + base64.RawURLEncoding.EncodeToString(digest[:18]), nil
}
