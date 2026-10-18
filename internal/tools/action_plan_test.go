package tools

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

func TestActionPlansAreUnpredictableTTLBoundAndSingleUse(t *testing.T) {
	var auditOut bytes.Buffer
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := NewActionPlanStore(audit.New(&auditOut))
	store.now = func() time.Time { return now }
	store.ttl = time.Minute

	first, err := store.Create("repo-fast-forward", map[string]string{"repo": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("repo-fast-forward", map[string]string{"repo": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || len(first.ID) < 32 {
		t.Fatalf("plan ids must be long and unpredictable: %q %q", first.ID, second.ID)
	}
	if !first.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiry = %s", first.ExpiresAt)
	}
	if _, err := store.Consume(first.ID, "repo-fast-forward"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(first.ID, "repo-fast-forward"); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay must fail explicitly, got %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := store.Consume(second.ID, "repo-fast-forward"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired plan must fail explicitly, got %v", err)
	}
	log := auditOut.String()
	for _, event := range []string{"plan-created", "plan-executed", "plan-rejected"} {
		if !strings.Contains(log, event) {
			t.Errorf("audit missing %s: %s", event, log)
		}
	}
}
