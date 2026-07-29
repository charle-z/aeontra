package edgeclient

import (
	"context"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestOpenCodeLauncherKeepsExecutionBudgetAfterStartup(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	fixture.lease.TimeoutSeconds = 1
	fixture.launcher.config.RuntimeStartupBudget = 2 * time.Second
	fixture.launcher.runProcess = func(ctx context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		if err := spec.Started(); err != nil {
			return openCodeProcessResult{ExitCode: -1, Err: err}
		}
		select {
		case <-ctx.Done():
			return openCodeProcessResult{ExitCode: -1, Err: ctx.Err()}
		case <-time.After(1200 * time.Millisecond):
			return openCodeProcessResult{ExitCode: 0}
		}
	}
	started := time.Now()
	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	elapsed := time.Since(started)
	if err != nil || result.State != OpenCodeLocalCompleted {
		t.Fatalf("result=%+v err=%v elapsed=%s", result, err, elapsed)
	}
	if elapsed < time.Second {
		t.Fatalf("startup delay was not exercised: %s", elapsed)
	}
}

func TestOpenCodeLauncherValidatesRuntimeStartupBudget(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	base := OpenCodeLauncherConfig{
		StateRoot: fixture.state, SocketRoot: fixture.launcher.config.SocketRoot,
		OpenCodePath: fixture.executable, ProviderPath: fixture.provider, BubblewrapPath: fixture.bubblewrap,
		IntegrityPath: fixture.lock, OutputLimit: 4096, Heartbeat: time.Second,
		Workspaces: fixture.registry, Journal: fixture.journal,
	}
	launcher, err := NewOpenCodeLauncher(base)
	if err != nil {
		t.Fatal(err)
	}
	if launcher.config.RuntimeStartupBudget != modelturn.RemoteRuntimeStartupTTL {
		t.Fatalf("budget=%s want=%s", launcher.config.RuntimeStartupBudget, modelturn.RemoteRuntimeStartupTTL)
	}
	for _, budget := range []time.Duration{time.Millisecond, modelturn.MaxTurnTTL + time.Second} {
		invalid := base
		invalid.RuntimeStartupBudget = budget
		if _, err := NewOpenCodeLauncher(invalid); err == nil {
			t.Fatalf("invalid runtime startup budget accepted: %s", budget)
		}
	}
}
