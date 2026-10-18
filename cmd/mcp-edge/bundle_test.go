package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBundleCommandHasClosedOperationAndFailsUnstampedBuild(t *testing.T) {
	if err := bundleCommand([]string{"status"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "only the verify") {
		t.Fatalf("unexpected closed operation result: %v", err)
	}
	if err := bundleCommand([]string{"verify"}, &bytes.Buffer{}); err == nil || err.Error() != "manifest_invalid" {
		t.Fatalf("unstamped build must fail closed, got %v", err)
	}
}
