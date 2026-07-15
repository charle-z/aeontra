package brain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConsoleSnapshotReturnsOpaqueRealGraph(t *testing.T) {
	store, root := openIndexedStore(t)
	seedIndexSources(t, root)
	status, err := store.Reindex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status || len(snapshot.Nodes) != 3 || len(snapshot.Edges) != 2 || !snapshot.GraphTruncated {
		t.Fatalf("snapshot=%+v status=%+v", snapshot, status)
	}
	for index, node := range snapshot.Nodes {
		want := "n000" + string(rune('1'+index))
		if node.ID != want || node.Degree < 1 || (node.Trust != TrustCurated && node.Trust != TrustWorking) {
			t.Fatalf("node=%+v want id=%s", node, want)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(encoded))
	for _, private := range []string{"release-gates", "deployment-rollback", "console-hypothesis", "title", "body", "provenance", "author", "slug"} {
		if strings.Contains(lower, private) {
			t.Fatalf("console snapshot leaked %q: %s", private, encoded)
		}
	}
}

func TestConsoleSnapshotRequiresContextAndOpenIndex(t *testing.T) {
	store, _ := openIndexedStore(t)
	if _, err := store.ConsoleSnapshot(nil); err == nil {
		t.Fatal("nil context accepted")
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
