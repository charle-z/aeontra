package brain

import (
	"context"
	"strings"
	"testing"
)

func TestConsoleLabelIsExplicitSingleLineUnicodeMetadata(t *testing.T) {
	maximum := strings.Repeat("界", MaxConsoleLabelRunes)
	source := strings.Replace(validSource("bounded-label", TrustCurated, AuthorOwner), "\n---\n\n", "\nconsole_label: "+maximum+"\n---\n\n", 1)
	note, err := ParseNote([]byte(source), "bounded-label", TrustCurated, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if note.Metadata.ConsoleLabel != maximum {
		t.Fatalf("label=%q", note.Metadata.ConsoleLabel)
	}

	oversized := strings.Replace(source, maximum, maximum+"界", 1)
	if _, err := ParseNote([]byte(oversized), "bounded-label", TrustCurated, fixedNow); err == nil {
		t.Fatal("console_label longer than 32 Unicode characters accepted")
	}
	multiline := strings.Replace(source, "console_label: "+maximum, "console_label: |\n  line one\n  line two", 1)
	if _, err := ParseNote([]byte(multiline), "bounded-label", TrustCurated, fixedNow); err == nil {
		t.Fatal("multiline console_label accepted")
	}
}

func TestConsoleLabelFallbackUsesOnlySafeTitleWords(t *testing.T) {
	note := Note{Metadata: Metadata{
		Title: "P11.2 Remote OpenCode relay production closure",
	}}
	label, title, summary := safeConsoleMetadata(note)
	if label != "P11.2 Remote OpenCode relay" {
		t.Fatalf("fallback label=%q", label)
	}
	if title != note.Metadata.Title {
		t.Fatalf("title=%q", title)
	}
	if summary != "No curated summary available." {
		t.Fatalf("summary=%q", summary)
	}
	if strings.Contains(label, "...") || strings.Contains(label, "…") {
		t.Fatalf("fallback added an unhelpful ellipsis: %q", label)
	}
}

func TestConsoleLabelRedactionFallsBackWithoutPrivateFields(t *testing.T) {
	note := Note{Metadata: Metadata{
		Slug:         "private-slug-must-not-appear",
		Title:        "OpenCode provider baseline",
		ConsoleLabel: "token=***REDACTED-SECRET***",
		Provenance:   "private provenance",
	}, Body: "private body"}
	label, _, _ := safeConsoleMetadata(note)
	if label != "OpenCode provider baseline" {
		t.Fatalf("redacted label fallback=%q", label)
	}
	for _, forbidden := range []string{note.Metadata.Slug, note.Metadata.Provenance, note.Body, "token="} {
		if strings.Contains(label, forbidden) {
			t.Fatalf("label leaked %q: %q", forbidden, label)
		}
	}
}

func TestAgentUpdatePreservesOwnerSuppliedConsoleLabel(t *testing.T) {
	source := strings.Replace(validSource("working-label", TrustWorking, "agent:chatgpt"), "\n---\n\n", "\nconsole_label: P11.2 Relay\n---\n\n", 1)
	existing, err := ParseNote([]byte(source), "working-label", TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentDraft("working-label", "agent:chatgpt")
	draft.Title = "Updated private title"
	draft.Body = "Updated private body."
	updated, err := BuildAgentNote(draft, &existing, fixedNow.Add(1))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.ConsoleLabel != "P11.2 Relay" {
		t.Fatalf("label changed to %q", updated.Metadata.ConsoleLabel)
	}
}

func TestConsoleLabelSchemaMigrationAndReindexAreIdempotent(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "migration-label", AuthorOwner, "P11.1 Baseline Production Closure", "Private body remains private.")
	if err := store.withIndex(func(index *Index) error {
		if _, err := index.db.Exec(`DROP TABLE console_metadata`); err != nil {
			return err
		}
		_, err := index.db.Exec(`CREATE TABLE console_metadata (
			slug TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			console_summary TEXT NOT NULL,
			FOREIGN KEY (slug) REFERENCES notes(slug) ON DELETE CASCADE
		) STRICT`)
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
		var label string
		return index.db.QueryRow(`SELECT console_label FROM console_metadata WHERE slug='migration-label'`).Scan(&label)
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ConsoleLabel != "P11.1 Baseline Production" {
		t.Fatalf("nodes=%+v", snapshot.Nodes)
	}
}
