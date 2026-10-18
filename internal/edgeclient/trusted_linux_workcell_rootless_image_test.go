//go:build p12_e2e && !windows

package edgeclient

import (
	"strings"
	"testing"
)

func TestP12PostgresImageAcceptsOnlyDigestDerivedLocalTag(t *testing.T) {
	valid := "localhost/p12-postgres-fixture:" + strings.Repeat("a", 64)
	if !validP12PostgresImageReference(valid) {
		t.Fatalf("valid fixture reference rejected: %q", valid)
	}
}

func TestP12PostgresImageRejectsMutableOrRemoteReferences(t *testing.T) {
	for name, value := range map[string]string{
		"remote":         "docker.io/library/postgres:17-alpine",
		"mutable local":  "localhost/p12-postgres-fixture:latest",
		"raw digest":     "sha256:" + strings.Repeat("a", 64),
		"uppercase":      "localhost/p12-postgres-fixture:" + strings.Repeat("A", 64),
		"path traversal": "localhost/p12-postgres-fixture:../fixture",
	} {
		t.Run(name, func(t *testing.T) {
			if validP12PostgresImageReference(value) {
				t.Fatalf("invalid fixture reference accepted: %q", value)
			}
		})
	}
}

func TestP12RootlessResourceSnapshotRequiresExactConvergence(t *testing.T) {
	baseline := map[string][]string{
		"container": {"container-a"},
		"pod":       {"pod-a"},
		"network":   {"network-a"},
		"volume":    {"volume-a"},
	}
	matching := map[string][]string{
		"container": {"container-a"},
		"pod":       {"pod-a"},
		"network":   {"network-a"},
		"volume":    {"volume-a"},
	}
	if !p12SameRootlessResourceSnapshot(baseline, matching) {
		t.Fatal("matching snapshot was rejected")
	}
	for resource, extra := range map[string]string{
		"container": "container-b",
		"pod":       "pod-b",
		"network":   "network-b",
		"volume":    "volume-b",
	} {
		changed := map[string][]string{
			"container": append([]string(nil), matching["container"]...),
			"pod":       append([]string(nil), matching["pod"]...),
			"network":   append([]string(nil), matching["network"]...),
			"volume":    append([]string(nil), matching["volume"]...),
		}
		changed[resource] = append(changed[resource], extra)
		if p12SameRootlessResourceSnapshot(baseline, changed) {
			t.Fatalf("snapshot accepted residual %s resource", resource)
		}
	}
}
