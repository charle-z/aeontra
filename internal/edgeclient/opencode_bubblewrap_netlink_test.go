//go:build !windows

package edgeclient

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyOpenCodeSandboxReturnsRedactedNetlinkDiagnostic(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	body := "#!/bin/sh\nprintf '%s\\n' 'bwrap: loopback: Failed to create NETLINK_ROUTE socket: Address family not supported by protocol' >&2\nexit 1\n"
	if err := os.WriteFile(fixture.bubblewrap, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(fixture.state, "r", "verify-netlink")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := fixture.launcher.processSpec(
		runtimeDir,
		fixture.workspace,
		filepath.Join(runtimeDir, openCodeDriverSocketName),
		fixture.lease,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.launcher.verifyOpenCodeSandbox(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap_netlink_route_denied") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "NETLINK_ROUTE") || strings.Contains(err.Error(), "Address family") {
		t.Fatalf("diagnostic leaked raw stderr: %v", err)
	}
}
