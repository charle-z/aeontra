package config

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSecureDefaults_IsReadOnly(t *testing.T) {
	d := SecureDefaults()
	if d.Mode != ModeReadOnly {
		t.Errorf("SecureDefaults().Mode = %q, want %q", d.Mode, ModeReadOnly)
	}
	if len(d.AllowedCommands) == 0 {
		t.Error("SecureDefaults should provide a conservative command allowlist")
	}
}

func TestNew_RequiresRoots(t *testing.T) {
	if _, err := New(SecureDefaults()); !errors.Is(err, ErrNoRoots) {
		t.Errorf("New without roots = %v, want ErrNoRoots", err)
	}
}

func TestNew_RejectsRelativeRoot(t *testing.T) {
	c := SecureDefaults()
	c.Roots = []string{"relative/path"}
	if _, err := New(c); !errors.Is(err, ErrRootNotAbsolute) {
		t.Errorf("New with relative root = %v, want ErrRootNotAbsolute", err)
	}
}

func TestNew_CleansRootsAndDefaultsMode(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "proj", "..", "proj")
	c := Config{Roots: []string{abs}}
	got, err := New(c)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if got.Mode != ModeReadOnly {
		t.Errorf("Mode defaulted to %q, want read-only", got.Mode)
	}
	if got.Roots[0] != filepath.Clean(abs) {
		t.Errorf("root not cleaned: %q", got.Roots[0])
	}
}

func TestNew_RejectsUnknownMode(t *testing.T) {
	c := Config{Roots: []string{t.TempDir()}, Mode: Mode("alow")}
	if _, err := New(c); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("New(mode=alow) = %v, want ErrUnknownMode", err)
	}
}

func TestNormalizeModeAcceptsOnlyExhaustiveValues(t *testing.T) {
	tests := []struct {
		input Mode
		want  Mode
	}{
		{input: "", want: ModeReadOnly},
		{input: ModeReadOnly, want: ModeReadOnly},
		{input: ModeAsk, want: ModeAsk},
		{input: ModeAllow, want: ModeAllow},
	}
	for _, test := range tests {
		got, err := NormalizeMode(test.input)
		if err != nil || got != test.want {
			t.Errorf("NormalizeMode(%q) = %q, %v; want %q, nil", test.input, got, err, test.want)
		}
	}
}
