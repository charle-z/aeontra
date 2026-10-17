package catalogidentity

import (
	"strings"
	"testing"
)

func validIdentity() Identity {
	return Identity{
		ProtocolVersion: "2024-11-05",
		ToolCount:       137,
		CatalogHash:     "sha256:" + strings.Repeat("a", 64),
	}
}

func TestIdentityValidate(t *testing.T) {
	if err := validIdentity().Validate(); err != nil {
		t.Fatalf("valid identity: %v", err)
	}
	tests := []Identity{
		{ProtocolVersion: "2024-11-5", ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("a", 64)},
		{ProtocolVersion: "2024-11-05", ToolCount: 0, CatalogHash: "sha256:" + strings.Repeat("a", 64)},
		{ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: "*"},
		{ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("A", 64)},
	}
	for _, identity := range tests {
		if err := identity.Validate(); err == nil {
			t.Fatalf("invalid identity accepted: %+v", identity)
		}
	}
}

func TestDecodeManifestIsStrict(t *testing.T) {
	identity := validIdentity()
	data := `{"schema_version":1,"protocol_version":"` + identity.ProtocolVersion + `","tool_count":137,"catalog_hash":"` + identity.CatalogHash + `"}`
	decoded, err := DecodeManifest([]byte(data))
	if err != nil || decoded.Identity != identity {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	for _, malformed := range []string{
		strings.Replace(data, `"schema_version":1`, `"schema_version":2`, 1),
		strings.TrimSuffix(data, "}") + `,"extra":true}`,
		data + `{}`,
		strings.Replace(data, identity.CatalogHash, "*", 1),
	} {
		if _, err := DecodeManifest([]byte(malformed)); err == nil {
			t.Fatalf("malformed manifest accepted: %s", malformed)
		}
	}
}

func TestManifestMatchesExactIdentity(t *testing.T) {
	manifest := Manifest{SchemaVersion: 1, Identity: validIdentity()}
	if err := manifest.Matches(validIdentity()); err != nil {
		t.Fatal(err)
	}
	other := validIdentity()
	other.ToolCount++
	if err := manifest.Matches(other); err == nil {
		t.Fatal("mismatched identity accepted")
	}
}
