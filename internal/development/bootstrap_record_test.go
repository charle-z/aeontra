package development

import (
	"strings"
	"testing"
)

func TestBootstrapResolutionValidatesCanonicalSavedSelection(t *testing.T) {
	goResolution := BootstrapResolution{
		CapabilityID: "toolchain.go.v1-26", Toolchain: "go", Version: "1.26.10", Platform: "amd64",
		ArtifactFile: "go1.26.10.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("a", 64), ArtifactSize: 71_000_000,
	}
	if err := ValidateBootstrapResolution(goResolution); err != nil {
		t.Fatal(err)
	}
	digest, err := BootstrapResolutionDigest(goResolution)
	if err != nil || len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("canonical Go resolution did not produce a digest: %q, %v", digest, err)
	}
	if repeat, err := BootstrapResolutionDigest(goResolution); err != nil || repeat != digest {
		t.Fatalf("same saved resolution changed digest: %q, %v", repeat, err)
	}

	rustResolution := BootstrapResolution{
		CapabilityID: "toolchain.rust.v1-95-0", Toolchain: "rust", Version: "1.95.0", Platform: "x86_64-unknown-linux-gnu",
		InstallerSHA256: strings.Repeat("b", 64), ManifestSHA256: strings.Repeat("c", 64),
	}
	if err := ValidateBootstrapResolution(rustResolution); err != nil {
		t.Fatalf("canonical Rust release lost its exact .0 patch: %v", err)
	}
	if _, err := BootstrapResolutionDigest(rustResolution); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapResolutionRejectsCallerControlledOrNoncanonicalFields(t *testing.T) {
	base := BootstrapResolution{
		CapabilityID: "toolchain.go.v1-26-10", Toolchain: "go", Version: "1.26.10", Platform: "amd64",
		ArtifactFile: "go1.26.10.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("a", 64), ArtifactSize: 71_000_000,
	}
	for _, test := range []struct {
		name string
		edit func(*BootstrapResolution)
	}{
		{name: "unknown capability", edit: func(value *BootstrapResolution) { value.CapabilityID = "toolchain.python.v3-13-1" }},
		{name: "leading zero capability", edit: func(value *BootstrapResolution) { value.CapabilityID = "toolchain.go.v1-026-10" }},
		{name: "version does not match pin", edit: func(value *BootstrapResolution) { value.Version = "1.26.11" }},
		{name: "caller artifact url", edit: func(value *BootstrapResolution) { value.ArtifactFile = "https://example.invalid/go.tar.gz" }},
		{name: "digest not canonical lowercase sha256", edit: func(value *BootstrapResolution) { value.ArtifactSHA256 = strings.Repeat("A", 64) }},
		{name: "artifact too large", edit: func(value *BootstrapResolution) { value.ArtifactSize = bootstrapArtifactSizeLimit + 1 }},
		{name: "unavailable platform", edit: func(value *BootstrapResolution) { value.Platform = "riscv64" }},
		{name: "unexpected Rust field", edit: func(value *BootstrapResolution) { value.ManifestSHA256 = strings.Repeat("c", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.edit(&value)
			if err := ValidateBootstrapResolution(value); err == nil {
				t.Fatal("invalid bootstrap resolution was accepted")
			}
			if _, err := BootstrapResolutionDigest(value); err == nil {
				t.Fatal("invalid bootstrap resolution received a persistence digest")
			}
		})
	}
}
