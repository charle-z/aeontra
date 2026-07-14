package brain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextDigestIsBoundedCuratedFirstAndOmitsBodies(t *testing.T) {
	store, root := openIndexedStore(t)
	writeSourceForIndex(t, root, TrustCurated, "owner-reference", AuthorOwner, "Owner reference", "PRIVATE BODY MUST NOT APPEAR.")
	writeSourceForIndex(t, root, TrustWorking, "active-working", "agent:chatgpt", "Active working", "ACTIVE BODY MUST NOT APPEAR.")
	expired := validSource("expired-working", TrustWorking, "agent:claude")
	expired = strings.Replace(expired, "review_by: 2026-08-13", "review_by: 2026-07-12", 1)
	expired = strings.Replace(expired, "Deployment rollback rule", "Expired working", 1)
	if err := writePrivateTestFile(filepath.Join(root, WorkingDir, "expired-working.md"), []byte(expired)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}

	digest, err := store.ContextDigest(context.Background(), 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) > MaxContextBytes {
		t.Fatalf("digest bytes=%d", len(digest))
	}
	if strings.Contains(digest, "PRIVATE BODY") || strings.Contains(digest, "ACTIVE BODY") {
		t.Fatalf("digest leaked body: %q", digest)
	}
	if strings.Contains(digest, "expired-working") {
		t.Fatalf("digest included expired working note: %q", digest)
	}
	owner := strings.Index(digest, "owner-reference")
	working := strings.Index(digest, "active-working")
	if owner < 0 || working < 0 || owner > working {
		t.Fatalf("curated note was not first: %q", digest)
	}
	for _, required := range []string{"curated", "fact", "owner", "working", "agent:chatgpt", "2026-08-13"} {
		if !strings.Contains(digest, required) {
			t.Errorf("digest does not contain %q: %q", required, digest)
		}
	}
}

func TestContextDigestValidatesLimitAndLifecycle(t *testing.T) {
	store, _ := openIndexedStore(t)
	if _, err := store.ContextDigest(context.Background(), -1); err == nil {
		t.Fatal("negative limit unexpectedly succeeded")
	}
	if _, err := store.ContextDigest(context.Background(), MaxContextNotes+1); err == nil {
		t.Fatal("oversized limit unexpectedly succeeded")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ContextDigest(context.Background(), 1); err == nil {
		t.Fatal("closed index unexpectedly returned context")
	}
}

func writePrivateTestFile(path string, data []byte) error {
	return atomicWritePrivate(path, data)
}
