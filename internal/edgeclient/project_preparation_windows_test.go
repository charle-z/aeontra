//go:build windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsProjectPreparationCloneFailureReleasesReservationBeforeCleanup(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "repo")
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	reservation, err := os.Open(candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	reserved, err := reservation.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeReservedProjectClone(root, candidate, reservation, reserved); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(candidate); !os.IsNotExist(statErr) {
		t.Fatalf("reserved directory remained: %v", statErr)
	}
}
