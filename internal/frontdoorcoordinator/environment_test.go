package frontdoorcoordinator

import (
	"strings"
	"testing"
)

func TestManagedEnvironmentCommentAuthenticatesPublicAndSealedValues(t *testing.T) {
	const token = "operator-token"

	public := ManagedEnvironmentComment(token, "MCP_FRONT_DOOR_BACKEND_URL", BackendOrigin)
	if !strings.Contains(public, "public:") || strings.Contains(public, token) {
		t.Fatalf("unsafe public comment: %q", public)
	}
	value, err := ManagedEnvironmentValue(public, token, "MCP_FRONT_DOOR_BACKEND_URL", FrontPublicOrigin, BackendOrigin)
	if err != nil || value != BackendOrigin {
		t.Fatalf("public value=%q err=%v", value, err)
	}
	if _, err := ManagedEnvironmentValue(public, "wrong-token", "MCP_FRONT_DOOR_BACKEND_URL", BackendOrigin); err == nil {
		t.Fatal("public comment accepted with the wrong token")
	}
	if _, err := ManagedEnvironmentValue(public, token, "MCP_FRONT_DOOR_BACKEND_URL", FrontPublicOrigin); err == nil {
		t.Fatal("public value outside the caller contract was accepted")
	}

	sealed := ManagedEnvironmentComment(token, "COOLIFY_API_TOKEN", token)
	if !strings.Contains(sealed, "sealed::") || strings.Contains(sealed, token) {
		t.Fatalf("secret leaked in sealed comment: %q", sealed)
	}
	value, err = ManagedEnvironmentValue(sealed, token, "COOLIFY_API_TOKEN", token)
	if err != nil || value != token {
		t.Fatalf("sealed value=%q err=%v", value, err)
	}
	if _, err := ManagedEnvironmentValue(sealed, token, "COOLIFY_API_TOKEN"); err == nil {
		t.Fatal("sealed value resolved without a bounded candidate")
	}
}

func TestManagedEnvironmentCommentRejectsTamperingAndMalformedMetadata(t *testing.T) {
	comment := ManagedEnvironmentComment("token", "MCP_FRONT_DOOR_COORDINATOR_TARGET", "cutover")
	tampered := comment[:len(comment)-1] + "0"
	for _, bad := range []string{
		"",
		"unmanaged",
		comment + "00",
		tampered,
	} {
		if _, err := ManagedEnvironmentValue(bad, "token", "MCP_FRONT_DOOR_COORDINATOR_TARGET", "idle", "cutover", "rollback"); err == nil {
			t.Fatalf("tampered comment accepted: %q", bad)
		}
	}
}
