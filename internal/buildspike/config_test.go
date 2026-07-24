//go:build !windows

package buildspike

import "testing"

func TestConfigAcceptsOnlyDedicatedNonRootBuilderAndMeasuredQuotas(t *testing.T) {
	valid := Config{
		BuilderUser:      "mcp-build",
		BuilderUID:       1001,
		RuntimeRoot:      "/run/mcp-devbox-buildkit",
		StateRoot:        "/var/lib/mcp-devbox-buildkit",
		CacheRoot:        "/var/cache/mcp-devbox-buildkit",
		BuildkitdPath:    "/usr/local/lib/mcp-devbox-builder/buildkitd",
		BuildctlPath:     "/usr/local/lib/mcp-devbox-builder/buildctl",
		RootlesskitPath:  "/usr/bin/rootlesskit",
		CPUQuotaPercent:  65,
		MemoryHighMiB:    1280,
		MemoryMaxMiB:     1792,
		PIDsMax:          512,
		IOWeight:         25,
		MaxOutputBytes:   1 << 20,
		MaxArtifactBytes: 2 << 30,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, quota := range []int{50, 65, 80} {
		candidate := valid
		candidate.CPUQuotaPercent = quota
		if err := candidate.Validate(); err != nil {
			t.Fatalf("quota %d rejected: %v", quota, err)
		}
	}
	invalid := []Config{
		func() Config { value := valid; value.BuilderUID = 0; return value }(),
		func() Config { value := valid; value.BuilderUser = "root"; return value }(),
		func() Config { value := valid; value.CPUQuotaPercent = 100; return value }(),
		func() Config { value := valid; value.MemoryHighMiB = value.MemoryMaxMiB + 1; return value }(),
		func() Config { value := valid; value.RuntimeRoot = "/run/buildkit"; return value }(),
		func() Config { value := valid; value.BuildkitdPath = "/usr/bin/buildkitd"; return value }(),
	}
	for index, candidate := range invalid {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid config %d accepted: %+v", index, candidate)
		}
	}
}

func TestSystemdPropertiesEnforceWholeServiceBoundary(t *testing.T) {
	config := DefaultConfig(1001)
	properties, err := config.SystemdProperties()
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"CPUQuota":        "65%",
		"MemoryHigh":      "1280M",
		"MemoryMax":       "1792M",
		"TasksMax":        "512",
		"IOWeight":        "25",
		"KillMode":        "control-group",
		"Delegate":        "yes",
		"User":            "mcp-build",
		"NoNewPrivileges": "no",
	}
	for key, expected := range required {
		if properties[key] != expected {
			t.Fatalf("%s=%q want %q", key, properties[key], expected)
		}
	}
	for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "privileged", "sudo", "sh -c"} {
		for key, value := range properties {
			if key == forbidden || value == forbidden {
				t.Fatalf("forbidden value in properties: %s=%s", key, value)
			}
		}
	}
}
