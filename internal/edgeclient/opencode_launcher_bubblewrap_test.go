//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOpenCodeLauncherBubblewrapFailureIsFailClosed(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	executions := 0
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		executions++
		if spec.Executable != fixture.bubblewrap {
			t.Fatalf("launcher bypassed Bubblewrap executable=%q", spec.Executable)
		}
		_, _ = spec.Stderr.Write([]byte("bwrap: No permissions to create new namespace\n"))
		return openCodeProcessResult{ExitCode: 1, Err: errors.New("exit status 1")}
	}
	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap_user_namespace_denied") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if executions != 1 || result.State != OpenCodeLocalFailed {
		t.Fatalf("executions=%d result=%+v", executions, result)
	}
}
