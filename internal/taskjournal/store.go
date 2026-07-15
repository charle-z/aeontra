package taskjournal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxEntries       = 256
	maxEntryFileSize = 4 << 10
)

// Store owns the bounded JSON records under the configured private task root.
type Store struct {
	root string
	mu   sync.Mutex
}

func OpenStore(root string) (*Store, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("task journal: root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, errors.New("task journal: cannot create root")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("task journal: root must be a real directory")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, errors.New("task journal: cannot secure root")
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(entry Entry) error {
	if s == nil {
		return errors.New("task journal: store is unavailable")
	}
	if err := entry.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, entry.TaskID+".json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(s.root)
		if readErr != nil {
			return errors.New("task journal: cannot inspect root")
		}
		count := 0
		for _, item := range entries {
			if !item.IsDir() && strings.HasSuffix(item.Name(), ".json") {
				count++
			}
		}
		if count >= maxEntries {
			return errors.New("task journal: entry limit reached")
		}
	} else if err != nil {
		return errors.New("task journal: cannot inspect entry")
	} else {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("task journal: entry path is unsafe")
		}
	}

	body, err := json.Marshal(entry)
	if err != nil || len(body) > maxEntryFileSize {
		return errors.New("task journal: entry cannot be encoded")
	}
	return writeAtomic0600(path, body)
}

func (s *Store) Get(taskID string) (Entry, bool, error) {
	if s == nil || !taskIDPattern.MatchString(taskID) {
		return Entry{}, false, errors.New("task journal: invalid task id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.readLocked(filepath.Join(s.root, taskID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	return entry, true, nil
}

func (s *Store) List(limit int) ([]Entry, error) {
	if s == nil {
		return nil, errors.New("task journal: store is unavailable")
	}
	if limit <= 0 || limit > maxEntries {
		return nil, errors.New("task journal: invalid limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := os.ReadDir(s.root)
	if err != nil {
		return nil, errors.New("task journal: cannot inspect root")
	}
	entries := make([]Entry, 0, min(limit, len(items)))
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		if len(entries) >= maxEntries {
			return nil, errors.New("task journal: entry limit exceeded")
		}
		entry, readErr := s.readLocked(filepath.Join(s.root, item.Name()))
		if readErr != nil {
			return nil, readErr
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Heartbeat.After(entries[j].Heartbeat)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *Store) readLocked(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxEntryFileSize {
		return Entry{}, errors.New("task journal: entry file is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, errors.New("task journal: cannot read entry")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var entry Entry
	if err := decoder.Decode(&entry); err != nil {
		return Entry{}, errors.New("task journal: malformed entry")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Entry{}, errors.New("task journal: malformed entry")
	}
	if err := entry.validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func writeAtomic0600(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".task-*.tmp")
	if err != nil {
		return errors.New("task journal: cannot create temporary entry")
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return errors.New("task journal: cannot write entry")
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return errors.New("task journal: cannot secure entry")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.New("task journal: cannot sync entry")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("task journal: cannot close entry")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errors.New("task journal: cannot replace entry")
	}
	cleanup = false
	return nil
}

func staleSnapshot(entries []Entry, now time.Time, staleAfter time.Duration) []Entry {
	result := make([]Entry, len(entries))
	copy(result, entries)
	for index := range result {
		if canDisconnect(result[index].State) && now.Sub(result[index].Heartbeat) > staleAfter {
			result[index].State = StateDisconnected
		}
	}
	return result
}
