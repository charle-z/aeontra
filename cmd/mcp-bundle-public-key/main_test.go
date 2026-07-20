package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestReadKeyAcceptsOnlyExactRawPrivateKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(path, privateKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readKey(path); err != nil || !got.Equal(privateKey) {
		t.Fatalf("key rejected: %v", err)
	}
	if err := os.WriteFile(path, privateKey[:len(privateKey)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKey(path); err == nil {
		t.Fatal("truncated key accepted")
	}
}
