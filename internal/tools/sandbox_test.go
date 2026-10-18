package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSandboxStatus_DefaultUnavailable(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	out := svc.SandboxStatus()
	for _, want := range []string{
		"available: false",
		"backend: none",
		"default_egress: deny",
		"free_terminal: false",
		"no Docker socket",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sandbox status missing %q:\n%s", want, out)
		}
	}
}

func TestNewSandboxRunner_DisabledByDefault(t *testing.T) {
	for _, in := range []string{"", "none", "NONE"} {
		r := NewSandboxRunner(in)
		st := r.Status(context.Background())
		if st.Available || st.Backend != "none" || st.FreeTerminal {
			t.Errorf("NewSandboxRunner(%q) status = %+v, want disabled/none/no-terminal", in, st)
		}
		if _, err := r.Run(context.Background(), SandboxRunRequest{Argv: []string{"echo", "hi"}}); err == nil {
			t.Errorf("NewSandboxRunner(%q).Run should error (no backend)", in)
		}
	}
}

func TestNewSandboxRunner_KnownBackendIsPendingNotAvailable(t *testing.T) {
	r := NewSandboxRunner("docker")
	st := r.Status(context.Background())
	// Plumbed + visible, but NOT available and never a free terminal.
	if st.Backend != "docker" {
		t.Errorf("backend = %q, want docker", st.Backend)
	}
	if st.Available {
		t.Error("a configured-but-unimplemented backend must NOT report available")
	}
	if st.FreeTerminal {
		t.Error("no free terminal before L3")
	}
	if st.DefaultEgress != "deny" {
		t.Errorf("default egress = %q, want deny", st.DefaultEgress)
	}
	// Run must refuse — no execution capability is granted.
	if _, err := r.Run(context.Background(), SandboxRunRequest{Argv: []string{"echo", "hi"}}); err == nil {
		t.Error("pending backend Run must error (not yet implemented)")
	}
	out := strings.ToLower(formatSandboxStatus(st))
	if !strings.Contains(out, "not yet implemented") || !strings.Contains(out, "no free terminal") {
		t.Errorf("status text should state pending + no free terminal: %s", out)
	}
}
