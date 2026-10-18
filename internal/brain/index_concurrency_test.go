package brain

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSearchWriteAndReindexRemainConsistent(t *testing.T) {
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

	var wg sync.WaitGroup
	errorsOut := make(chan error, 40)
	for index := 0; index < 12; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 10; iteration++ {
				results, err := store.Search(context.Background(), "release gates", 5)
				if err != nil {
					errorsOut <- err
					return
				}
				if len(results) == 0 {
					errorsOut <- fmt.Errorf("search snapshot unexpectedly empty")
					return
				}
			}
		}()
	}
	for index := 0; index < 6; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			draft := agentDraft("indexed-concurrent-"+alphaSlug(index), "agent:chatgpt")
			draft.Body = "Concurrent searchable memory linked to [[release-gates]]."
			if _, err := store.WriteAgent(context.Background(), draft); err != nil {
				errorsOut <- err
			}
		}(index)
	}
	for index := 0; index < 3; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Reindex(context.Background()); err != nil {
				errorsOut <- err
			}
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}

	status, err := store.IndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.NoteCount != 7 || status.BrokenLinkCount != 0 {
		t.Fatalf("status=%+v", status)
	}
	results, err := store.Search(context.Background(), "concurrent searchable", MaxTopK)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 6 {
		t.Fatalf("concurrent results=%d", len(results))
	}
	backlinks, err := store.Backlinks(context.Background(), "release-gates")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 6 {
		t.Fatalf("backlinks=%d", len(backlinks))
	}
}

func TestCloseWaitsForReadersAndReopenWorks(t *testing.T) {
	store, root := openIndexedStore(t)
	seedIndexSources(t, root)
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errorsOut := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Search(context.Background(), "release gates", 5)
			errorsOut <- err
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(context.Background(), "release gates", 5)
	if err != nil || len(results) == 0 {
		t.Fatalf("reopened results=%+v err=%v", results, err)
	}
}
