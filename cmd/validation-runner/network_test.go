package main

import (
	"strings"
	"testing"
)

func TestLockfileProfileEnablesCorepackNetworkOnlyForBootstrap(t *testing.T) {
	c, entry := testValidationConfig(t)
	args, err := c.argv(entry, "pnpm-lockfile")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "COREPACK_ENABLE_NETWORK=1") {
		t.Fatalf("lockfile profile cannot bootstrap Corepack: %s", joined)
	}

	args, err = c.argv(entry, "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, "COREPACK_ENABLE_NETWORK=0") {
		t.Fatalf("validation profile must remain offline: %s", joined)
	}
}
