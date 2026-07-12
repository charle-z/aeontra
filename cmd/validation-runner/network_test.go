package main

import (
	"strings"
	"testing"
)

func TestLockfileProfileEnablesCorepackNetworkOnlyForBootstrap(t *testing.T) {
	c := config{root: "/repos", hostRoot: "/host/repos", image: "node:22-alpine", store: "store", user: "10001:10001"}
	args, err := c.argv("/repos/demo", "pnpm-lockfile")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "COREPACK_ENABLE_NETWORK=1") {
		t.Fatalf("lockfile profile cannot bootstrap Corepack: %s", joined)
	}

	args, err = c.argv("/repos/demo", "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, "COREPACK_ENABLE_NETWORK=0") {
		t.Fatalf("validation profile must remain offline: %s", joined)
	}
}
