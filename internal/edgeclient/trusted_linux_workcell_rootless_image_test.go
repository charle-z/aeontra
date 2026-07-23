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
