package app

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const (
	grantAdminDirectory = "grant-admin"
	grantAdminMaxBytes  = 4096
)

type grantAdminDescriptor struct {
	SchemaVersion int       `json:"schema_version"`
	BaseURL       string    `json:"base_url"`
	Token         string    `json:"token"`
	CreatedAt     time.Time `json:"created_at"`
}

func writeGrantAdminDescriptor(stateRoot, baseURL, token string) (string, error) {
	descriptor := grantAdminDescriptor{SchemaVersion: 1, BaseURL: baseURL, Token: token, CreatedAt: time.Now().UTC()}
	if err := validateGrantAdminDescriptor(descriptor); err != nil {
		return "", err
	}
	directory := filepath.Join(stateRoot, grantAdminDirectory)
	if err := edgeclient.PreparePrivateRoot(directory); err != nil {
		return "", fmt.Errorf("securing grant admin directory: %w", err)
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, "channel-*.json")
	if err != nil {
		return "", fmt.Errorf("creating grant admin descriptor: %w", err)
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := edgeclient.SecurePrivateOpenFile(file); err != nil {
		return "", fmt.Errorf("securing grant admin descriptor: %w", err)
	}
	if err := json.NewEncoder(file).Encode(descriptor); err != nil {
		return "", fmt.Errorf("encoding grant admin descriptor: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("syncing grant admin descriptor: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("closing grant admin descriptor: %w", err)
	}
	ok = true
	return path, nil
}

func readGrantAdminDescriptor(path string) (grantAdminDescriptor, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return grantAdminDescriptor{}, fmt.Errorf("--admin-file must be an absolute private path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return grantAdminDescriptor{}, fmt.Errorf("reading grant admin descriptor: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > grantAdminMaxBytes {
		return grantAdminDescriptor{}, fmt.Errorf("grant admin descriptor is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return grantAdminDescriptor{}, fmt.Errorf("opening grant admin descriptor: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return grantAdminDescriptor{}, fmt.Errorf("grant admin descriptor changed while opening")
	}
	if err := edgeclient.ValidatePrivateOpenFile(file); err != nil {
		return grantAdminDescriptor{}, fmt.Errorf("grant admin descriptor permissions are unsafe: %w", err)
	}
	decoder := json.NewDecoder(io.LimitReader(file, grantAdminMaxBytes+1))
	decoder.DisallowUnknownFields()
	var descriptor grantAdminDescriptor
	if err := decoder.Decode(&descriptor); err != nil {
		return grantAdminDescriptor{}, fmt.Errorf("decoding grant admin descriptor: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return grantAdminDescriptor{}, fmt.Errorf("grant admin descriptor has trailing data")
	}
	if err := validateGrantAdminDescriptor(descriptor); err != nil {
		return grantAdminDescriptor{}, err
	}
	return descriptor, nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reading grant admin directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("grant admin directory is not a real directory")
	}
	return edgeclient.PreparePrivateRoot(path)
}

func validateGrantAdminDescriptor(descriptor grantAdminDescriptor) error {
	if descriptor.SchemaVersion != 1 {
		return fmt.Errorf("unsupported grant admin descriptor schema")
	}
	parsed, err := url.Parse(descriptor.BaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("grant admin descriptor URL must be an exact loopback HTTP origin")
	}
	if _, err := net.LookupPort("tcp", parsed.Port()); err != nil {
		return fmt.Errorf("grant admin descriptor has an invalid port")
	}
	if len(descriptor.Token) != 64 {
		return fmt.Errorf("grant admin descriptor token is invalid")
	}
	if _, err := hex.DecodeString(descriptor.Token); err != nil {
		return fmt.Errorf("grant admin descriptor token is invalid")
	}
	if descriptor.CreatedAt.IsZero() {
		return fmt.Errorf("grant admin descriptor timestamp is missing")
	}
	return nil
}
