//go:build !windows

package buildspike

import "testing"

func TestValidateProcessEvidenceRequiresDedicatedUIDAndSingleCgroup(t *testing.T) {
	evidence := []ProcessEvidence{
		{PID: 100, PPID: 1, UID: 1001, Cgroup: "/mcp-devbox-builder.service", Command: "rootlesskit"},
		{PID: 101, PPID: 100, UID: 1001, Cgroup: "/mcp-devbox-builder.service", Command: "buildkitd"},
		{PID: 102, PPID: 101, UID: 1001, Cgroup: "/mcp-devbox-builder.service", Command: "buildkit-runc"},
		{PID: 103, PPID: 102, UID: 1001, Cgroup: "/mcp-devbox-builder.service", Command: "compile"},
	}
	if err := ValidateProcessEvidence(1001, "/mcp-devbox-builder.service", evidence); err != nil {
		t.Fatal(err)
	}
	cases := [][]ProcessEvidence{
		append([]ProcessEvidence(nil), evidence[:1]...),
		func() []ProcessEvidence {
			value := append([]ProcessEvidence(nil), evidence...)
			value[2].UID = 0
			return value
		}(),
		func() []ProcessEvidence {
			value := append([]ProcessEvidence(nil), evidence...)
			value[3].Cgroup = "/system.slice/docker.service"
			return value
		}(),
		func() []ProcessEvidence {
			value := append([]ProcessEvidence(nil), evidence...)
			value[2].PPID = 999
			return value
		}(),
	}
	for index, candidate := range cases {
		if err := ValidateProcessEvidence(1001, "/mcp-devbox-builder.service", candidate); err == nil {
			t.Fatalf("invalid evidence %d accepted: %+v", index, candidate)
		}
	}
}

func TestParseUnifiedCgroupRejectsLegacyAndAmbiguousMembership(t *testing.T) {
	path, err := ParseUnifiedCgroup("0::/mcp-devbox-builder.service\n")
	if err != nil || path != "/mcp-devbox-builder.service" {
		t.Fatalf("path=%q err=%v", path, err)
	}
	for _, raw := range []string{
		"2:cpu:/legacy\n",
		"0::/one\n0::/two\n",
		"0::/../../escape\n",
		"",
	} {
		if _, err := ParseUnifiedCgroup(raw); err == nil {
			t.Fatalf("invalid cgroup accepted: %q", raw)
		}
	}
}
