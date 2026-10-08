package edgeclient

import "context"

// StatusWithAncestors observes exact commit ancestry using fixed read-only Git
// commands. HEAD/cleanliness/identity are checked again after all probes, so no
// receipt may combine ancestry from one HEAD with evidence from another.
func (m *ProjectWorktreeManager) StatusWithAncestors(ctx context.Context, id string, ancestors []string) (ProjectWorktreeSnapshot, error) {
	if len(ancestors) == 0 {
		return m.Status(ctx, id)
	}
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(id) || len(ancestors) > 4 {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeInvalid
	}
	seen := make(map[string]bool)
	for _, head := range ancestors {
		if !projectWorktreeCommitPattern.MatchString(head) || seen[head] {
			return ProjectWorktreeSnapshot{}, ErrProjectWorktreeInvalid
		}
		seen[head] = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, found, err := m.byID(id)
	if err != nil || !found {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeNotFound
	}
	if err := m.revalidate(ctx, snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if err := m.collectEvidence(ctx, &snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if !snapshot.Clean {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeDirty
	}
	head := snapshot.HeadCommit
	for _, ancestor := range ancestors {
		if _, err := m.runner.Run(ctx, snapshot.path, []string{"merge-base", "--is-ancestor", ancestor, head}, m.credential); err != nil {
			return ProjectWorktreeSnapshot{}, ErrProjectWorktreeUnavailable
		}
	}
	if err := m.revalidate(ctx, snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if err := m.collectEvidence(ctx, &snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if snapshot.HeadCommit != head || !snapshot.Clean {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeBaseChanged
	}
	return snapshot, nil
}
