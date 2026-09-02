package edgeclient

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectToolchainReadinessReportsFixedBaselineAsSupported(t *testing.T) {
	workspace := t.TempDir()
	writeToolchainFixture(t, workspace, map[string]string{
		"go.mod":            "module example.com/project\ngo 1.26.6\n",
		"package.json":      `{"packageManager":"npm@12.0.2","scripts":{"test":"node test.js"}}`,
		"package-lock.json": "{\"lockfileVersion\":3}\n",
		"pyproject.toml":    "[project]\nrequires-python = \">=3.12,<3.15\"\n",
		"Cargo.toml":        "[package]\nname = \"example\"\nversion = \"0.1.0\"\n",
		"Makefile":          "test:\n\t@true\n",
	})

	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != ToolchainSupported {
		t.Fatalf("status=%q findings=%+v", readiness.Status, readiness.Findings)
	}
	wantManifests := []string{"package.json", "go.mod", "pyproject.toml", "Cargo.toml", "Makefile"}
	if !reflect.DeepEqual(readiness.Manifests, wantManifests) {
		t.Fatalf("manifests=%v want=%v", readiness.Manifests, wantManifests)
	}
	for _, finding := range readiness.Findings {
		if finding.Status != ToolchainSupported {
			t.Fatalf("unexpected finding: %+v", finding)
		}
	}
}

func TestDetectToolchainReadinessRequiresEdgeForMissingL3Capabilities(t *testing.T) {
	workspace := t.TempDir()
	writeToolchainFixture(t, workspace, map[string]string{
		"package.json":        `{"packageManager":"pnpm@10.13.1"}`,
		"pnpm-lock.yaml":      "lockfileVersion: '9.0'\n",
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.95.0\"\n",
		"pom.xml":             "<project/>\n",
		"CMakeLists.txt":      "cmake_minimum_required(VERSION 3.20)\n",
	})

	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != ToolchainEdgeRequired {
		t.Fatalf("status=%q findings=%+v", readiness.Status, readiness.Findings)
	}
	for _, want := range []string{"pnpm", "rust", "java", "cmake"} {
		if !hasReadinessFinding(readiness.Findings, want, ToolchainEdgeRequired) {
			t.Fatalf("missing Edge finding for %q: %+v", want, readiness.Findings)
		}
	}
}

func TestDetectToolchainReadinessReportsConflictingPins(t *testing.T) {
	workspace := t.TempDir()
	writeToolchainFixture(t, workspace, map[string]string{
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.96.0\"\n",
		".tool-versions":      "rust 1.95.0\n",
		"mise.toml":           "[tools]\nrust = \"1.94.0\"\n",
	})

	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != ToolchainPinConflict {
		t.Fatalf("status=%q findings=%+v", readiness.Status, readiness.Findings)
	}
	if !hasReadinessFinding(readiness.Findings, "rust", ToolchainPinConflict) {
		t.Fatalf("missing rust conflict: %+v", readiness.Findings)
	}
}

func TestDetectToolchainReadinessRequiresPackageLockAndRejectsUnsafeWorkspace(t *testing.T) {
	workspace := t.TempDir()
	writeToolchainFixture(t, workspace, map[string]string{
		"package.json": `{"name":"unlocked"}`,
	})
	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != ToolchainEdgeRequired || !hasReadinessFinding(readiness.Findings, "package-manager", ToolchainEdgeRequired) {
		t.Fatalf("readiness=%+v", readiness)
	}
	for _, invalid := range []string{"", "relative", string(os.PathSeparator)} {
		if _, err := DetectToolchainReadiness(invalid); err == nil {
			t.Fatalf("unsafe workspace accepted: %q", invalid)
		}
	}
}

func TestDetectToolchainReadinessIsBoundedAndDoesNotWrite(t *testing.T) {
	workspace := t.TempDir()
	for index := 0; index < 100; index++ {
		name := "go.mod"
		if index > 0 {
			name = "ignored-" + strings.Repeat("x", index) + ".txt"
		}
		writeToolchainFixture(t, workspace, map[string]string{name: "module example.com/project\ngo 1.26\n"})
	}
	sentinel := filepath.Join(workspace, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(readiness.Findings) > toolchainFindingLimit || len(readiness.Manifests) > len(toolchainManifestNames) {
		t.Fatalf("unbounded result: %+v", readiness)
	}
	after, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("detector changed sentinel")
	}
}

func TestDetectToolchainReadinessRejectsSymlinkManifest(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(target, []byte(`{"packageManager":"npm@12"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workspace, "package.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := DetectToolchainReadiness(workspace); err == nil {
		t.Fatal("symlink manifest accepted")
	}
}

func writeToolchainFixture(t *testing.T, workspace string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func hasReadinessFinding(findings []ToolchainReadinessFinding, tool string, status ToolchainReadinessStatus) bool {
	for _, finding := range findings {
		if finding.Tool == tool && finding.Status == status {
			return true
		}
	}
	return false
}
