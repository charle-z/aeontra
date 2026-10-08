package policy

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestPendingAccessRequestExpiresBeforeApproval(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	p.grants.now = func() time.Time { return now }

	secret := filepath.Join(root, ".env")
	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(pendingRequestTTL + time.Nanosecond)
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); !errors.Is(err, ErrAccessRequestExpired) {
		t.Fatalf("error = %v, want ErrAccessRequestExpired", err)
	}
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); !errors.Is(err, ErrAccessGrantInvalid) {
		t.Fatalf("second approval error = %v, want ErrAccessGrantInvalid", err)
	}
}

func TestPendingAccessRequestLimitIsBoundedAndExpiredEntriesArePruned(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	p.grants.now = func() time.Time { return now }
	counter := 0
	p.grants.newID = func() (string, error) {
		counter++
		return fmt.Sprintf("id-%03d", counter), nil
	}

	for i := 0; i < maxPendingAccessRequests; i++ {
		path := filepath.Join(root, fmt.Sprintf(".env.%03d", i))
		if _, err := p.RequestReadAccess(path, false); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if _, err := p.RequestReadAccess(filepath.Join(root, ".env.overflow"), false); !errors.Is(err, ErrAccessRequestLimit) {
		t.Fatalf("overflow error = %v, want ErrAccessRequestLimit", err)
	}

	now = now.Add(pendingRequestTTL + time.Nanosecond)
	if _, err := p.RequestReadAccess(filepath.Join(root, ".env.after-prune"), false); err != nil {
		t.Fatalf("request after pruning expired entries: %v", err)
	}
}

func TestDuplicatePendingAccessRequestsHaveDistinctRequestIDs(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")

	first, err := p.RequestReadAccess(secret, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.RequestReadAccess(secret, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("separate callers must not share request id %q", first.ID)
	}
	if _, err := p.RequestReadAccess(secret, false); err != nil {
		t.Fatal(err)
	}
	if got := len(p.grants.requests); got != 3 {
		t.Fatalf("pending request count = %d, want one request per caller attempt", got)
	}
}
