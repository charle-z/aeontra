package tools

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func TestNewServiceBuildsCapabilityServicesOverOneSharedCore(t *testing.T) {
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
	logger, err := audit.Open(root + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	svc := NewService(pol, logger, root)
	if svc.serviceCore == nil {
		t.Fatal("shared service core is nil")
	}
	if svc.RepositoryCapability == nil || svc.GitCapability == nil || svc.SourceCapability == nil || svc.PlatformCapability == nil || svc.ExecutionCapability == nil || svc.BrainCapability == nil {
		t.Fatal("one or more capability services are nil")
	}

	cores := []*serviceCore{
		svc.RepositoryCapability.serviceCore,
		svc.GitCapability.serviceCore,
		svc.SourceCapability.serviceCore,
		svc.PlatformCapability.serviceCore,
		svc.ExecutionCapability.serviceCore,
		svc.BrainCapability.serviceCore,
	}
	for i, core := range cores {
		if core != svc.serviceCore {
			t.Fatalf("capability %d does not share the central core", i)
		}
	}
	if svc.GitCapability.SourceCapability != svc.SourceCapability {
		t.Fatal("Git capability does not share the source capability")
	}
	if svc.PlatformCapability.SourceCapability != svc.SourceCapability {
		t.Fatal("platform capability does not share the source capability")
	}
	if svc.plans == nil || svc.RepositoryCapability.plans != svc.plans || svc.ExecutionCapability.plans != svc.plans {
		t.Fatal("action plans are not central and shared")
	}
}

func TestCapabilityConfigurationUpdatesOnlyOwningCapability(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	coolify := NewCoolifyClient("https://coolify.example", "token", nil)
	github := NewGitHubClient("https://api.github.test", "token", "owner", "user", "private")
	sandbox := disabledSandboxRunner{}
	validation := disabledValidationRunner{}

	svc.WithCoolify(coolify).
		WithGitHub(github).
		WithSandboxRunner(sandbox).
		WithValidationRunner(validation).
		WithTestCommand([]string{"go", "test", "./..."})

	if svc.PlatformCapability.coolify != coolify {
		t.Fatal("Coolify client not owned by platform capability")
	}
	if svc.SourceCapability.github != github {
		t.Fatal("GitHub client not owned by source capability")
	}
	if svc.ExecutionCapability.sandbox == nil || svc.ExecutionCapability.validation == nil {
		t.Fatal("execution dependencies not configured")
	}
	if len(svc.ExecutionCapability.testCmd) != 3 {
		t.Fatalf("test command = %#v", svc.ExecutionCapability.testCmd)
	}
}
