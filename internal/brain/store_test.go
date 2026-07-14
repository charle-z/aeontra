package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenStoreCreatesPrivateDedicatedLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if store.Root() != root {
		t.Fatalf("root=%q want=%q", store.Root(), root)
	}
	for _, relative := range []string{"", CuratedDir, WorkingDir, CacheDir} {
		path := root
		if relative != "" {
			path = filepath.Join(root, relative)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat %s: %v", relative, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s mode=%v", relative, info.Mode())
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s permissions=%o", relative, info.Mode().Perm())
		}
	}
}

func TestOpenStoreRejectsRelativeBroadAndSymlinkRoots(t *testing.T) {
	if _, err := OpenStore("relative/brain", fixedNow); err == nil {
		t.Fatal("relative root unexpectedly succeeded")
	}

	broad := filepath.Join(t.TempDir(), "broad")
	if err := os.Mkdir(broad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(broad, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(broad, fixedNow); err == nil {
		t.Fatal("broad root unexpectedly succeeded")
	}

	realRoot := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "brain-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := OpenStore(link, fixedNow); err == nil {
		t.Fatal("symlink root unexpectedly succeeded")
	}
}

func TestStoreRejectsSymlinkTrustDirectoryAndSource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}

	working := filepath.Join(root, WorkingDir)
	if err := os.Remove(working); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, working); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.AgentTarget(TrustWorking, "safe-note"); err == nil {
		t.Fatal("symlink trust directory unexpectedly accepted")
	}

	if err := os.Remove(working); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(working, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "note.md")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(working, "safe-note.md")
	if err := os.Symlink(outsideFile, target); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if _, err := store.AgentTarget(TrustWorking, "safe-note"); err == nil {
		t.Fatal("symlink source unexpectedly accepted")
	}
}

func TestAgentTargetCanOnlyResolveWorkingNotes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AgentTarget(TrustCurated, "owner-fact"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "curated") {
		t.Fatalf("curated target error=%v", err)
	}
	target, err := store.AgentTarget(TrustWorking, "agent-note")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, WorkingDir, "agent-note.md")
	if target != want {
		t.Fatalf("target=%q want=%q", target, want)
	}
}

func TestReadSourceValidatesTrustFilenameAndGlobalUniqueness(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	curatedPath := filepath.Join(root, CuratedDir, "release-gates.md")
	if err := os.WriteFile(curatedPath, []byte(validSource("release-gates", TrustCurated, AuthorOwner)), 0o600); err != nil {
		t.Fatal(err)
	}
	note, err := store.ReadSource(TrustCurated, "release-gates")
	if err != nil {
		t.Fatal(err)
	}
	if note.Metadata.Slug != "release-gates" || note.Trust != TrustCurated {
		t.Fatalf("note=%+v", note)
	}

	workingPath := filepath.Join(root, WorkingDir, "release-gates.md")
	if err := os.WriteFile(workingPath, []byte(validSource("release-gates", TrustWorking, "agent:chatgpt")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindBySlug("release-gates"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestReadSourceRedactsManualSecretContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	source := validSource("manual-reference", TrustCurated, AuthorOwner)
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	source = strings.Replace(source, "Use the verified rollback procedure.", "Observed "+secret+" in historical evidence.", 1)
	path := filepath.Join(root, CuratedDir, "manual-reference.md")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	note, err := store.ReadSource(TrustCurated, "manual-reference")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(note.Body, secret) || !strings.Contains(note.Body, "***REDACTED-SECRET***") {
		t.Fatalf("body=%q", note.Body)
	}
}

func TestStoreRootIsNotAnExistingRepositorySelection(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "repos")
	brainRoot := filepath.Join(t.TempDir(), "brain")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(brainRoot, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if store.Root() == workspace || strings.HasPrefix(store.Root(), workspace+string(os.PathSeparator)) {
		t.Fatalf("brain root unexpectedly nested in workspace: %s", store.Root())
	}
}
