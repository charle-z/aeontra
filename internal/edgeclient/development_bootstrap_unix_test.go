//go:build !windows

package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestSelectGoBootstrapArtifactUsesNumericStablePatchAndExactArchitecture(t *testing.T) {
	metadata := []byte(`[
  {"version":"go1.26.9","stable":true,"files":[{"filename":"go1.26.9.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.9","sha256":"` + strings.Repeat("9", 64) + `","size":70000000,"kind":"archive"}]},
  {"version":"go1.26.10","stable":true,"files":[{"filename":"go1.26.10.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.10","sha256":"` + strings.Repeat("a", 64) + `","size":71000000,"kind":"archive"},{"filename":"go1.26.10.linux-arm64.tar.gz","os":"linux","arch":"arm64","version":"go1.26.10","sha256":"` + strings.Repeat("b", 64) + `","size":71000000,"kind":"archive"}]},
  {"version":"go1.26.99rc1","stable":false,"files":[{"filename":"go1.26.99rc1.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.99rc1","sha256":"` + strings.Repeat("c", 64) + `","size":71000000,"kind":"archive"}]}
]`)

	resolved, err := selectGoBootstrapArtifact(metadata, development.CapabilityID("toolchain.go.v1-26"), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Version != "1.26.10" || resolved.ArtifactFile != "go1.26.10.linux-amd64.tar.gz" || resolved.ArtifactSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("minor selector did not choose the highest stable numeric patch: %+v", resolved)
	}

	exact, err := selectGoBootstrapArtifact(metadata, development.CapabilityID("toolchain.go.v1-26-9"), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if exact.Version != "1.26.9" || exact.ArtifactSHA256 != strings.Repeat("9", 64) {
		t.Fatalf("exact selector resolved a different patch: %+v", exact)
	}

	arm64, err := selectGoBootstrapArtifact(metadata, development.CapabilityID("toolchain.go.v1-26-10"), "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if arm64.ArtifactFile != "go1.26.10.linux-arm64.tar.gz" || arm64.ArtifactSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("architecture selector chose the wrong artifact: %+v", arm64)
	}

	archived := []byte(`[{"version":"go1.10.8","stable":true,"files":[{"filename":"go1.10.8.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.10.8","sha256":"` + strings.Repeat("d", 64) + `","size":102184719,"kind":"archive"}]}]`)
	archivedResolution, err := selectGoBootstrapArtifact(archived, development.CapabilityID("toolchain.go.v1-10-8"), "amd64")
	if err != nil || archivedResolution.Version != "1.10.8" || archivedResolution.ArtifactFile != "go1.10.8.linux-amd64.tar.gz" {
		t.Fatalf("an archived exact Go pin was not resolvable: %+v, %v", archivedResolution, err)
	}
}

func TestSelectGoBootstrapArtifactRejectsUnsupportedOrMalformedMetadata(t *testing.T) {
	good := []byte(`[{"version":"go1.26.10","stable":true,"files":[{"filename":"go1.26.10.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.10","sha256":"` + strings.Repeat("a", 64) + `","size":71000000,"kind":"archive"}]}]`)
	for _, test := range []struct {
		name         string
		metadata     []byte
		capability   development.CapabilityID
		architecture string
	}{
		{name: "unknown capability", metadata: good, capability: "toolchain.python.v3-13-1", architecture: "amd64"},
		{name: "unversioned capability", metadata: good, capability: "toolchain.go", architecture: "amd64"},
		{name: "major-only selector", metadata: good, capability: "toolchain.go.v1", architecture: "amd64"},
		{name: "injected selector", metadata: good, capability: "toolchain.go.v1-26;touch-x", architecture: "amd64"},
		{name: "unsupported architecture", metadata: good, capability: "toolchain.go.v1-26-10", architecture: "riscv64"},
		{name: "missing exact version", metadata: good, capability: "toolchain.go.v1-26-11", architecture: "amd64"},
		{name: "checksum invalid", metadata: []byte(`[{"version":"go1.26.10","stable":true,"files":[{"filename":"go1.26.10.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.10","sha256":"not-a-digest","size":71000000,"kind":"archive"}]}]`), capability: "toolchain.go.v1-26-10", architecture: "amd64"},
		{name: "filename does not match metadata", metadata: []byte(`[{"version":"go1.26.10","stable":true,"files":[{"filename":"go1.26.11.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.26.10","sha256":"` + strings.Repeat("a", 64) + `","size":71000000,"kind":"archive"}]}]`), capability: "toolchain.go.v1-26-10", architecture: "amd64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := selectGoBootstrapArtifact(test.metadata, test.capability, test.architecture); err == nil {
				t.Fatal("unsupported bootstrap selector or metadata was accepted")
			}
		})
	}
}

func TestOfficialSHA256SidecarAcceptsOfficialArtifactNames(t *testing.T) {
	digest := "4acc9acc76d5079515b46346a485974457b5a79893cfb01112423c89aeb5aa10"
	for _, name := range []string{"", "rustup-init", "*rustup-init", "./rustup-init", "*./rustup-init"} {
		t.Run(name, func(t *testing.T) {
			got, err := parseOfficialSHA256Sidecar([]byte(digest+" "+name+"\n"), "rustup-init")
			if err != nil || got != digest {
				t.Fatalf("official artifact checksum was rejected: got=%q err=%v", got, err)
			}
		})
	}
}

func TestOfficialSHA256SidecarRejectsOtherArtifactNames(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, name := range []string{"other-artifact", "dir/rustup-init", "../rustup-init", "/rustup-init", "././rustup-init", "**./rustup-init", "rustup-init extra"} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseOfficialSHA256Sidecar([]byte(digest+" "+name+"\n"), "rustup-init"); err == nil {
				t.Fatal("a different artifact name was accepted")
			}
		})
	}
	for _, body := range []string{"", "invalid rustup-init", strings.Repeat("a", developmentBootstrapSHA256Limit+1)} {
		if _, err := parseOfficialSHA256Sidecar([]byte(body), "rustup-init"); err == nil {
			t.Fatal("invalid checksum metadata was accepted")
		}
	}
}

func TestDevelopmentBootstrapRecipeIsFixedQuotedAndRuntimeScoped(t *testing.T) {
	goResolution := DevelopmentBootstrapResolution{
		CapabilityID: "toolchain.go.v1-26-6", Toolchain: "go", Version: "1.26.6", Platform: "amd64",
		ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("a", 64), ArtifactSize: 70_000_000,
	}
	goRecipe, err := BuildDevelopmentBootstrapRecipe(goResolution)
	if err != nil {
		t.Fatal(err)
	}
	if len(goRecipe.Argv) != 4 || goRecipe.Argv[0] != "/bin/sh" || goRecipe.Argv[1] != "-c" || goRecipe.Argv[3] != "mcp-devbox-development-bootstrap" {
		t.Fatalf("Go recipe argv is not the fixed server shell recipe: %#v", goRecipe.Argv)
	}
	goScript := goRecipe.Argv[2]
	for _, required := range []string{"https://go.dev/dl/", "--proto", "--tlsv1.2", "sha256sum -c", "flock", "timeout", "mv -T", "/runtime/tools", "/runtime/go", "/cache/go-mod", "version=\"1.26.6\"", "go version", "mcp-devbox-bootstrap-verified=go:"} {
		if !strings.Contains(goScript, required) {
			t.Fatalf("Go recipe is missing %q", required)
		}
	}
	for _, forbidden := range []string{"sudo", "/var/run/docker.sock", ".cargo", "rm -rf /runtime", "rm -rf /workspace", "https://example.invalid", "1.26.6;"} {
		if strings.Contains(goScript, forbidden) {
			t.Fatalf("Go recipe contains forbidden authority or input text %q", forbidden)
		}
	}
	if goRecipe.Timeout <= 0 || goRecipe.Timeout > bootstrapRecipeHardTimeout+bootstrapRecipeKillGrace || goRecipe.Environment["GOBIN"] != "/runtime/tools/bin" || goRecipe.Environment["GOMODCACHE"] != "/cache/go-mod" {
		t.Fatalf("Go recipe timeout or runtime environment is invalid: %+v", goRecipe)
	}

	rustResolution := DevelopmentBootstrapResolution{
		CapabilityID: "toolchain.rust.v1-95-0", Toolchain: "rust", Version: "1.95.0", Platform: "x86_64-unknown-linux-gnu",
		InstallerSHA256: strings.Repeat("b", 64), ManifestSHA256: strings.Repeat("c", 64),
	}
	rustRecipe, err := BuildDevelopmentBootstrapRecipe(rustResolution)
	if err != nil {
		t.Fatal(err)
	}
	rustScript := rustRecipe.Argv[2]
	for _, required := range []string{"https://static.rust-lang.org/rustup/archive/1.29.0/$target/rustup-init", "toolchain install", "--profile minimal --no-self-update", "version=\"1.95.0\"", "CARGO_HOME=/runtime/cargo", "RUSTUP_HOME=/runtime/rustup", "rustc --version", "cargo --version", "mcp-devbox-bootstrap-verified=rust:"} {
		if !strings.Contains(rustScript, required) {
			t.Fatalf("Rust recipe is missing %q", required)
		}
	}
	for _, forbidden := range []string{"sudo", "/var/run/docker.sock", "/workspace/.cargo", "https://example.invalid", "stable", "nightly"} {
		if strings.Contains(rustScript, forbidden) {
			t.Fatalf("Rust recipe contains forbidden authority or floating channel %q", forbidden)
		}
	}
	if rustRecipe.Environment["CARGO_HOME"] != "/runtime/cargo" || rustRecipe.Environment["RUSTUP_HOME"] != "/runtime/rustup" {
		t.Fatalf("Rust recipe does not use the runtime-owned homes: %+v", rustRecipe.Environment)
	}
}

func TestDevelopmentBootstrapCapabilitiesRequireVerifiedRuntimeMarker(t *testing.T) {
	goResolution := DevelopmentBootstrapResolution{
		CapabilityID: "toolchain.go.v1-26-6", Toolchain: "go", Version: "1.26.6", Platform: "amd64",
		ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("a", 64), ArtifactSize: 70_000_000,
	}
	goCapabilities, err := DevelopmentBootstrapCapabilities(goResolution, "go version go1.26.6 linux/amd64\nmcp-devbox-bootstrap-verified=go:1.26.6\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []development.CapabilityID{"toolchain.go", "toolchain.go.v1", "toolchain.go.v1-26", "toolchain.go.v1-26-6"} {
		if !goCapabilities.Has(capability) {
			t.Fatalf("verified Go probe did not attest %q: %+v", capability, goCapabilities.IDs())
		}
	}
	if _, err := DevelopmentBootstrapCapabilities(goResolution, "mcp-devbox-bootstrap-verified=go:1.26.7\n"); err == nil {
		t.Fatal("mismatched Go runtime version was attested")
	}
	if _, err := DevelopmentBootstrapCapabilities(goResolution, "go version go1.26.6 linux/amd64\n"); err == nil {
		t.Fatal("Go runtime was attested without the post-install verification marker")
	}

	rustResolution := DevelopmentBootstrapResolution{
		CapabilityID: "toolchain.rust.v1-95-0", Toolchain: "rust", Version: "1.95.0", Platform: "x86_64-unknown-linux-gnu",
		InstallerSHA256: strings.Repeat("b", 64), ManifestSHA256: strings.Repeat("c", 64),
	}
	rustCapabilities, err := DevelopmentBootstrapCapabilities(rustResolution, "rustc 1.95.0 (hash 2026-01-01)\ncargo 1.95.0 (hash 2026-01-01)\nmcp-devbox-bootstrap-verified=rust:1.95.0:cargo:1.95.0\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []development.CapabilityID{"toolchain.rust", "toolchain.rust.v1-95-0", "toolchain.cargo", "toolchain.cargo.v1-95-0"} {
		if !rustCapabilities.Has(capability) {
			t.Fatalf("verified Rust probes did not attest %q: %+v", capability, rustCapabilities.IDs())
		}
	}
	if _, err := DevelopmentBootstrapCapabilities(rustResolution, "rustc 1.95.0 (hash)\ncargo 1.95.0 (hash)\nmcp-devbox-bootstrap-verified=rust:1.95.0:cargo:1.95.1\n"); err == nil {
		t.Fatal("mismatched Rust/Cargo version was attested")
	}
}

func TestGoBootstrapRecipeInstallsAtomicallyAndRetriesWithoutRedownload(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("bootstrap recipe requires a supported Linux runtime")
	}
	const version = "1.26.6"
	architecture, err := goBootstrapArchitecture(runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot := t.TempDir()
	goBinary := filepath.Join(fixtureRoot, "go", "bin", "go")
	if err := os.MkdirAll(filepath.Dir(goBinary), 0o700); err != nil {
		t.Fatal(err)
	}
	fixtureScript := fmt.Sprintf("#!/bin/sh\nprintf 'go version go%s linux/%s\\n'\n", version, architecture)
	if err := os.WriteFile(goBinary, []byte(fixtureScript), 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "go-fixture.tar.gz")
	createArchive := exec.Command("tar", "-czf", archivePath, "-C", fixtureRoot, "go")
	if output, err := createArchive.CombinedOutput(); err != nil {
		t.Fatalf("create test tar fixture: %v: %s", err, output)
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := sha256.Sum256(archive)
	goodResolution := DevelopmentBootstrapResolution{
		CapabilityID: "toolchain.go.v1-26-6", Toolchain: "go", Version: version, Platform: architecture,
		ArtifactFile: "go" + version + ".linux-" + architecture + ".tar.gz", ArtifactSHA256: hex.EncodeToString(archiveDigest[:]), ArtifactSize: int64(len(archive)),
	}

	testRoot := t.TempDir()
	workingRuntime := filepath.Join(testRoot, "runtime")
	if err := os.Mkdir(workingRuntime, 0o700); err != nil {
		t.Fatal(err)
	}
	mockBin := filepath.Join(testRoot, "mock-bin")
	if err := os.Mkdir(mockBin, 0o700); err != nil {
		t.Fatal(err)
	}
	curlCalls := filepath.Join(testRoot, "curl-calls")
	curlMock := "#!/bin/sh\n" +
		"set -eu\n" +
		"output= url=\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    --output) output=$2; shift 2 ;;\n" +
		"    --write-out|--max-filesize|--max-redirs|--connect-timeout|--max-time|--proto|--proto-redir) shift 2 ;;\n" +
		"    https://*) url=$1; shift ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"[ -n \"$output\" ] && [ -n \"$url\" ]\n" +
		"printf x >>\"$BOOTSTRAP_TEST_CURL_COUNT\"\n" +
		"cp \"$BOOTSTRAP_TEST_ARCHIVE\" \"$output\"\n" +
		"printf '%s' \"$url\"\n"
	if err := os.WriteFile(filepath.Join(mockBin, "curl"), []byte(curlMock), 0o700); err != nil {
		t.Fatal(err)
	}
	makeRecipe := func(resolution DevelopmentBootstrapResolution) string {
		t.Helper()
		recipe, err := BuildDevelopmentBootstrapRecipe(resolution)
		if err != nil {
			t.Fatal(err)
		}
		// Bubblewrap uses a fixed /runtime mount; replace only that mount for
		// this isolated execution test, never the host runtime directory.
		return strings.ReplaceAll(recipe.Argv[2], "/runtime", workingRuntime)
	}
	runRecipe := func(script string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		command.Env = append(os.Environ(),
			"PATH="+mockBin+":"+os.Getenv("PATH"),
			"BOOTSTRAP_TEST_ARCHIVE="+archivePath,
			"BOOTSTRAP_TEST_CURL_COUNT="+curlCalls,
		)
		return command.CombinedOutput()
	}

	badResolution := goodResolution
	badResolution.ArtifactSHA256 = strings.Repeat("b", 64)
	badOutput, err := runRecipe(makeRecipe(badResolution))
	if err == nil || strings.Contains(string(badOutput), "mcp-devbox-bootstrap-verified=") {
		t.Fatalf("bad artifact checksum unexpectedly installed or attested: err=%v output=%s", err, badOutput)
	}
	destination := filepath.Join(workingRuntime, "tools", "go", ".versions", "go"+version)
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("checksum failure left a published toolchain directory: %v", err)
	}

	goodScript := makeRecipe(goodResolution)
	firstOutput, err := runRecipe(goodScript)
	if err != nil {
		t.Fatalf("first install failed: %v: %s", err, firstOutput)
	}
	secondOutput, err := runRecipe(goodScript)
	if err != nil {
		t.Fatalf("idempotent retry failed: %v: %s", err, secondOutput)
	}
	for _, output := range [][]byte{firstOutput, secondOutput} {
		if !strings.Contains(string(output), "go version go"+version+" linux/"+architecture) || !containsExactBootstrapMarker(string(output), "mcp-devbox-bootstrap-verified=go:"+version) {
			t.Fatalf("successful recipe omitted its reread version or unique marker: %s", output)
		}
	}
	linkPath := filepath.Join(workingRuntime, "tools", "bin", "go")
	linkTarget, err := os.Readlink(linkPath)
	if err != nil || linkTarget != filepath.Join(destination, "bin", "go") {
		t.Fatalf("atomic selector points at the wrong installed binary: target=%q err=%v", linkTarget, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "bin", "go")); err != nil {
		t.Fatalf("installed Go binary is not present at the published layout: %v", err)
	}
	calls, err := os.ReadFile(curlCalls)
	if err != nil || string(calls) != "xx" {
		t.Fatalf("retry did not reuse the verified installation without a third download: calls=%q err=%v", calls, err)
	}
}
