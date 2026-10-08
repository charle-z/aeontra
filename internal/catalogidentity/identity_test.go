package catalogidentity

import (
	"strings"
	"testing"
)

func validIdentity() Identity {
	return Identity{ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("a", 64)}
}

func TestIdentityValidate(t *testing.T) {
	if err := validIdentity().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, value := range []Identity{
		{ProtocolVersion: "bad", ToolCount: 137, CatalogHash: "sha256:" + strings.Repeat("a", 64)},
		{ProtocolVersion: "2024-11-05", ToolCount: 0, CatalogHash: "sha256:" + strings.Repeat("a", 64)},
		{ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: "*"},
	} {
		if err := value.Validate(); err == nil {
			t.Fatalf("invalid accepted: %+v", value)
		}
	}
}

func TestDecodeManifestStrictAndExact(t *testing.T) {
	identity := validIdentity()
	data := `{"schema_version":1,"protocol_version":"2024-11-05","tool_count":137,"catalog_hash":"` + identity.CatalogHash + `"}`
	manifest, err := DecodeManifest([]byte(data))
	if err != nil || manifest.Identity != identity {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	if err := manifest.Matches(identity); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(data, `"schema_version":1`, `"schema_version":2`, 1),
		strings.TrimSuffix(data, "}") + `,"extra":true}`,
		data + `{}`,
		strings.Replace(data, identity.CatalogHash, "*", 1),
	} {
		if _, err := DecodeManifest([]byte(bad)); err == nil {
			t.Fatalf("bad accepted: %s", bad)
		}
	}
	other := identity
	other.ToolCount++
	if err := manifest.Matches(other); err == nil {
		t.Fatal("mismatch accepted")
	}
}
