package policy

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAccessGrantsConcurrentDuplicateRequestAndSingleConsume(t *testing.T) {
	grants := NewAccessGrants()
	var ids atomic.Int64
	grants.newID = func() (string, error) {
		return "request-" + time.Unix(ids.Add(1), 0).UTC().Format("150405"), nil
	}
	var notifications atomic.Int64
	grants.SetNotifier(func(AccessRequest) { notifications.Add(1) })

	const workers = 64
	requestIDs := make(chan string, workers)
	errorsCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request, err := grants.Request("/repo/.env", false)
			if err != nil {
				errorsCh <- err
				return
			}
			requestIDs <- request.ID
		}()
	}
	wg.Wait()
	close(requestIDs)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent request: %v", err)
	}

	var requestID string
	for id := range requestIDs {
		if requestID == "" {
			requestID = id
		}
		if id != requestID {
			t.Fatalf("duplicate request returned multiple ids: %q and %q", requestID, id)
		}
	}
	if got := notifications.Load(); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
	if got := len(grants.requests); got != 1 {
		t.Fatalf("pending requests = %d, want 1", got)
	}
	if _, err := grants.Approve(requestID, false, time.Minute); err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int64
	var used atomic.Int64
	errorsCh = make(chan error, workers)
	wg = sync.WaitGroup{}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := grants.Consume(requestID, "/repo/.env", false)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrAccessGrantUsed):
				used.Add(1)
			default:
				errorsCh <- err
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent consume: %v", err)
	}
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful consumes = %d, want 1", got)
	}
	if got := used.Load(); got != workers-1 {
		t.Fatalf("already-used responses = %d, want %d", got, workers-1)
	}
}
