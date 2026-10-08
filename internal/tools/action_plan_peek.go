package tools

import "fmt"

// Peek returns an immutable copy without authorizing or consuming the plan. It is
// used only to route an existing public operation to its stricter managed path.
func (s *ActionPlanStore) Peek(id string) (ActionPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[id]
	if !ok {
		return ActionPlan{}, fmt.Errorf("action plan not found")
	}
	return cloneActionPlan(plan), nil
}
