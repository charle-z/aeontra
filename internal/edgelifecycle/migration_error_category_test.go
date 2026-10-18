//go:build !windows

package edgelifecycle

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMigrationErrorExposesOnlyStableFilesystemCategory(t *testing.T) {
	cases := map[error]string{
		unix.EXDEV:  "cross_device",
		unix.EPERM:  "permission",
		unix.EINVAL: "unsupported",
		unix.EEXIST: "destination_conflict",
		unix.ENOENT: "path_missing",
		unix.EBUSY:  "busy",
	}
	for failure, category := range cases {
		t.Run(category, func(t *testing.T) {
			err := migrationErr(MigrationErrorRenameFailed, errors.Join(errors.New("private/path/that/must/not/escape"), failure))
			message := err.Error()
			if message != "edge state migration failed: rename_failed ("+category+")" {
				t.Fatalf("message=%q", message)
			}
			if strings.Contains(message, "private/path") || strings.Contains(message, failure.Error()) {
				t.Fatalf("raw error escaped: %q", message)
			}
		})
	}
}

func TestMigrationErrorWithoutKnownFilesystemFailureKeepsStableCodeOnly(t *testing.T) {
	err := migrationErr(MigrationErrorVerificationFailed, errors.New("private detail"))
	if got := err.Error(); got != "edge state migration failed: verification_failed" {
		t.Fatalf("message=%q", got)
	}
}

func TestPortableExchangeFailureRemovesPlaceholderAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	source := root + "/legacy"
	destination := root + "/preferred"
	if err := unix.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	err := portableDirectoryRenameNoReplaceWith(source, destination, func(_, _ string) error { return unix.EOPNOTSUPP })
	if !errors.Is(err, unix.EOPNOTSUPP) {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(destination); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("placeholder remained: %v", statErr)
	}
	if _, statErr := os.Stat(source); statErr != nil {
		t.Fatalf("source changed: %v", statErr)
	}
}
