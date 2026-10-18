package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func newTestService(t testing.TB, mode config.Mode) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            mode,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve root the same way the jail does, so command-dir comparisons match.
	resolvedRoot := pol.Roots()[0]
	svc := NewService(pol, audit.New(&bytes.Buffer{}), resolvedRoot)
	return svc, resolvedRoot
}

func write(t testing.TB, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFile_ReturnsContent(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, "main.go", "package main\n")
	out, err := svc.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "package main") {
		t.Errorf("unexpected content: %q", out)
	}
}

func TestReadFile_DeniesSecretByPath(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, ".env", "API_KEY=supersecretvalue123")
	_, err := svc.ReadFile(filepath.Join(root, ".env"))
	if err == nil {
		t.Fatal("reading .env should require access")
	}
	var required *policy.AccessRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("secret read should return access-required, got %v", err)
	}
	if required.Type != "access-required" || required.RequestID == "" {
		t.Fatalf("access-required should be structured with request id: %#v", required)
	}
}

func TestReadFile_AccessRequestIsAudited(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var auditBuf bytes.Buffer
	svc := NewService(pol, audit.New(&auditBuf), pol.Roots()[0])
	secret := write(t, root, ".env", "API_KEY=supersecretvalue123")
	_, _ = svc.ReadFile(secret)
	log := auditBuf.String()
	if !strings.Contains(log, `"decision":"ask"`) ||
		!strings.Contains(log, `"tool":"read_file"`) ||
		!strings.Contains(log, `\"type\":\"access-required\"`) {
		t.Fatalf("access request should be audited as ask, got %s", log)
	}
}

func TestReadFile_RedactsSecretInContent(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	// A secret living inside a legitimately-named source file.
	write(t, root, "config.go", "package config\nconst Key = \"gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz\"\n")
	out, err := svc.ReadFile(filepath.Join(root, "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret leaked through read_file: %q", out)
	}
}

func TestReadFile_DeniesOutsideJail(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	if _, err := svc.ReadFile(filepath.Join(root, "..", "escape.txt")); err == nil {
		t.Error("reading outside the jail should be denied")
	}
}

func TestReadManyFiles_BatchesAndMarksDenied(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, "a.go", "package a\n")
	write(t, root, ".env", "SECRET=x")
	out, err := svc.ReadManyFiles([]string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, ".env"),
		filepath.Join(root, "..", "escape.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "package a") {
		t.Errorf("legit file content missing: %q", out)
	}
	if !strings.Contains(out, "denied") {
		t.Errorf("denied files should be marked inline: %q", out)
	}
}

func TestReadFile_RejectsBinary(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	p := filepath.Join(root, "blob.bin")
	if err := os.WriteFile(p, []byte{0x00, 0x01, 0x02, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadFile(p); err == nil {
		t.Error("binary file should be rejected")
	}
}

func TestReadFile_GrantReadsSecretRedactedAndSingleUse(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	secret := write(t, root, ".env", "API_KEY=supersecretvalue123")

	_, err := svc.ReadFile(secret)
	var required *policy.AccessRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("secret read should return access-required, got %v", err)
	}
	if _, err := svc.pol.ApproveReadAccess(required.RequestID, false, time.Minute); err != nil {
		t.Fatal(err)
	}

	out, err := svc.ReadFileWithAccess(secret, required.RequestID, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "supersecretvalue123") {
		t.Fatalf("normal grant should still redact secret value: %q", out)
	}

	if _, err := svc.ReadFileWithAccess(secret, required.RequestID, false); !errors.Is(err, policy.ErrAccessGrantUsed) {
		t.Fatalf("grant should be single-use, got %v", err)
	}
}

func TestReadFile_RawRequiresRawGrant(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	secret := write(t, root, ".env", "API_KEY=supersecretvalue123")

	_, err := svc.ReadFileWithAccess(secret, "", true)
	var required *policy.AccessRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("raw secret read should return access-required, got %v", err)
	}
	if !required.RawRequested {
		t.Fatalf("raw access request should record raw intent: %#v", required)
	}
	if _, err := svc.pol.ApproveReadAccess(required.RequestID, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadFileWithAccess(secret, required.RequestID, true); !errors.Is(err, policy.ErrRawAccessDenied) {
		t.Fatalf("raw read with normal grant should be denied, got %v", err)
	}

	_, err = svc.ReadFileWithAccess(secret, "", true)
	if !errors.As(err, &required) {
		t.Fatalf("second raw secret read should return access-required, got %v", err)
	}
	if _, err := svc.pol.ApproveReadAccess(required.RequestID, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ReadFileWithAccess(secret, required.RequestID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "supersecretvalue123") {
		t.Fatalf("raw grant should reveal exact content, got %q", out)
	}
}

func TestReadManyFiles_SecretMarkerContainsStructuredRequest(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, ".env", "API_KEY=supersecretvalue123")
	out, err := svc.ReadManyFiles([]string{filepath.Join(root, ".env")})
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end < start {
		t.Fatalf("expected structured access-required marker, got %q", out)
	}
	var payload struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &payload); err != nil {
		t.Fatalf("access marker should be JSON: %v in %q", err, out)
	}
	if payload.Type != "access-required" || payload.RequestID == "" {
		t.Fatalf("bad access marker: %#v", payload)
	}
}
