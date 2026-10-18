package brain

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConsoleIdentityPersistsAcrossRestart(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	keyPath := filepath.Join(stateRoot, "brain", "console-node.key")
	brainRoot := filepath.Join(t.TempDir(), "brain")

	first, err := OpenStore(brainRoot, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ConfigureConsoleIdentity(keyPath); err != nil {
		t.Fatal(err)
	}
	firstID, err := first.consoleNodeID("stable-private-slug")
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyBefore) != consoleIdentityBytes || !strings.HasPrefix(firstID, consoleNodeIDPrefix) || strings.Contains(firstID, "stable-private-slug") {
		t.Fatalf("id=%q key-bytes=%d", firstID, len(keyBefore))
	}

	second, err := OpenStore(brainRoot, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.ConfigureConsoleIdentity(keyPath); err != nil {
		t.Fatal(err)
	}
	secondID, err := second.consoleNodeID("stable-private-slug")
	if err != nil {
		t.Fatal(err)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if firstID != secondID || !bytes.Equal(keyBefore, keyAfter) {
		t.Fatalf("identity changed across restart: first=%q second=%q", firstID, secondID)
	}

	directoryInfo, err := os.Lstat(filepath.Dir(keyPath))
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory=%v err=%v", directoryInfo, err)
	}
	keyInfo, err := os.Lstat(keyPath)
	if err != nil || !keyInfo.Mode().IsRegular() || keyInfo.Mode()&os.ModeSymlink != 0 || keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("key=%v err=%v", keyInfo, err)
	}
}

func TestConsoleIdentityAtomicConcurrentCreationUsesOneKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "state", "brain", "console-node.key")
	const workers = 12
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			store, err := OpenStore(filepath.Join(t.TempDir(), "brain"), fixedNow)
			if err == nil {
				err = store.ConfigureConsoleIdentity(keyPath)
			}
			if err != nil {
				errs <- err
				return
			}
			id, err := store.consoleNodeID("same-private-slug")
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}(index)
	}
	group.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	var expected string
	count := 0
	for id := range ids {
		count++
		if expected == "" {
			expected = id
		} else if id != expected {
			t.Fatalf("concurrent creators loaded different keys: %q != %q", id, expected)
		}
	}
	if count != workers {
		t.Fatalf("ids=%d want=%d", count, workers)
	}
}

func TestConsoleIdentityRejectsUnsafePathsWithoutRepair(t *testing.T) {
	newStore := func(t *testing.T) *Store {
		t.Helper()
		store, err := OpenStore(filepath.Join(t.TempDir(), "brain"), fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		return store
	}

	t.Run("broad directory permissions", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "state", "brain")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "console-node.key")
		if err := newStore(t).ConfigureConsoleIdentity(path); err == nil || strings.Contains(err.Error(), path) {
			t.Fatalf("error=%v", err)
		}
		info, err := os.Lstat(directory)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("unsafe directory was repaired: %v err=%v", info, err)
		}
	})

	t.Run("broad file permissions", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "state", "brain")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "console-node.key")
		if err := os.WriteFile(path, bytes.Repeat([]byte{0x41}, consoleIdentityBytes), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := newStore(t).ConfigureConsoleIdentity(path); err == nil || strings.Contains(err.Error(), path) {
			t.Fatalf("error=%v", err)
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("unsafe file was repaired: %v err=%v", info, err)
		}
	})

	t.Run("symlink file", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "state", "brain")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside.key")
		if err := os.WriteFile(outside, bytes.Repeat([]byte{0x42}, consoleIdentityBytes), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "console-node.key")
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := newStore(t).ConfigureConsoleIdentity(path); err == nil || strings.Contains(err.Error(), outside) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("symlink ancestor", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "brain")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		path := filepath.Join(link, "console-node.key")
		if err := newStore(t).ConfigureConsoleIdentity(path); err == nil || strings.Contains(err.Error(), outside) {
			t.Fatalf("error=%v", err)
		}
	})
}
