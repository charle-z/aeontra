package tools

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

func TestActionPlanConcurrentConsumeSucceedsExactlyOnce(t *testing.T) {
	var auditOut bytes.Buffer
	store := NewActionPlanStore(audit.New(&auditOut))
	plan, err := store.Create("deploy", map[string]string{"app": "demo"})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 64
	var successes atomic.Int64
	var used atomic.Int64
	errorsCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Consume(plan.ID, "deploy")
			switch {
			case err == nil:
				successes.Add(1)
			case strings.Contains(err.Error(), "already used"):
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
	if got := strings.Count(auditOut.String(), "plan-executed"); got != 1 {
		t.Fatalf("executed audit events = %d, want 1", got)
	}
}
