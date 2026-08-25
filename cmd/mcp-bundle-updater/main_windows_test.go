//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
	"golang.org/x/sys/windows"
)

func TestParseWindowsUpdaterOperationIsClosed(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"status"}, "status"},
		{[]string{"update", "stable"}, "update"},
		{[]string{"rollback"}, "rollback"},
	} {
		got, err := parseWindowsUpdaterOperation(test.args)
		if err != nil || got != test.want {
			t.Fatalf("%v -> %q, %v; want %q", test.args, got, err, test.want)
		}
	}
	for _, args := range [][]string{
		{"update", "https://example.invalid/bundle"},
		{"update", "stable", "--path", "x"},
		{"install", "x"},
		{"status", "extra"},
	} {
		if _, err := parseWindowsUpdaterOperation(args); err == nil {
			t.Fatalf("open operation accepted: %v", args)
		}
	}
}

func TestWindowsInstallRootFromUpdaterAcceptsManagedFixedDriveLayout(t *testing.T) {
	originalType, originalReady := windowsManagedDriveType, windowsManagedDriveReady
	windowsManagedDriveType = func(string) uint32 { return windows.DRIVE_FIXED }
	windowsManagedDriveReady = func(string) bool { return true }
	t.Cleanup(func() {
		windowsManagedDriveType, windowsManagedDriveReady = originalType, originalReady
	})
	for _, executable := range []string{
		`C:\Program Files\Aeontra\Edge\releases\v1.2.1\bin\mcp-bundle-updater.exe`,
		`D:\Aeontra\Edge\releases\v1.2.1\bin\mcp-bundle-updater.exe`,
	} {
		root, err := windowsInstallRootFromUpdater(executable)
		if err != nil {
			t.Fatalf("%s: %v", executable, err)
		}
		if !strings.EqualFold(filepath.Base(root), "Edge") || !strings.EqualFold(filepath.Base(filepath.Dir(root)), "Aeontra") {
			t.Fatalf("unexpected install root %q", root)
		}
	}
}

func TestWindowsInstallRootFromUpdaterRejectsUnmanagedLayout(t *testing.T) {
	for _, executable := range []string{
		`D:\mcp-bundle-updater.exe`,
		`D:\Aeontra\Edge\bin\mcp-bundle-updater.exe`,
		`D:\Other\Edge\releases\v1.2.1\bin\mcp-bundle-updater.exe`,
		`D:\Aeontra\Edge\releases\v1.2.1\bin\other.exe`,
	} {
		if _, err := windowsInstallRootFromUpdater(executable); err == nil {
			t.Fatalf("unmanaged updater layout accepted: %s", executable)
		}
	}
}

func TestCleanManagedWindowsRootRequiresFixedManagedLayout(t *testing.T) {
	originalType, originalReady := windowsManagedDriveType, windowsManagedDriveReady
	windowsManagedDriveType = func(string) uint32 { return windows.DRIVE_FIXED }
	windowsManagedDriveReady = func(string) bool { return true }
	t.Cleanup(func() {
		windowsManagedDriveType, windowsManagedDriveReady = originalType, originalReady
	})
	valid := filepath.Join(t.TempDir(), "Aeontra", "State")
	if _, err := cleanManagedWindowsRoot(valid, "State", true); err != nil {
		t.Fatalf("managed state rejected: %v", err)
	}
	if _, err := cleanManagedWindowsRoot(filepath.Join(t.TempDir(), "Other", "State"), "State", true); err == nil {
		t.Fatal("unmanaged parent accepted")
	}
	if _, err := cleanManagedWindowsRoot(filepath.Join(t.TempDir(), "Aeontra", "Other"), "State", true); err == nil {
		t.Fatal("unmanaged leaf accepted")
	}
	if _, err := cleanManagedWindowsRoot(filepath.Join(t.TempDir(), "Aeontra", "Edge"), "State", true); err == nil {
		t.Fatal("non-ProgramData legacy state accepted")
	}
	windowsManagedDriveType = func(string) uint32 { return windows.DRIVE_REMOVABLE }
	if _, err := cleanManagedWindowsRoot(valid, "State", true); err == nil {
		t.Fatal("removable drive accepted")
	}
}

func TestWindowsArchiveRejectsTraversalAndRequiresClosedLayout(t *testing.T) {
	root := t.TempDir()
	archive := makeWindowsArchive(t, map[string][]byte{
		"../escape": []byte("bad"),
	})
	if err := extractWindowsArchive(archive, root); err == nil {
		t.Fatal("traversal archive accepted")
	}
	archive = makeWindowsArchive(t, map[string][]byte{
		bundleManifestName: []byte("{}"),
	})
	if err := extractWindowsArchive(archive, root); err == nil {
		t.Fatal("incomplete archive accepted")
	}
}

func TestWindowsArchiveExtractsOnlySignedLayout(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{bundleManifestName: []byte("manifest"), bundleSignatureName: []byte("signature")}
	for relative := range windowsLayoutForTest() {
		files[relative] = []byte(relative)
	}
	archive := makeWindowsArchive(t, files)
	if err := extractWindowsArchive(archive, root); err != nil {
		t.Fatal(err)
	}
	for name, expected := range files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || string(content) != string(expected) {
			t.Fatalf("%s: content=%q err=%v", name, content, err)
		}
	}
}

func TestWindowsArchiveRejectsBackslashAndDuplicateEntries(t *testing.T) {
	root := t.TempDir()
	archive := makeWindowsArchive(t, map[string][]byte{"bin\\mcp-edge.exe": []byte("bad")})
	if err := extractWindowsArchive(archive, root); err == nil {
		t.Fatal("backslash archive entry accepted")
	}
	var encoded bytes.Buffer
	writer := zip.NewWriter(&encoded)
	for i := 0; i < 2; i++ {
		header := &zip.FileHeader{Name: bundleManifestName, Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("manifest")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractWindowsArchive(encoded.Bytes(), root); err == nil {
		t.Fatal("duplicate archive entry accepted")
	}
}

func TestWindowsServiceConfigRejectsUnknownFieldsAndWrongOwner(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install", "Aeontra", "Edge")
	state := filepath.Join(root, "state-drive", "Aeontra", "State")
	workspace := filepath.Join(root, "workspace-drive", "Aeontra", "Workspaces")
	for _, path := range []string{installRoot, state, workspace} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	filename := filepath.Join(installRoot, "service-config.json")
	validBytes, err := json.Marshal(windowsServiceConfig{Version: 1, Service: windowsServiceName, ServiceIdentity: windowsServiceIdentity, StateRoot: state, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	valid := string(validBytes)
	if err := os.WriteFile(filename, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := readWindowsServiceConfig(filename, installRoot)
	if err != nil || config.WorkspaceRoot != workspace {
		t.Fatalf("valid config rejected: %+v %v", config, err)
	}
	if err := os.WriteFile(filename, []byte(strings.TrimSuffix(valid, "}")+`,"extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowsServiceConfig(filename, installRoot); err == nil {
		t.Fatal("unknown config field accepted")
	}
	foreignBytes, err := json.Marshal(windowsServiceConfig{Version: 1, Service: windowsServiceName, ServiceIdentity: windowsServiceIdentity, StateRoot: filepath.Join(root, "other"), WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, foreignBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowsServiceConfig(filename, installRoot); err == nil {
		t.Fatal("foreign state root accepted")
	}
}

func TestWindowsServiceConfigAcceptsLegacyStateAndDifferentFixedDrives(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install", "Aeontra", "Edge")
	legacyState := filepath.Join(root, "legacy", "Aeontra", "Edge")
	workspace := filepath.Join(root, "workspace", "Aeontra", "Workspaces")
	originalLegacyRoot := windowsLegacyStateRoot
	windowsLegacyStateRoot = func() (string, error) { return legacyState, nil }
	t.Cleanup(func() { windowsLegacyStateRoot = originalLegacyRoot })
	for _, path := range []string{installRoot, legacyState, workspace} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	filename := filepath.Join(installRoot, "service-config.json")
	content, err := json.Marshal(windowsServiceConfig{Version: 1, Service: windowsServiceName, ServiceIdentity: windowsServiceIdentity, StateRoot: legacyState, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, content, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := readWindowsServiceConfig(filename, installRoot)
	if err != nil || config.StateRoot != legacyState || config.WorkspaceRoot != workspace {
		t.Fatalf("legacy managed config rejected: %#v %v", config, err)
	}
}

func TestVerifyDigestRejectsMalformedAndAcceptsExact(t *testing.T) {
	content := []byte("release")
	if err := verifyDigest(content, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if err := verifyDigest(content, "sha256:bad"); err == nil {
		t.Fatal("malformed digest accepted")
	}
	if err := verifyDigest(content, digestForTest(content)); err != nil {
		t.Fatal(err)
	}
}

type fakeWindowsService struct {
	fail  bool
	calls int
}

func (f *fakeWindowsService) SwapAndStart(_, _ string, _ windowsServiceConfig) error {
	f.calls++
	if f.fail {
		return errors.New("simulated service failure")
	}
	return nil
}

func (f *fakeWindowsService) Verify(_ string, _ windowsServiceConfig) error { return nil }

func (f *fakeWindowsService) Active() bool { return true }

type trackingWindowsService struct {
	runningBinary string
}

func (s *trackingWindowsService) SwapAndStart(_, newBinary string, _ windowsServiceConfig) error {
	s.runningBinary = newBinary
	return nil
}

func (s *trackingWindowsService) Verify(binary string, _ windowsServiceConfig) error {
	if s.runningBinary != binary {
		return errors.New("service binary mismatch")
	}
	return nil
}

func (s *trackingWindowsService) Active() bool { return s.runningBinary != "" }

func TestWindowsTransactionRecoversMarkersFromRunningOldService(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.0"), "v1.0.0", strings.Repeat("a", 40), privateKey)
	current := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.1"), "v1.0.1", strings.Repeat("b", 40), privateKey)
	target := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.2"), "v1.0.2", strings.Repeat("c", 40), privateKey)
	if err := writeWindowsMarker(filepath.Join(root, "active.json"), current); err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsMarker(filepath.Join(root, "previous.json"), old); err != nil {
		t.Fatal(err)
	}
	tx := windowsUpdateTransaction{Version: 1, Operation: "update", Phase: transactionMarkersWritten,
		OldActive: current, OldPrevious: old, TargetActive: target, TargetPrevious: current}
	if err := writeWindowsTransaction(filepath.Join(root, windowsTransactionFile), tx, root); err != nil {
		t.Fatal(err)
	}
	service := &trackingWindowsService{runningBinary: filepath.Join(root, "releases", current.Release, "bin", "mcp-edge.exe")}
	updater := &windowsUpdater{installRoot: root, publicKey: publicKey, service: service}
	if err := updater.recoverPendingTransaction(windowsServiceConfig{}); err != nil {
		t.Fatal(err)
	}
	active, previous, err := updater.readMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if active.Release != current.Release || previous.Release != old.Release {
		t.Fatalf("recovery changed authoritative markers: active=%+v previous=%+v", active, previous)
	}
	if _, err := os.Stat(filepath.Join(root, windowsTransactionFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains: %v", err)
	}
}

func TestWindowsStatusRequiresExactRunningServiceBinary(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	active := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.1"), "v1.0.1", strings.Repeat("b", 40), privateKey)
	if err := writeWindowsMarker(filepath.Join(root, "active.json"), active); err != nil {
		t.Fatal(err)
	}
	service := &trackingWindowsService{runningBinary: filepath.Join(root, "releases", "v1.0.0", "bin", "mcp-edge.exe")}
	updater := &windowsUpdater{installRoot: root, publicKey: publicKey, service: service}
	if _, err := updater.status(windowsServiceConfig{}); err == nil {
		t.Fatal("status accepted a service running another release")
	}
	service.runningBinary = filepath.Join(root, "releases", active.Release, "bin", "mcp-edge.exe")
	status, err := updater.status(windowsServiceConfig{})
	if err != nil || !status.ServiceActive || status.Release != active.Release {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestWindowsTransactionVerifiesServiceAndClearsJournal(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.0"), "v1.0.0", strings.Repeat("a", 40), privateKey)
	target := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.1"), "v1.0.1", strings.Repeat("b", 40), privateKey)
	if err := writeWindowsMarker(filepath.Join(root, "active.json"), old); err != nil {
		t.Fatal(err)
	}
	service := &trackingWindowsService{runningBinary: filepath.Join(root, "releases", old.Release, "bin", "mcp-edge.exe")}
	updater := &windowsUpdater{installRoot: root, publicKey: publicKey, service: service}
	tx := windowsUpdateTransaction{Version: 1, Operation: "update", Phase: transactionPrepared,
		OldActive: old, TargetActive: target, TargetPrevious: old}
	if err := updater.commitWindowsTransaction(tx,
		filepath.Join(root, "releases", old.Release, "bin", "mcp-edge.exe"),
		filepath.Join(root, "releases", target.Release, "bin", "mcp-edge.exe"), windowsServiceConfig{}); err != nil {
		t.Fatal(err)
	}
	active, previous, err := updater.readMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if active.Release != target.Release || previous.Release != old.Release {
		t.Fatalf("unexpected final markers: active=%+v previous=%+v", active, previous)
	}
	if _, err := os.Stat(filepath.Join(root, windowsTransactionFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction journal remains: %v", err)
	}
}

func TestWindowsRollbackRestoresMarkersWhenServiceFails(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.0"), "v1.0.0", strings.Repeat("a", 40), privateKey)
	current := writeSignedWindowsRelease(t, filepath.Join(root, "releases", "v1.0.1"), "v1.0.1", strings.Repeat("b", 40), privateKey)
	updater := &windowsUpdater{installRoot: root, publicKey: publicKey, service: &fakeWindowsService{fail: true}}
	if err := updater.writeActive(current); err != nil {
		t.Fatal(err)
	}
	if err := updater.writePrevious(old); err != nil {
		t.Fatal(err)
	}
	config := windowsServiceConfig{StateRoot: filepath.Join(root, "state"), WorkspaceRoot: filepath.Join(root, "workspace")}
	if _, err := updater.rollback(config); err == nil {
		t.Fatal("failed service rollback accepted")
	}
	active, previous, err := updater.readMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if active.Release != current.Release || previous.Release != old.Release {
		t.Fatalf("markers changed after failed rollback: active=%+v previous=%+v", active, previous)
	}
}

const (
	bundleManifestName  = "manifest.json"
	bundleSignatureName = "manifest.sig"
)

func windowsLayoutForTest() map[string]struct{} {
	files := map[string]struct{}{}
	for _, relative := range []string{"bin/mcp-edge.exe", "bin/mcp-bundle-updater.exe", "install-edge.ps1", "uninstall-edge.ps1"} {
		files[relative] = struct{}{}
	}
	return files
}

func makeWindowsArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	writer := zip.NewWriter(&encoded)
	for name, content := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func digestForTest(content []byte) string {
	// Keep this helper local to the test so the production verifier remains the
	// only code that defines accepted digest syntax.
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeSignedWindowsRelease(t *testing.T, root, release, commit string, privateKey ed25519.PrivateKey) windowsActiveMarker {
	t.Helper()
	for _, relative := range bundle.WindowsLayout() {
		filename := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(release+relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	metadata := bundle.Metadata{Release: release, Commit: commit, ProtocolVersion: buildinfo.EdgeBundleProtocolVersion, CatalogHash: "sha256:" + strings.Repeat("c", 64), Architecture: "amd64", Platform: "windows"}
	manifest, err := bundle.BuildVersion(root, metadata, 6)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := bundle.Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bundle.ManifestFile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bundle.SignatureFile), signature, 0o600); err != nil {
		t.Fatal(err)
	}
	return windowsActiveMarker{Version: 1, Release: release, Commit: commit, Path: root}
}
