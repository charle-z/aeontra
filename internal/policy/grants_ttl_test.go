package policy

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestAccessGrantsRejectsOutOfBoundsTTLAtPolicyBoundary(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")
	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, ttl := range []time.Duration{-time.Second, 500 * time.Millisecond, time.Hour + time.Nanosecond} {
		if _, err := p.ApproveReadAccess(req.ID, false, ttl); !errors.Is(err, ErrAccessGrantTTL) {
			t.Errorf("ApproveReadAccess ttl=%s error=%v, want ErrAccessGrantTTL", ttl, err)
		}
	}

	// An invalid TTL must not consume the pending request; the local human can retry.
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatalf("valid retry failed: %v", err)
	}
	if _, err := p.ConsumeReadGrant(req.ID, secret, false); err != nil {
		t.Fatalf("valid retried grant failed: %v", err)
	}
}

func TestAccessGrantsZeroTTLUsesFiveMinuteDefault(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")
	fixed := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	p.grants.now = func() time.Time { return fixed }

	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := p.ApproveReadAccess(req.ID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := fixed.Add(5 * time.Minute); !decision.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %s, want %s", decision.ExpiresAt, want)
	}
}
