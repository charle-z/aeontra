package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIndexLifecycleRejectsInvalidStateAndIsIdempotent(t *testing.T) {
	var nilStore *Store
	if err := nilStore.OpenIndex(context.Background()); err == nil {
		t.Fatal("nil store opened an index")
	}
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil store close: %v", err)
	}

	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(nil); err == nil {
		t.Fatal("nil context opened an index")
	}
	if _, err := store.IndexStatus(context.Background()); err == nil {
		t.Fatal("status succeeded before index open")
	}
	if _, err := store.Search(context.Background(), "release gates", 5); err == nil {
		t.Fatal("search succeeded before index open")
	}
	if _, err := store.Reindex(nil); err == nil {
		t.Fatal("nil context reindex succeeded")
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatalf("second open should be idempotent: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}
	if _, err := store.IndexStatus(context.Background()); err == nil {
		t.Fatal("status succeeded after close")
	}
}

func TestOpenIndexRejectsUnsafeCachePermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, CacheDir, IndexFileName)
	if err := os.WriteFile(cache, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err == nil {
		t.Fatal("broad cache permissions unexpectedly accepted")
	}
}

func TestReindexRejectsUnexpectedSourceEntries(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "non markdown file",
			setup: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, CuratedDir, "unexpected.txt"), []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "nested directory",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(filepath.Join(root, WorkingDir, "nested"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "broad source permissions",
			setup: func(t *testing.T, root string) {
				path := filepath.Join(root, CuratedDir, "broad-note.md")
				if err := os.WriteFile(path, []byte(validSource("broad-note", TrustCurated, AuthorOwner)), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, root := openIndexedStore(t)
			testCase.setup(t, root)
			if _, err := store.Reindex(context.Background()); err == nil {
				t.Fatal("unsafe source entry unexpectedly indexed")
			}
		})
	}
}

func TestSearchAndBacklinksRejectInvalidInputWithoutEcho(t *testing.T) {
	store, _ := openIndexedStore(t)
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	queries := []string{
		"!!!",
		strings.Repeat("term ", MaxQueryTerms+1),
	}
	for _, query := range queries {
		if _, err := store.Search(context.Background(), query, 5); err == nil || strings.Contains(err.Error(), query) {
			t.Fatalf("query=%q err=%v", query, err)
		}
	}
	if _, err := store.Search(nil, secret, 5); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("nil context search error=%v", err)
	}
	if _, err := store.Backlinks(nil, "safe-note"); err == nil {
		t.Fatal("nil context backlink query succeeded")
	}
	if _, err := store.Backlinks(context.Background(), "../"+secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid backlink slug error=%v", err)
	}
}

func TestExcerptAndTruncationRespectUTF8Bounds(t *testing.T) {
	body := strings.Repeat("á", MaxExcerptBytes) + " target ending"
	excerpt := makeExcerpt(body, []string{"target"})
	if len(excerpt) > MaxExcerptBytes || !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt bytes=%d valid=%t", len(excerpt), utf8.ValidString(excerpt))
	}
	if got := truncateUTF8("áéí", 3); got != "á" {
		t.Fatalf("truncateUTF8=%q", got)
	}
	if got := truncateUTF8("value", 0); got != "" {
		t.Fatalf("zero bound returned %q", got)
	}
}
