package app

import (
	"bytes"
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
	runtime, err := buildRuntime(serveOptions{Config: cfg, AuditPath: filepath.Join(root, "audit.log")})
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
	if strings.Contains(output.String(), admin.Token) {
		t.Fatal("startup diagnostics must not print the admin token")
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
}
