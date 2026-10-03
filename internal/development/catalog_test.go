package development

import (
	"strings"
	"testing"
)

func TestEnvironmentCatalogCanonicalizesByAuthorityAndIdentity(t *testing.T) {
	l3 := mustEnvironment(t, "l3", ClassL3Sandbox, 2, "toolchain.go")
	workcell := mustEnvironment(t, "workcell", ClassWorkcell, 3, "toolchain.go", "network.host-shared")
	runner := mustEnvironment(t, "runner", ClassIsolatedRunner, 1, "namespace.user.nested")
	catalog, err := NewEnvironmentCatalog(runner, workcell, l3)
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.Environments()
	if len(got) != 3 || got[0].EnvironmentID != "l3" || got[1].EnvironmentID != "workcell" || got[2].EnvironmentID != "runner" {
		t.Fatalf("catalog order=%+v", got)
	}
	reordered, err := NewEnvironmentCatalog(l3, runner, workcell)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Digest() == "" || catalog.Digest() != reordered.Digest() {
		t.Fatalf("catalog digest depends on input order: %s %s", catalog.Digest(), reordered.Digest())
	}
}

func TestEnvironmentCatalogRejectsAmbiguousIdentity(t *testing.T) {
	first := mustEnvironment(t, "workcell", ClassWorkcell, 1, "toolchain.go")
	second := mustEnvironment(t, "workcell", ClassWorkcell, 2, "toolchain.go", "build.make")
	if _, err := NewEnvironmentCatalog(first, second); err == nil {
		t.Fatal("catalog accepted two attestations for one environment identity")
	}
}

func TestEnvironmentCatalogRejectsSameIdentityAcrossNonAdjacentClasses(t *testing.T) {
	first := mustEnvironment(t, "shared", ClassL3Sandbox, 1, "toolchain.go")
	middle := mustEnvironment(t, "other", ClassWorkcell, 1, "toolchain.go")
	last := mustEnvironment(t, "shared", ClassIsolatedRunner, 1, "toolchain.go")
	if _, err := NewEnvironmentCatalog(first, middle, last); err == nil {
		t.Fatal("non-adjacent environment identity collision accepted")
	}
}

func TestEnvironmentCatalogRejectsInvalidAndOversizedInput(t *testing.T) {
	if _, err := NewEnvironmentCatalog(); err == nil {
		t.Fatal("empty catalog accepted")
	}
	invalid := EnvironmentAttestation{EnvironmentID: "broken"}
	if _, err := NewEnvironmentCatalog(invalid); err == nil {
		t.Fatal("invalid attestation accepted")
	}
	entries := make([]EnvironmentAttestation, 0, MaxEnvironmentCatalogEntries+1)
	for index := 0; index <= MaxEnvironmentCatalogEntries; index++ {
		id := "env-" + strings.Repeat("x", 2) + string(rune('a'+index%26))
		// Environment IDs must stay unique even as the catalog crosses its bound.
		id = id + "-" + strings.Repeat("0", index/26) + string(rune('a'+index%26))
		environment := mustEnvironment(t, id, ClassWorkcell, 1, "toolchain.go")
		entries = append(entries, environment)
	}
	if _, err := NewEnvironmentCatalog(entries...); err == nil {
		t.Fatal("oversized environment catalog accepted")
	}
}

func TestEnvironmentCatalogFindDigestRejectsDrift(t *testing.T) {
	before := mustEnvironment(t, "workcell", ClassWorkcell, 1, "toolchain.go")
	after := mustEnvironment(t, "workcell", ClassWorkcell, 2, "toolchain.go", "build.make")
	catalog, err := NewEnvironmentCatalog(after)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := catalog.FindDigest(before.Digest); found {
		t.Fatal("stale environment digest survived catalog generation change")
	}
	if got, found := catalog.FindDigest(after.Digest); !found || got.Digest != after.Digest {
		t.Fatalf("current environment missing: found=%t got=%+v", found, got)
	}
}
