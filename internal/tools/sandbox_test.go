package tools

import (
	"strings"
	"testing"

	"github.com/carbe/mcp-devbox/internal/config"
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
