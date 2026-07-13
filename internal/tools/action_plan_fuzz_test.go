package tools

import (
	"strings"
	"testing"
	"time"
)

func FuzzActionPlanSingleUseAndOperationBinding(f *testing.F) {
	seeds := []struct {
		operation string
		consume   string
		ttlNanos  int64
	}{
		{"deploy", "deploy", int64(time.Minute)},
		{"deploy", "publish", int64(time.Minute)},
		{"", "", int64(time.Second)},
		{"deploy", "deploy", -1},
	}
	for _, seed := range seeds {
		f.Add(seed.operation, seed.consume, seed.ttlNanos)
	}
	f.Fuzz(func(t *testing.T, operation, consumeOperation string, ttlNanos int64) {
		// Keep time arithmetic bounded while retaining expired/live cases.
		ttl := time.Duration(ttlNanos % int64(time.Hour))
		store := NewActionPlanStore(nil)
		fixed := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
		store.now = func() time.Time { return fixed }
		plan, err := store.CreateTTL(operation, map[string]string{"key": "value"}, ttl)
		if err != nil {
			t.Fatal(err)
		}

		_, firstErr := store.Consume(plan.ID, consumeOperation)
		firstShouldSucceed := consumeOperation == operation && ttl > 0
		if firstShouldSucceed && firstErr != nil {
			t.Fatalf("bound live plan rejected: %v", firstErr)
		}
		if !firstShouldSucceed && firstErr == nil {
			t.Fatalf("mismatched or expired plan unexpectedly succeeded")
		}

		_, secondErr := store.Consume(plan.ID, operation)
		switch {
		case firstShouldSucceed:
			if secondErr == nil || !strings.Contains(secondErr.Error(), "already used") {
				t.Fatalf("replay error = %v, want already used", secondErr)
			}
		case consumeOperation != operation && ttl > 0:
			if secondErr != nil {
				t.Fatalf("operation mismatch consumed the plan: %v", secondErr)
			}
		default:
			if secondErr == nil || !strings.Contains(secondErr.Error(), "already used") {
				t.Fatalf("consumed expired plan error = %v, want already used", secondErr)
			}
		}
	})
}
