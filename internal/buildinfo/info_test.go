package buildinfo

import "testing"

func TestCurrentReturnsCentralBuildIdentity(t *testing.T) {
	oldCommit, oldBuiltAt := Commit, BuiltAt
	Commit = "abc123"
	BuiltAt = "2026-07-12T22:00:00Z"
	defer func() {
		Commit = oldCommit
		BuiltAt = oldBuiltAt
	}()

	got := Current()
	if got.Version != Version {
		t.Fatalf("version = %q, want %q", got.Version, Version)
	}
	if got.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", got.ProtocolVersion, ProtocolVersion)
	}
	if got.Commit != "abc123" || got.BuiltAt != "2026-07-12T22:00:00Z" {
		t.Fatalf("unexpected build identity: %#v", got)
	}
}

func TestBuildIdentityDefaultsAreSafe(t *testing.T) {
	if Version == "" || ProtocolVersion == "" {
		t.Fatal("version constants must not be empty")
	}
	if Commit == "" || BuiltAt == "" {
		t.Fatal("build stamp defaults must not be empty")
	}
}
