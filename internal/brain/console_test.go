package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func setConsoleSummary(t *testing.T, root string, trust TrustLevel, slug, summary string) {
	t.Helper()
	directory := CuratedDir
	if trust == TrustWorking {
		directory = WorkingDir
	}
	path := filepath.Join(root, directory, slug+".md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(body), "\n---\n\n", "\nconsole_summary: "+summary+"\n---\n\n", 1)
	if updated == string(body) {
		t.Fatal("frontmatter closing delimiter not found")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSnapshotReturnsStableSafeRealGraph(t *testing.T) {
	store, root := openIndexedStore(t)
	seedIndexSources(t, root)
	setConsoleSummary(t, root, TrustCurated, "release-gates", "Verified release controls.")
	setConsoleSummary(t, root, TrustCurated, "deployment-rollback", "Verified rollback controls.")
	setConsoleSummary(t, root, TrustWorking, "console-hypothesis", "Working note awaiting owner review.")
	status, err := store.Reindex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, again) {
		t.Fatalf("snapshot identity changed without state change\nfirst=%+v\nsecond=%+v", snapshot, again)
	}
	if snapshot.Status != status || len(snapshot.Nodes) != 3 || len(snapshot.Edges) != 2 || !snapshot.GraphTruncated {
		t.Fatalf("snapshot=%+v status=%+v", snapshot, status)
	}
	for _, node := range snapshot.Nodes {
		if !strings.HasPrefix(node.ID, consoleNodeIDPrefix) || len(node.ID) != len(consoleNodeIDPrefix)+24 || node.Title == "" || node.Summary == "" || node.Degree < 1 || (node.Trust != TrustCurated && node.Trust != TrustWorking) {
			t.Fatalf("unsafe or incomplete node=%+v", node)
		}
		for _, slug := range []string{"release-gates", "deployment-rollback", "console-hypothesis"} {
			if strings.Contains(node.ID, slug) {
				t.Fatalf("opaque id leaked slug: node=%+v", node)
			}
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(encoded))
	for _, private := range []string{
		`"slug"`, `"body"`, `"provenance"`, `"author"`, `"path"`,
		"release-gates", "deployment-rollback", "console-hypothesis",
		"staticcheck race codeql", "tagged commit and health smoke", "missing-reference",
	} {
		if strings.Contains(lower, private) {
			t.Fatalf("console snapshot leaked %q: %s", private, encoded)
		}
	}
}

func TestConsoleSnapshotUsesFallbacksForSecretTitleAndSummary(t *testing.T) {
	store, root := openIndexedStore(t)
	source := validSource("secret-metadata", TrustCurated, AuthorOwner)
	source = strings.Replace(source, "title: Deployment rollback rule", `title: "token = abcdefghijklmnop"`, 1)
	source = strings.Replace(source, "\n---\n\n", "\nconsole_summary: \"password=abcdefghijklmnop\"\n---\n\n", 1)
	path := filepath.Join(root, CuratedDir, "secret-metadata.md")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Title != "Brain note" || snapshot.Nodes[0].Summary != "Console summary withheld." {
		t.Fatalf("node=%+v", snapshot.Nodes)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"abcdefghijklmnop", "token =", "password="} {
		if strings.Contains(strings.ToLower(string(encoded)), secret) {
			t.Fatalf("secret metadata leaked %q: %s", secret, encoded)
		}
	}
}

func TestConsoleMetadataMigrationAndReindexAreIdempotent(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "migration-note", AuthorOwner, "Migration note", "Private body remains private.")
	setConsoleSummary(t, root, TrustCurated, "migration-note", "Explicit safe summary.")
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.withIndex(func(index *Index) error {
		_, err := index.db.Exec(`DROP TABLE console_metadata`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := store.Reindex(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.withIndex(func(index *Index) error {
		var count int
		var title, summary string
		if err := index.db.QueryRow(`SELECT COUNT(*) FROM console_metadata`).Scan(&count); err != nil {
			return err
		}
		if err := index.db.QueryRow(`SELECT title,console_summary FROM console_metadata WHERE slug='migration-note'`).Scan(&title, &summary); err != nil {
			return err
		}
		if count != 1 || title != "Migration note" || summary != "Explicit safe summary." {
			return fmt.Errorf("count=%d title=%q summary=%q", count, title, summary)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSnapshotBoundsNodeGraph(t *testing.T) {
	store, _ := openIndexedStore(t)
	if err := store.withIndex(func(index *Index) error {
		transaction, err := index.db.Begin()
		if err != nil {
			return err
		}
		defer transaction.Rollback()
		for number := 0; number <= MaxConsoleGraphNodes; number++ {
			slug := fmt.Sprintf("node-%04d", number)
			if _, err := transaction.Exec(`INSERT INTO notes(slug,trust,title,note_type,author,created,updated,provenance,review_by,expired,body,source_bytes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				slug, TrustCurated, "Bounded note", TypeFact, AuthorOwner, "2026-07-13T22:00:00Z", "2026-07-13T22:30:00Z", "test", "", 0, "private", 1); err != nil {
				return err
			}
			if _, err := transaction.Exec(`INSERT INTO console_metadata(slug,title,console_label,console_summary) VALUES(?,?,?,?)`, slug, "Bounded note", "Bounded note", "Safe summary."); err != nil {
				return err
			}
		}
		return transaction.Commit()
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != MaxConsoleGraphNodes || !snapshot.GraphTruncated {
		t.Fatalf("nodes=%d truncated=%v", len(snapshot.Nodes), snapshot.GraphTruncated)
	}
}

func TestConsoleSnapshotRequiresContextIdentityAndOpenIndex(t *testing.T) {
	store, _ := openIndexedStore(t)
	var missingContext context.Context
	if _, err := store.ConsoleSnapshot(missingContext); err == nil {
		t.Fatal("nil context accepted")
	}
	store.mu.Lock()
	store.consoleKey = nil
	store.mu.Unlock()
	if _, err := store.ConsoleSnapshot(context.Background()); err == nil {
		t.Fatal("missing identity accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsoleSnapshot(context.Background()); err == nil {
		t.Fatal("closed index accepted")
	}
	var nilStore *Store
	if _, err := nilStore.ConsoleSnapshot(context.Background()); err == nil {
		t.Fatal("nil store accepted")
	}
}
