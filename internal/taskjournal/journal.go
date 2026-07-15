package taskjournal

import (
	"errors"
	"sync"
	"time"
)

const (
	defaultStaleAfter = 45 * time.Second
	defaultListLimit  = 100
)

// Journal combines durable state with an in-process best-effort event fan-out. The
// hub has no background worker; it exists only while the Go server is already running.
type Journal struct {
	store      *Store
	now        func() time.Time
	staleAfter time.Duration
	mu         sync.Mutex
	subsMu     sync.Mutex
	subs       map[chan Entry]struct{}
}

func Open(root string) (*Journal, error) {
	store, err := OpenStore(root)
	if err != nil {
		return nil, err
	}
	return &Journal{
		store: store, now: time.Now, staleAfter: defaultStaleAfter,
		subs: make(map[chan Entry]struct{}),
	}, nil
}

func (j *Journal) Start(taskID, operation, controller string) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	entry, err := newEntry(taskID, operation, controller, StateExecuting, j.now())
	if err != nil {
		return err
	}
	return j.write(entry, true)
}

func (j *Journal) Transition(taskID string, state State) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	if _, ok := validStates[state]; !ok {
		return errors.New("task journal: invalid state")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok, err := j.store.Get(taskID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return errors.New("task journal: task not found")
	}
	if isTerminal(entry.State) {
		return errors.New("task journal: terminal task cannot transition")
	}
	entry.State = state
	entry.Heartbeat = j.now().UTC()
	if err := j.store.Put(entry); err != nil {
		return err
	}
	j.publish(entry)
	return nil
}

func (j *Journal) Heartbeat(taskID string) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok, err := j.store.Get(taskID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return errors.New("task journal: task not found")
	}
	if isTerminal(entry.State) {
		return nil
	}
	entry.Heartbeat = j.now().UTC()
	if err := j.store.Put(entry); err != nil {
		return err
	}
	j.publish(entry)
	return nil
}

func (j *Journal) write(entry Entry, createOnly bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if createOnly {
		_, exists, err := j.store.Get(entry.TaskID)
		if err != nil {
			return err
		}
		if exists {
			return errors.New("task journal: task already exists")
		}
	}
	if err := j.store.Put(entry); err != nil {
		return err
	}
	j.publish(entry)
	return nil
}

func (j *Journal) Snapshot(limit int) ([]Entry, error) {
	if j == nil {
		return nil, errors.New("task journal: unavailable")
	}
	if limit == 0 {
		limit = defaultListLimit
	}
	entries, err := j.store.List(limit)
	if err != nil {
		return nil, err
	}
	return staleSnapshot(entries, j.now(), j.staleAfter), nil
}

func (j *Journal) Subscribe() (<-chan Entry, func()) {
	channel := make(chan Entry, 16)
	if j == nil {
		close(channel)
		return channel, func() {}
	}
	j.subsMu.Lock()
	j.subs[channel] = struct{}{}
	j.subsMu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			j.subsMu.Lock()
			if _, ok := j.subs[channel]; ok {
				delete(j.subs, channel)
				close(channel)
			}
			j.subsMu.Unlock()
		})
	}
	return channel, cancel
}

func (j *Journal) publish(entry Entry) {
	j.subsMu.Lock()
	defer j.subsMu.Unlock()
	for subscriber := range j.subs {
		select {
		case subscriber <- entry:
		default:
		}
	}
}
