package brain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeSourceForIndex(t *testing.T, root string, trust TrustLevel, slug, author, title, body string) {
	t.Helper()
	directory := CuratedDir
	if trust == TrustWorking {
		directory = WorkingDir
	}
	source := validSource(slug, trust, author)
	source = strings.Replace(source, "Deployment rollback rule", title, 1)
	source = strings.Replace(source, "Use the verified rollback procedure. See [[release-gates]].", body, 1)
	path := filepath.Join(root, directory, slug+".md")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openIndexedStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "state", "brain", "console-node.key")
	if err := store.ConfigureConsoleIdentity(identityPath); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return store, root
}

func seedIndexSources(t *testing.T, root string) {
	t.Helper()
	writeSourceForIndex(t, root, TrustCurated, "release-gates", AuthorOwner, "Release gates", "Staticcheck race CodeQL dependency review and container gate.")
	writeSourceForIndex(t, root, TrustCurated, "deployment-rollback", AuthorOwner, "Deployment rollback", "Rollback uses a tagged commit and health smoke. See [[release-gates]].")
	writeSourceForIndex(t, root, TrustWorking, "console-hypothesis", "agent:chatgpt", "Console hypothesis", "The console remains presentation only. Compare [[release-gates]] and [[missing-reference]].")
}

func TestOpenIndexCreatesPrivateFTS5Cache(t *testing.T) {
	store, root := openIndexedStore(t)
	status, err := store.IndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.SchemaVersion != IndexSchemaVersion || status.NoteCount != 0 {
		t.Fatalf("status=%+v", status)
	}
	path := filepath.Join(root, CacheDir, IndexFileName)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%v", info.Mode())
	}
}

func TestOpenIndexRejectsSymlinkCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.db")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, CacheDir, IndexFileName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := store.OpenIndex(context.Background()); err == nil {
		t.Fatal("symlink cache unexpectedly accepted")
	}
}

func TestReindexSearchBacklinksAndBrokenLinks(t *testing.T) {
	store, root := openIndexedStore(t)
	seedIndexSources(t, root)
	status, err := store.Reindex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.NoteCount != 3 || status.LinkCount != 3 || status.BrokenLinkCount != 1 || status.SourceBytes == 0 {
		t.Fatalf("status=%+v", status)
	}
	results, err := store.Search(context.Background(), "staticcheck race", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Slug != "release-gates" || results[0].Trust != TrustCurated {
		t.Fatalf("results=%+v", results)
	}
	if len(results[0].Excerpt) > MaxExcerptBytes {
		t.Fatalf("excerpt bytes=%d", len(results[0].Excerpt))
	}
	backlinks, err := store.Backlinks(context.Background(), "release-gates")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"console-hypothesis", "deployment-rollback"}
	if !reflect.DeepEqual(backlinks, want) {
		t.Fatalf("backlinks=%v want=%v", backlinks, want)
	}
}

func TestSearchTreatsQueryAsPlainTextNotFTSSyntax(t *testing.T) {
	store, root := openIndexedStore(t)
	seedIndexSources(t, root)
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"release OR nonexistent",
		`title:release`,
		`"release gates" OR missing`,
	} {
		results, err := store.Search(context.Background(), query, 5)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if len(results) != 0 {
			t.Fatalf("query %q interpreted as FTS syntax: %+v", query, results)
		}
	}
}

func TestSearchDoesNotTreatAsteriskAsPrefixOperator(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "plural-only", AuthorOwner, "Plural only", "Releases deployments safely.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(context.Background(), "release*", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("asterisk acted as prefix syntax: %+v", results)
	}
}

func TestSearchAndBacklinkBounds(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "target-note", AuthorOwner, "Target note", "Target body.")
	for index := 0; index < MaxBacklinks+8; index++ {
		slug := "link-note-" + alphaSlug(index)
		writeSourceForIndex(t, root, TrustCurated, slug, AuthorOwner, "Link note "+slug, strings.Repeat("bounded search text ", 40)+"[[target-note]].")
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	backlinks, err := store.Backlinks(context.Background(), "target-note")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != MaxBacklinks {
		t.Fatalf("backlinks=%d want=%d", len(backlinks), MaxBacklinks)
	}
	results, err := store.Search(context.Background(), "bounded search text", MaxTopK)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > MaxTopK {
		t.Fatalf("results=%d", len(results))
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxSearchResponseBytes {
		t.Fatalf("encoded results=%d > %d", len(encoded), MaxSearchResponseBytes)
	}
	for _, result := range results {
		if len(result.Excerpt) > MaxExcerptBytes {
			t.Fatalf("excerpt=%d", len(result.Excerpt))
		}
	}
}

func TestSearchRejectsInvalidBoundsWithoutEchoingQuery(t *testing.T) {
	store, _ := openIndexedStore(t)
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	for _, testCase := range []struct {
		query string
		topK  int
	}{
		{"", 5},
		{strings.Repeat("x", MaxQueryBytes+1) + secret, 5},
		{"valid", -1},
		{"valid", MaxTopK + 1},
	} {
		if _, err := store.Search(context.Background(), testCase.query, testCase.topK); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("query=%q topK=%d err=%v", testCase.query, testCase.topK, err)
		}
	}
}

func TestManualSecretsAreAbsentFromIndexAndResults(t *testing.T) {
	store, root := openIndexedStore(t)
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	writeSourceForIndex(t, root, TrustCurated, "manual-secret", AuthorOwner, "Manual secret reference", "Historical value "+secret+" must not enter the cache.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(context.Background(), secret, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("secret query returned %+v", results)
	}
	cache, err := os.ReadFile(filepath.Join(root, CacheDir, IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cache), secret) {
		t.Fatal("raw secret exists in SQLite cache")
	}
	redacted, err := store.Search(context.Background(), "redacted secret", 5)
	if err != nil || len(redacted) != 1 || strings.Contains(redacted[0].Excerpt, secret) {
		t.Fatalf("redacted results=%+v err=%v", redacted, err)
	}
}

func TestReindexFailureLeavesPriorSnapshot(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "stable-note", AuthorOwner, "Stable note", "Stable searchable content.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, CuratedDir, "broken-note.md"), []byte("not frontmatter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err == nil {
		t.Fatal("malformed source unexpectedly reindexed")
	}
	results, err := store.Search(context.Background(), "stable searchable", 5)
	if err != nil || len(results) != 1 || results[0].Slug != "stable-note" {
		t.Fatalf("prior snapshot lost: %+v err=%v", results, err)
	}
}

func TestDeleteCacheAndReindexRestoresEquivalentResults(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedIndexSources(t, root)
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := store.Search(context.Background(), "rollback tagged commit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, CacheDir, IndexFileName)); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := store.Search(context.Background(), "rollback tagged commit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("results differ\nbefore=%+v\nafter=%+v", before, after)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexBoundsValidation(t *testing.T) {
	for _, testCase := range []struct {
		notes int
		bytes int64
		ok    bool
	}{
		{MaxIndexedNotes, MaxAggregateSourceBytes, true},
		{MaxIndexedNotes + 1, 1, false},
		{1, MaxAggregateSourceBytes + 1, false},
	} {
		err := validateIndexBounds(testCase.notes, testCase.bytes)
		if (err == nil) != testCase.ok {
			t.Fatalf("notes=%d bytes=%d err=%v", testCase.notes, testCase.bytes, err)
		}
	}
}

func alphaSlug(index int) string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	if index < len(letters) {
		return string(letters[index])
	}
	return string(letters[index/len(letters)-1]) + string(letters[index%len(letters)])
}

func TestWorkingWriteUpdatesIndexAndBacklinksIncrementally(t *testing.T) {
	now := fixedNow
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStoreWithClock(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writeSourceForIndex(t, root, TrustCurated, "release-gates", AuthorOwner, "Release gates", "Staticcheck race CodeQL gate.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	draft := agentDraft("incremental-note", "agent:chatgpt")
	draft.Title = "Incremental memory"
	draft.Body = "Incremental searchable content linking [[release-gates]]."
	if _, err := store.WriteAgent(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(context.Background(), "incremental searchable", 5)
	if err != nil || len(results) != 1 || results[0].Slug != "incremental-note" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	backlinks, err := store.Backlinks(context.Background(), "release-gates")
	if err != nil || !reflect.DeepEqual(backlinks, []string{"incremental-note"}) {
		t.Fatalf("backlinks=%v err=%v", backlinks, err)
	}

	now = now.Add(time.Hour)
	draft.Body = "Updated incremental content with no links."
	if _, err := store.WriteAgent(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	backlinks, err = store.Backlinks(context.Background(), "release-gates")
	if err != nil || len(backlinks) != 0 {
		t.Fatalf("stale backlinks=%v err=%v", backlinks, err)
	}
}

func TestGitFailureRestoresIncrementalIndexState(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "release-gates", AuthorOwner, "Release gates", "Stable release content.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.git.runner = &failGitRunner{delegate: store.git.runner, command: "commit-tree"}
	draft := agentDraft("failed-index-note", "agent:chatgpt")
	draft.Body = "This searchable content must roll back."
	if _, err := store.WriteAgent(context.Background(), draft); err == nil {
		t.Fatal("injected Git failure unexpectedly succeeded")
	}
	results, err := store.Search(context.Background(), "searchable content rollback", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("failed note remains indexed: %+v", results)
	}
	status, err := store.IndexStatus(context.Background())
	if err != nil || status.NoteCount != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
