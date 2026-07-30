package main

import (
	"testing"
	"time"
)

func TestLoadConfigRequiresPinnedBackendCompatibility(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		backendURLEnv:       "https://backend.example",
		expectedProtocolEnv: "2024-11-05",
		expectedCatalogEnv:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		listenAddrEnv:       "0.0.0.0:9000",
		probeIntervalEnv:    "2s",
		probeTimeoutEnv:     "4s",
	}
	config, addr, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.BackendURL != values[backendURLEnv] || config.ExpectedProtocol != values[expectedProtocolEnv] ||
		config.ExpectedCatalogHash != values[expectedCatalogEnv] || addr != values[listenAddrEnv] {
		t.Fatalf("config=%+v addr=%q", config, addr)
	}
	if config.ProbeInterval != 2*time.Second || config.ProbeTimeout != 4*time.Second {
		t.Fatalf("probe durations=%s/%s", config.ProbeInterval, config.ProbeTimeout)
	}

	delete(values, expectedCatalogEnv)
	if _, _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("missing catalog pin accepted")
	}
}

func TestLoadConfigUsesSafeDefaults(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		backendURLEnv:       "https://backend.example",
		expectedProtocolEnv: "2024-11-05",
		expectedCatalogEnv:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	config, addr, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if addr != "0.0.0.0:8765" || config.ProbeInterval != time.Second || config.ProbeTimeout != 3*time.Second {
		t.Fatalf("config=%+v addr=%q", config, addr)
	}
}

func TestResolveFrontDoorCommitUsesCoolifyFallbackOnlyForUnstampedBuild(t *testing.T) {
	t.Parallel()
	const sourceCommit = "0123456789abcdef0123456789abcdef01234567"
	values := map[string]string{
		"MCP_DEVBOX_COMMIT": "",
		"SOURCE_COMMIT":     sourceCommit,
	}
	getenv := func(key string) string { return values[key] }
	if got := resolveFrontDoorCommit("unknown", getenv); got != sourceCommit {
		t.Fatalf("unstamped commit=%q want %q", got, sourceCommit)
	}
	const linked = "fedcba9876543210fedcba9876543210fedcba98"
	if got := resolveFrontDoorCommit(linked, getenv); got != linked {
		t.Fatalf("linked commit=%q want %q", got, linked)
	}
}
