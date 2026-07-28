//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestBundleOperationReceiptIsDurableExclusiveAndValidated(t *testing.T) {
	stateRoot := t.TempDir()
	receipt := bundleOperationReceipt{OperationID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationBundleRollback}
	if err := writeBundleReceipt(stateRoot, receipt); err != nil {
		t.Fatal(err)
	}
	read, err := readBundleReceipt(stateRoot)
	if err != nil || read != receipt {
		t.Fatalf("read receipt = %+v, %v", read, err)
	}
	other := bundleOperationReceipt{OperationID: "eo_abcdef0123456789abcdef0123456789", Kind: edge.OperationBundleUpdate}
	if err := writeBundleReceipt(stateRoot, other); err == nil {
		t.Fatal("expected an existing receipt to prevent a second updater operation")
	}
	read, err = readBundleReceipt(stateRoot)
	if err != nil || read != receipt {
		t.Fatalf("existing receipt changed = %+v, %v", read, err)
	}
	clearBundleReceipt(stateRoot, other.OperationID)
	if _, err := readBundleReceipt(stateRoot); err != nil {
		t.Fatalf("unrelated completion cleared receipt: %v", err)
	}
	clearBundleReceipt(stateRoot, receipt.OperationID)
	if _, err := readBundleReceipt(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("receipt was not cleared: %v", err)
	}
}

func TestBundleOperationReceiptFailsClosedOnUnsafeState(t *testing.T) {
	stateRoot := t.TempDir()
	path := filepath.Join(stateRoot, bundleReceiptFile)
	if err := os.WriteFile(path, []byte("{\"operation_id\":\"bad\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundleReceipt(stateRoot); err == nil {
		t.Fatal("expected malformed receipt rejection")
	}
	if err := writeBundleReceipt(stateRoot, bundleOperationReceipt{OperationID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationEdgeRepair}); err == nil {
		t.Fatal("expected malformed existing receipt to block overwrite")
	}
}

func TestInstalledModelProviderAcceptsOnlyClosedLoopbackConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.json")
	valid := []byte("{\"version\":1,\"provider\":\"opencode-local\",\"endpoint\":\"http://127.0.0.1:4096/v1/next-action\"}\n")
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if !installedModelProviderValid(path) {
		t.Fatal("expected closed loopback model configuration to be valid")
	}
	for _, invalid := range []string{
		`{"version":1,"provider":"opencode-local","endpoint":"https://127.0.0.1:4096/v1/next-action"}`,
		`{"version":1,"provider":"opencode-local","endpoint":"http://example.com:4096/v1/next-action"}`,
		`{"version":1,"provider":"opencode-local","endpoint":"http://127.0.0.1:4096/other"}`,
		`{"version":1,"provider":"remote","endpoint":"http://127.0.0.1:4096/v1/next-action"}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if installedModelProviderValid(path) {
			t.Fatalf("accepted unsafe model config: %s", invalid)
		}
	}
}

func TestActiveSystemdInvocationValidatesManagedProcessIdentity(t *testing.T) {
	const invocationID = "0123456789abcdef0123456789abcdef"
	if !activeSystemdInvocation(invocationID, "4242", 4242) {
		t.Fatal("expected matching systemd invocation metadata to prove the managed process is active")
	}
	if !activeSystemdInvocation(invocationID, "", 4242) {
		t.Fatal("expected INVOCATION_ID-only compatibility for older systemd releases")
	}
	for _, test := range []struct {
		name         string
		invocationID string
		execPID      string
	}{
		{name: "missing invocation", invocationID: "", execPID: "4242"},
		{name: "invalid invocation", invocationID: "not-an-invocation", execPID: "4242"},
		{name: "different process", invocationID: invocationID, execPID: "4243"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if activeSystemdInvocation(test.invocationID, test.execPID, 4242) {
				t.Fatal("accepted invalid systemd invocation metadata")
			}
		})
	}
}
