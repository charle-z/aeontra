package app

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func TestGrantUsesPrivateDescriptorWithoutBearerArgument(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeAsk, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, ".env")
	if err := os.WriteFile(secret, []byte("TOKEN=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	required, err := pol.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("b", 64)
	server := httptest.NewServer(grantadmin.Handler(pol, audit.New(&bytes.Buffer{}), token))
	defer server.Close()
	descriptorPath, err := writeGrantAdminDescriptor(root, server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(descriptorPath)
	if err := grant([]string{"--admin-file", descriptorPath, "--ttl", "1m", required.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pol.ConsumeReadGrant(required.ID, secret, false); err != nil {
		t.Fatalf("private descriptor read approval was not consumable: %v", err)
	}
}

func TestGrantAdminDescriptorPermissionsAndStrictParsing(t *testing.T) {
	root := t.TempDir()
	path, err := writeGrantAdminDescriptor(root, "http://127.0.0.1:8765", strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("descriptor mode=%#o", info.Mode().Perm())
	}
	directoryInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode=%#o", directoryInfo.Mode().Perm())
	}
	if _, err := readGrantAdminDescriptor(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"base_url":"http://127.0.0.1:8765","token":"`+strings.Repeat("c", 64)+`","created_at":"`+time.Now().UTC().Format(time.RFC3339Nano)+`","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGrantAdminDescriptor(path); err == nil {
		t.Fatal("descriptor accepted an unknown field")
	}
}

func TestGrantAdminDescriptorRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unprivileged symlink creation is not portable on Windows")
	}
	root := t.TempDir()
	realPath, err := writeGrantAdminDescriptor(root, "http://127.0.0.1:8765", strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "descriptor-link.json")
	if err := os.Symlink(realPath, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readGrantAdminDescriptor(link); err == nil {
		t.Fatal("symlink descriptor was accepted")
	}
}
