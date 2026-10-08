//go:build windows

package main

import (
	"bytes"
	"io"
	"testing"
)

func TestWindowsLifecycleAcceptsOnlyClosedOperations(t *testing.T) {
	for _, args := range [][]string{
		nil, {"inspect", "extra"}, {"update"}, {"update", "latest"}, {"rollback", "extra"}, {"migrate-state"}, {"unknown"},
	} {
		if err := lifecycleCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("unsafe Windows lifecycle arguments accepted: %v", args)
		}
	}
}

func TestWindowsLifecycleDelegatesMutationsToSignedUpdater(t *testing.T) {
	old := runWindowsDoctorUpdater
	t.Cleanup(func() { runWindowsDoctorUpdater = old })
	var got []string
	runWindowsDoctorUpdater = func(args []string, stdout, stderr io.Writer) error {
		got = append([]string(nil), args...)
		return nil
	}
	if err := lifecycleCommand([]string{"update", "stable"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "update" || got[1] != "stable" {
		t.Fatalf("unexpected updater args: %v", got)
	}
	got = nil
	if err := lifecycleCommand([]string{"rollback"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "rollback" {
		t.Fatalf("unexpected rollback args: %v", got)
	}
}
