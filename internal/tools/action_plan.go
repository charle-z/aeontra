package tools

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const defaultActionPlanTTL = 5 * time.Minute

// ActionPlan is an immutable, short-lived authorization envelope for one exact
// normalized operation. Secret values must never be placed in Args.
type ActionPlan struct {
	ID        string
	Operation string
	Args      map[string]string
	CreatedAt time.Time
	ExpiresAt time.Time
	used      bool
}

// ActionPlanStore keeps plans in memory only. Plans are cryptographically named,
// TTL-bound, and single-use; restarting the daemon invalidates every outstanding plan.
type ActionPlanStore struct {
	mu    sync.Mutex
	plans map[string]*ActionPlan
	ttl   time.Duration
	now   func() time.Time
	log   *audit.Logger
}

func NewActionPlanStore(log *audit.Logger) *ActionPlanStore {
	return &ActionPlanStore{plans: map[string]*ActionPlan{}, ttl: defaultActionPlanTTL, now: time.Now, log: log}
}

func (s *ActionPlanStore) Create(operation string, args map[string]string) (ActionPlan, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ActionPlan{}, fmt.Errorf("creating action plan id: %w", err)
	}
	now := s.now().UTC()
	p := &ActionPlan{
		ID:        base64.RawURLEncoding.EncodeToString(random[:]),
		Operation: operation,
		Args:      clonePlanArgs(args),
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	s.mu.Lock()
	s.plans[p.ID] = p
	s.mu.Unlock()
	s.audit("plan-created", p, audit.Allow, nil)
	return cloneActionPlan(p), nil
}

func (s *ActionPlanStore) Consume(id, operation string) (ActionPlan, error) {
	s.mu.Lock()
	p, ok := s.plans[id]
	if !ok {
		s.mu.Unlock()
		err := fmt.Errorf("action plan not found")
		s.auditID("plan-rejected", operation, id, audit.Deny, err)
		return ActionPlan{}, err
	}
	if p.Operation != operation {
		s.mu.Unlock()
		err := fmt.Errorf("action plan operation mismatch")
		s.auditID("plan-rejected", operation, id, audit.Deny, err)
		return ActionPlan{}, err
	}
	if p.used {
		s.mu.Unlock()
		err := fmt.Errorf("action plan already used")
		s.audit("plan-rejected", p, audit.Deny, err)
		return ActionPlan{}, err
	}
	if !s.now().Before(p.ExpiresAt) {
		p.used = true
		s.mu.Unlock()
		err := fmt.Errorf("action plan expired at %s", p.ExpiresAt.Format(time.RFC3339))
		s.audit("plan-rejected", p, audit.Deny, err)
		return ActionPlan{}, err
	}
	p.used = true
	result := cloneActionPlan(p)
	s.mu.Unlock()
	s.audit("plan-executed", p, audit.Allow, nil)
	return result, nil
}

func cloneActionPlan(p *ActionPlan) ActionPlan {
	return ActionPlan{ID: p.ID, Operation: p.Operation, Args: clonePlanArgs(p.Args), CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt, used: p.used}
}

func clonePlanArgs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *ActionPlanStore) audit(event string, p *ActionPlan, decision audit.Decision, err error) {
	s.auditID(event, p.Operation, p.ID, decision, err)
}

func (s *ActionPlanStore) auditID(event, operation, id string, decision audit.Decision, err error) {
	if s.log == nil {
		return
	}
	msg := event + " operation=" + operation + " plan_id=" + id
	entry := audit.Entry{Tool: "action_plan", Decision: decision, Args: msg}
	if err != nil {
		entry.Error = err.Error()
	}
	_ = s.log.Log(entry)
}
