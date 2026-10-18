package config

import (
	"errors"
	"testing"
)

func TestNew_SandboxBackend(t *testing.T) {
	root := t.TempDir()
	base := func(backend string) Config {
		return Config{Roots: []string{root}, SandboxBackend: backend}
	}

	// Empty and "none" normalize to "none" (disabled).
	for _, in := range []string{"", "none", "NONE", " none "} {
		got, err := New(base(in))
		if err != nil {
			t.Fatalf("New(sandbox=%q) unexpected error: %v", in, err)
		}
		if got.SandboxBackend != "none" {
			t.Errorf("New(sandbox=%q).SandboxBackend = %q, want none", in, got.SandboxBackend)
		}
	}

	// Known backends are accepted and lowercased.
	for _, in := range []string{"docker", "Docker", "nsjail", "gvisor"} {
		got, err := New(base(in))
		if err != nil {
			t.Fatalf("New(sandbox=%q) unexpected error: %v", in, err)
		}
		if got.SandboxBackend == "" || got.SandboxBackend == "none" {
			t.Errorf("New(sandbox=%q).SandboxBackend = %q, want a known backend", in, got.SandboxBackend)
		}
	}

	// Unknown backend is rejected (fail-closed on config typos).
	if _, err := New(base("firejail")); !errors.Is(err, ErrUnknownSandboxBackend) {
		t.Errorf("New(sandbox=firejail) = %v, want ErrUnknownSandboxBackend", err)
	}
}
