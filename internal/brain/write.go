package brain

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// WriteAgent creates or updates one working note, then records exactly that source
// path in the local Brain Git repository. The operation is serialized and restores the
// prior source state if Git cannot publish the new commit.
func (s *Store) WriteAgent(ctx context.Context, draft AgentDraft) (Note, error) {
	if s == nil || s.jail == nil {
		return Note{}, errors.New("brain: store is unavailable")
	}
	if ctx == nil {
		return Note{}, errors.New("brain: context is required")
	}
	if ctx.Err() != nil {
		return Note{}, errors.New("brain: write request was cancelled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return Note{}, errors.New("brain: write request was cancelled")
	}
	if s.git == nil {
		return Note{}, errors.New("brain: local Git is not initialized")
	}
	if err := s.git.verifyMetadata(); err != nil {
		return Note{}, err
	}

	target, err := s.AgentTarget(TrustWorking, draft.Slug)
	if err != nil {
		return Note{}, err
	}
	curatedTarget, err := s.resolveTarget(TrustCurated, draft.Slug)
	if err != nil {
		return Note{}, err
	}
	curatedExists, err := sourceExists(curatedTarget)
	if err != nil {
		return Note{}, err
	}
	if curatedExists {
		return Note{}, errors.New("brain: slug already exists in curated memory")
	}

	prior, existed, existing, err := s.readWorkingRaw(target, draft.Slug)
	if err != nil {
		return Note{}, err
	}
	now := s.now()
	note, err := BuildAgentNote(draft, existing, now)
	if err != nil {
		return Note{}, err
	}
	encoded := RenderNote(note)
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	index := s.index
	if err := atomicWritePrivate(target, encoded); err != nil {
		return Note{}, err
	}

	criticalContext, cancel := context.WithTimeout(context.Background(), gitCriticalTimeout)
	defer cancel()
	if index != nil {
		if err := index.upsert(criticalContext, note, int64(len(encoded))); err != nil {
			if restoreErr := restoreSource(target, prior, existed); restoreErr != nil {
				return Note{}, errors.New("brain: SQLite update failed and source rollback failed")
			}
			return Note{}, errors.New("brain: SQLite incremental update failed")
		}
	}

	relative := filepath.ToSlash(filepath.Join(WorkingDir, draft.Slug+".md"))
	if _, err := s.git.commitPath(criticalContext, relative, draft.Author, "brain: write "+draft.Slug, now); err != nil {
		if restoreErr := restoreSource(target, prior, existed); restoreErr != nil {
			return Note{}, errors.New("brain: local Git failed and source rollback failed")
		}
		if index != nil {
			rollbackContext, rollbackCancel := context.WithTimeout(context.Background(), gitCriticalTimeout)
			defer rollbackCancel()
			var indexErr error
			if existed && existing != nil {
				indexErr = index.upsert(rollbackContext, *existing, int64(len(prior)))
			} else {
				indexErr = index.delete(rollbackContext, draft.Slug)
			}
			if indexErr != nil {
				return Note{}, errors.New("brain: local Git failed and index rollback failed")
			}
		}
		return Note{}, errors.New("brain: local Git write failed")
	}
	return note, nil
}

func (s *Store) readWorkingRaw(target, slug string) ([]byte, bool, *Note, error) {
	exists, err := sourceExists(target)
	if err != nil {
		return nil, false, nil, err
	}
	if !exists {
		return nil, false, nil, nil
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, false, nil, errors.New("brain: note source is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes {
		return nil, false, nil, errors.New("brain: note source could not be read safely")
	}
	note, err := ParseNote(data, slug, TrustWorking, s.now())
	if err != nil {
		return nil, false, nil, err
	}
	return data, true, &note, nil
}

func restoreSource(path string, prior []byte, existed bool) error {
	if existed {
		return atomicWritePrivate(path, prior)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("brain: source rollback failed")
	}
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
