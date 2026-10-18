package resultstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPutRedactsAndReturnsCompactOpaqueMetadata(t *testing.T) {
	now := time.Date(2026, 7, 14, 18, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "results"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	meta, err := store.Put(Input{Status: StatusSuccess, Summary: "large tool result stored", ExitStatus: 0, Content: "before " + secret + " after", Stages: []StageInput{{Name: "run_tests", Status: StatusSuccess}}})
	if err != nil {
		t.Fatal(err)
	}
	if !opaqueRefPattern.MatchString(meta.ResultRef) || meta.OutputBytes != int64(len("before "+secret+" after")) || !meta.ExpiresAt.Equal(now.Add(SuccessTTL)) {
		t.Fatalf("metadata = %+v", meta)
	}
	fragment, err := store.Read(meta.ResultRef, 0, MaxFragmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fragment.Fragment, secret) || !strings.Contains(fragment.Fragment, "REDACTED") {
		t.Fatalf("content was not redacted: %q", fragment.Fragment)
	}
	if strings.Contains(fragment.JSON(), filepath.ToSlash(store.root)) {
		t.Fatalf("private root leaked: %s", fragment.JSON())
	}
}

func TestFailureTTLExactFindAndStageAreBounded(t *testing.T) {
	now := time.Date(2026, 7, 14, 18, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "results"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := strings.Repeat("prefix ", 4000) + "EXACT-NEEDLE" + strings.Repeat(" suffix", 4000)
	meta, err := store.Put(Input{Status: StatusFailure, Summary: "large failing result stored", ExitStatus: 1, Content: content, Stages: []StageInput{{Name: "build", Status: StatusFailure}}})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.ExpiresAt.Equal(now.Add(FailureTTL)) {
		t.Fatalf("expiry = %s", meta.ExpiresAt)
	}
	found, err := store.FindExact("EXACT-NEEDLE", 10)
	if err != nil || len(found) != 1 || found[0].ResultRef != meta.ResultRef {
		t.Fatalf("find=%+v err=%v", found, err)
	}
	read, err := store.Read(meta.ResultRef, 0, MaxFragmentBytes*2)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Fragment) > MaxFragmentBytes || read.NextOffset <= 0 {
		t.Fatalf("unbounded read = %+v", read)
	}
	stage, err := store.ReadStage(meta.ResultRef, 0, MaxFragmentBytes*2)
	if err != nil || len(stage.Fragment) > MaxFragmentBytes || stage.Stage.Name != "build" {
		t.Fatalf("stage=%+v err=%v", stage, err)
	}
}

func TestCleanupTTLQuotaAndSymlinkSafety(t *testing.T) {
	now := time.Date(2026, 7, 14, 18, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "results")
	store, err := Open(Config{Root: root, Now: func() time.Time { return now }, QuotaBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(Input{Status: StatusSuccess, Summary: "first", Content: strings.Repeat("a", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	second, err := store.Put(Input{Status: StatusSuccess, Summary: "second", Content: strings.Repeat("b", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(first.ResultRef, 0, 10); err == nil {
		t.Fatal("oldest result should be evicted to enforce quota")
	}
	if _, err := store.Read(second.ResultRef, 0, 10); err != nil {
		t.Fatal(err)
	}
	now = now.Add(SuccessTTL + time.Second)
	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(second.ResultRef, 0, 10); err == nil {
		t.Fatal("expired result should be removed")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("root info=%v err=%v", info, err)
	}

	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked-results")
	if err := os.Symlink(target, link); err == nil {
		if _, err := Open(Config{Root: link}); err == nil {
			t.Fatal("symlink result root must be rejected")
		}
	}
}

func TestInvalidReferencesQueriesAndStageIndexesFailClosed(t *testing.T) {
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "results")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, ref := range []string{"", "../escape", strings.Repeat("a", 200)} {
		if _, err := store.Read(ref, 0, 10); err == nil {
			t.Fatalf("reference %q accepted", ref)
		}
	}
	if _, err := store.FindExact("", 10); err == nil {
		t.Fatal("empty exact query accepted")
	}
	meta, err := store.Put(Input{Status: StatusSuccess, Summary: "ok", Content: "small"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadStage(meta.ResultRef, 2, 10); err == nil {
		t.Fatal("invalid stage index accepted")
	}
}
