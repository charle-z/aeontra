package tools

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSharedWorkdirIsAvailableToCapabilities(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)

	for name, resolve := range map[string]func(string) (string, error){
		"repository": svc.RepositoryCapability.workdir,
		"git":        svc.GitCapability.workdir,
		"source":     svc.SourceCapability.workdir,
		"platform":   svc.PlatformCapability.workdir,
		"execution":  svc.ExecutionCapability.workdir,
	} {
		got, err := resolve("")
		if err != nil {
			t.Fatalf("%s workdir: %v", name, err)
		}
		if got != root {
			t.Fatalf("%s workdir = %q, want %q", name, got, root)
		}
	}
}
