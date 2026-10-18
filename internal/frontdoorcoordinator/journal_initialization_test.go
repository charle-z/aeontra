package frontdoorcoordinator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenJournalMaterializesDurableIdleState(t *testing.T) {
	root := t.TempDir()
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, JournalFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("journal permissions=%#o, want 0600", info.Mode().Perm())
	}
	status, err := journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if status.SchemaVersion != 1 || status.Revision != 0 || status.Target != TargetIdle || status.State != StateIdle {
		t.Fatalf("unexpected initial journal: %+v", status)
	}

	reopened, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	status, err = reopened.Read()
	if err != nil {
		t.Fatal(err)
	}
	if status.Revision != 0 || status.State != StateIdle {
		t.Fatalf("reopen changed idle journal: %+v", status)
	}
}

func TestOpenJournalRejectsKnownJournalSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, JournalFilename)); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(root); err == nil {
		t.Fatal("journal symlink was accepted")
	}
}
