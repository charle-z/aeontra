package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseNumericUser(t *testing.T) {
	uid, gid, err := parseNumericUser("10001:10002")
	if err != nil || uid != 10001 || gid != 10002 {
		t.Fatalf("parseNumericUser = %d:%d, %v", uid, gid, err)
	}
	for _, bad := range []string{"", "root", "10001", "-1:10001", "10001:-1", "10001:group", "1:2:3"} {
		if _, _, err := parseNumericUser(bad); err == nil {
			t.Fatalf("accepted invalid user %q", bad)
		}
	}
}

func TestPrepareStoreCreatesWritableCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("validation runner is Linux-only")
	}
	store := filepath.Join(t.TempDir(), "pnpm-store")
	user := fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid())
	if err := prepareStore(store, user); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store, "corepack", "v1"), 0o755); err != nil {
		t.Fatalf("prepared store is not writable: %v", err)
	}
}
