package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func mustPolicy(t *testing.T, mode config.Mode, root string) *Policy {
	t.Helper()
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            mode,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicy_CheckRead_DeniesSecretsAndEscapes(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)

	// Secret by path, even though it is inside the jail.
	secret := filepath.Join(root, ".env")
	if err := os.WriteFile(secret, []byte("X=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CheckRead(secret); !errors.Is(err, ErrSecretDenied) {
		t.Errorf("CheckRead(.env) = %v, want ErrSecretDenied", err)
	}
	// Outside the jail.
	if _, err := p.CheckRead(filepath.Join(root, "..", "x")); !errors.Is(err, ErrOutsideJail) {
		t.Errorf("CheckRead(escape) = %v, want ErrOutsideJail", err)
	}
	// Legit file inside the jail.
	ok := filepath.Join(root, "main.go")
	if err := os.WriteFile(ok, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CheckRead(ok); err != nil {
		t.Errorf("CheckRead(main.go) = %v, want nil", err)
	}
}

func TestPolicy_ReadOnlyBlocksWritesAndCommands(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)

	if _, _, err := p.CheckWrite(filepath.Join(root, "f.txt")); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckWrite in read-only = %v, want ErrReadOnly", err)
	}
	if _, err := p.CheckCommand("go", []string{"test"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckCommand in read-only = %v, want ErrReadOnly", err)
	}
}

func TestPolicy_AskRequiresApproval(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeAsk, root)

	resolved, needsApproval, err := p.CheckWrite(filepath.Join(root, "f.txt"))
	if err != nil {
		t.Fatalf("CheckWrite(ask) err = %v", err)
	}
	if !needsApproval || resolved == "" {
		t.Errorf("ask mode should require approval and resolve the path; got approval=%v resolved=%q", needsApproval, resolved)
	}
	needsApproval, err = p.CheckCommand("go", []string{"test"})
	if err != nil || !needsApproval {
		t.Errorf("CheckCommand(ask) = approval=%v err=%v, want approval=true nil", needsApproval, err)
	}
	// A non-allowlisted command is still denied outright, not merely gated.
	if _, err := p.CheckCommand("python", []string{"x"}); !errors.Is(err, ErrCommandNotAllowed) {
		t.Errorf("non-allowlisted under ask = %v, want ErrCommandNotAllowed", err)
	}
}

func TestPolicy_AllowExecutesWithoutApproval(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeAllow, root)
	needsApproval, err := p.CheckCommand("git", []string{"status"})
	if err != nil || needsApproval {
		t.Errorf("allow mode: approval=%v err=%v, want false nil", needsApproval, err)
	}
}
