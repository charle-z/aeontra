package main

import (
	"strings"
	"testing"
	"time"
)

func testValidationConfig(t *testing.T) (config, repositoryEntry) {
	t.Helper()
	registry, _ := newRegistryFixture(t, "demo")
	entry, err := registry.lookup("demo")
	if err != nil {
		t.Fatal(err)
	}
	return config{registry: registry, image: "node:22-alpine", store: "store", user: "10001:10001", timeout: time.Minute}, entry
}

func TestValidationProfileIsClosedAndHardened(t *testing.T) {
	c, entry := testValidationConfig(t)
	args, err := c.argv(entry, "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges", "--user 10001:10001", "corepack install -g --cache-only /pnpm-store/corepack-pnpm-10.13.1.tgz", "corepack pnpm install --offline --frozen-lockfile --ignore-scripts", "COREPACK_ENABLE_NETWORK=0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in validation argv", want)
		}
	}
	if !strings.Contains(joined, "src="+entry.hostPath+",dst=/workspace") {
		t.Fatal("runner did not use the server-owned registry mount")
	}
	if _, err := c.argv(entry, "anything-from-agent"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestLockfileProfileHasOnlyFixedRegistryNetwork(t *testing.T) {
	c, entry := testValidationConfig(t)
	args, err := c.argv(entry, "pnpm-lockfile")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "corepack enable") || !strings.Contains(joined, "--network bridge") || !strings.Contains(joined, "corepack pack pnpm@10.13.1 -o /pnpm-store/corepack-pnpm-10.13.1.tgz") || !strings.Contains(joined, "corepack pnpm install --lockfile-only --ignore-scripts --registry=https://registry.npmjs.org") {
		t.Fatal("unexpected lockfile argv")
	}
}

func TestValidationProfilesSharePersistentCorepackCache(t *testing.T) {
	c, entry := testValidationConfig(t)
	for _, profile := range []string{"pnpm-lockfile", "pnpm-validate"} {
		args, err := c.argv(entry, profile)
		if err != nil {
			t.Fatal(err)
		}
		if joined := strings.Join(args, " "); !strings.Contains(joined, "COREPACK_HOME=/pnpm-store/corepack") {
			t.Fatalf("profile %s does not reuse the persistent Corepack cache", profile)
		}
	}
}

func TestBearerComparison(t *testing.T) {
	if !constantBearer("Bearer 01234567890123456789012345678901", "01234567890123456789012345678901") {
		t.Fatal("valid bearer rejected")
	}
	if constantBearer("Bearer nope", "01234567890123456789012345678901") {
		t.Fatal("invalid bearer accepted")
	}
}
