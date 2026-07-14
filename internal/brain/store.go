package brain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

// Store owns the dedicated Brain filesystem jail. It is intentionally separate from
// repository roots so general filesystem tools cannot select or enumerate it.
type Store struct {
	root    string
	jail    *policy.Jail
	now     func() time.Time
	mu      sync.Mutex
	git     *localGit
	indexMu sync.RWMutex
	index   *Index
}

// OpenStore creates or verifies the private source/cache layout without initializing
// Git or SQLite. Those concerns are added in later P9 steps.
func OpenStore(root string, now time.Time) (*Store, error) {
	return OpenStoreWithClock(root, func() time.Time { return now })
}

// OpenStoreWithClock is the runtime constructor; the injected clock keeps review and
// timestamp validation correct for long-lived processes and deterministic in tests.
func OpenStoreWithClock(root string, now func() time.Time) (*Store, error) {
	if now == nil {
		return nil, errors.New("brain: clock is required")
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("brain: root must be an absolute path")
	}
	if err := rejectSymlinkAncestors(root); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	for _, directory := range []string{CuratedDir, WorkingDir, CacheDir} {
		if err := ensurePrivateDirectory(filepath.Join(root, directory)); err != nil {
			return nil, err
		}
	}
	jail, err := policy.NewJail([]string{root})
	if err != nil {
		return nil, err
	}
	return &Store{root: root, jail: jail, now: func() time.Time { return now().UTC() }}, nil
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// InitializeGit creates or verifies the private local-only history repository. It is
// idempotent and never creates or contacts a remote.
func (s *Store) InitializeGit(ctx context.Context) error {
	if s == nil || s.jail == nil {
		return errors.New("brain: store is unavailable")
	}
	if ctx == nil {
		return errors.New("brain: context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.git != nil {
		return nil
	}
	runner, err := newExecGitRunner(s.root)
	if err != nil {
		return err
	}
	repository := &localGit{root: s.root, runner: runner}
	if err := repository.initialize(ctx, s.now()); err != nil {
		return err
	}
	s.git = repository
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return errors.New("brain: private directory is unavailable")
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return errors.New("brain: private directory is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("brain: source path is not a private directory")
	}
	if info.Mode().Perm() != 0o700 {
		return errors.New("brain: private directory permissions must be 0700")
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.New("brain: root ancestry is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("brain: root ancestry contains a symlink")
		}
		if !info.IsDir() {
			return errors.New("brain: root ancestry is not a directory")
		}
	}
	return nil
}

func trustDirectory(trust TrustLevel) (string, error) {
	switch trust {
	case TrustCurated:
		return CuratedDir, nil
	case TrustWorking:
		return WorkingDir, nil
	default:
		return "", errors.New("brain: invalid trust level")
	}
}

func (s *Store) resolveTarget(trust TrustLevel, slug string) (string, error) {
	if s == nil || s.jail == nil {
		return "", errors.New("brain: store is unavailable")
	}
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	directory, err := trustDirectory(trust)
	if err != nil {
		return "", err
	}
	base := filepath.Join(s.root, directory)
	if err := ensurePrivateDirectory(base); err != nil {
		return "", err
	}
	target := filepath.Join(base, slug+".md")
	resolved, err := s.jail.Resolve(target)
	if err != nil {
		return "", err
	}
	if resolved != target {
		return "", errors.New("brain: note path changed during resolution")
	}
	if info, err := os.Lstat(resolved); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", errors.New("brain: note source must be a regular file")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("brain: note source permissions are too broad")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("brain: note source is unavailable")
	}
	return resolved, nil
}

// AgentTarget proves that agent tools can target only working notes.
func (s *Store) AgentTarget(trust TrustLevel, slug string) (string, error) {
	if trust == TrustCurated {
		return "", ErrCuratedRO
	}
	if trust != TrustWorking {
		return "", fmt.Errorf("brain: agent target must be working")
	}
	return s.resolveTarget(TrustWorking, slug)
}

// ReadSource reads, strictly parses, and content-redacts one source note.
func (s *Store) ReadSource(trust TrustLevel, slug string) (Note, error) {
	target, err := s.resolveTarget(trust, slug)
	if err != nil {
		return Note{}, err
	}
	file, err := os.Open(target)
	if err != nil {
		return Note{}, errors.New("brain: note source is unavailable")
	}
	defer file.Close()
	limited := io.LimitReader(file, MaxFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Note{}, errors.New("brain: note source could not be read")
	}
	if len(data) > MaxFileBytes {
		return Note{}, fmt.Errorf("brain: source file exceeds %d bytes", MaxFileBytes)
	}
	note, err := ParseNote(data, slug, trust, s.now())
	if err != nil {
		return Note{}, err
	}
	note.Metadata.Title, _ = policy.Redact(note.Metadata.Title)
	note.Metadata.Provenance, _ = policy.Redact(note.Metadata.Provenance)
	note.Body, _ = policy.Redact(note.Body)
	return note, nil
}

func sourceExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("brain: note source is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("brain: note source must be a regular file")
	}
	return true, nil
}

// FindBySlug enforces global uniqueness across curated and working notes.
func (s *Store) FindBySlug(slug string) (Note, error) {
	curatedPath, err := s.resolveTarget(TrustCurated, slug)
	if err != nil {
		return Note{}, err
	}
	workingPath, err := s.resolveTarget(TrustWorking, slug)
	if err != nil {
		return Note{}, err
	}
	curatedExists, err := sourceExists(curatedPath)
	if err != nil {
		return Note{}, err
	}
	workingExists, err := sourceExists(workingPath)
	if err != nil {
		return Note{}, err
	}
	if curatedExists && workingExists {
		return Note{}, errors.New("brain: duplicate slug exists in curated and working")
	}
	if curatedExists {
		return s.ReadSource(TrustCurated, slug)
	}
	if workingExists {
		return s.ReadSource(TrustWorking, slug)
	}
	return Note{}, os.ErrNotExist
}
