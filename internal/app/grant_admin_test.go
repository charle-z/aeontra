package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestStartLocalGrantAdminUsesLoopbackAndCloses(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime, err := buildRuntime(serveOptions{Config: cfg, StateRoot: state, AuditPath: filepath.Join(state, "logs", "audit.log")})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	var output bytes.Buffer
	admin, err := startLocalGrantAdmin(runtime, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(admin.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("base URL = %q", admin.BaseURL)
	}
	if len(admin.Token) != 64 {
		t.Fatalf("token length = %d", len(admin.Token))
	}
	if descriptor, err := readGrantAdminDescriptor(admin.DescriptorPath); err != nil || descriptor.Token != admin.Token || descriptor.BaseURL != admin.BaseURL {
		t.Fatalf("private descriptor mismatch: %#v err=%v", descriptor, err)
	}
	for _, secret := range []string{admin.Token, admin.BaseURL} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("startup diagnostics exposed private channel material %q", secret)
		}
	}
	if !strings.Contains(output.String(), admin.DescriptorPath) {
		t.Fatal("startup diagnostics omitted the private descriptor path needed by the local operator")
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(admin.DescriptorPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descriptor remained after close: %v", err)
	}
}

func TestLocalGrantAdminNotifiesReadRequestWithoutExposingChannelCredentials(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeAsk, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime, err := buildRuntime(serveOptions{Config: cfg, StateRoot: state, AuditPath: filepath.Join(state, "logs", "audit.log")})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var output bytes.Buffer
	admin, err := startLocalGrantAdmin(runtime, &output)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	secret := filepath.Join(root, ".env")
	if err := os.WriteFile(secret, []byte("TOKEN=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	required, err := runtime.Policy.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatalf("missing read request: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "ACCESS REQUIRED") || !strings.Contains(got, required.ID) || !strings.Contains(got, secret) {
		t.Fatalf("missing operator instructions: %s", got)
	}
	for _, secret := range []string{admin.Token, admin.BaseURL} {
		if strings.Contains(got, secret) {
			t.Fatalf("operator notification exposed channel credential %q", secret)
		}
	}
	if !strings.Contains(got, admin.DescriptorPath) {
		t.Fatal("operator notification omitted the private descriptor path")
	}
}
