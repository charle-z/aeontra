package taskjournal

import (
	"errors"
	"sync"
	"time"
)

const defaultStaleAfter = 45 * time.Second

// Journal combines durable SQLite state with an in-process best-effort event
// fan-out. No database connection is retained while an SSE client is waiting.
type Journal struct {
	store      *SQLiteStore
	now        func() time.Time
	staleAfter time.Duration
	subsMu     sync.Mutex
	subs       map[chan Event]struct{}
	statusMu   sync.Mutex
	degraded   string
}

func Open(root string) (*Journal, error) {
	store, err := OpenSQLiteStore(root)
	if err != nil {
		return nil, err
	}
	return &Journal{
		store: store, now: time.Now, staleAfter: defaultStaleAfter,
		subs: make(map[chan Event]struct{}),
	}, nil
}

func (j *Journal) Close() error {
	if j == nil || j.store == nil {
		return nil
	}
	return j.store.Close()
}

func (j *Journal) Start(taskID, operation, controller string) error {
	return j.StartScoped(taskID, operation, controller, "", "")
}

func (j *Journal) StartScoped(taskID, operation, controller, projectID, edgeID string) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	entry, err := newEntry(taskID, operation, controller, StateExecuting, j.now())
	entry.ProjectID = projectID
	entry.EdgeID = edgeID
	if err != nil {
		return err
	}
	entry, event, err := j.store.Create(entry)
	if err != nil {
		j.RecordFailure(err)
		return err
	}
	event.Task = j.derive(entry)
	j.publish(event)
	return nil
}

func (j *Journal) Transition(taskID string, state State) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	if _, ok := validStates[state]; !ok || state == StateDisconnected {
		return errors.New("task journal: invalid state")
	}
	entry, event, err := j.store.Update(taskID, &state, j.now())
	if err != nil {
		j.RecordFailure(err)
		return err
	}
	event.Task = j.derive(entry)
	j.publish(event)
	return nil
}

func (j *Journal) Heartbeat(taskID string) error {
	if j == nil {
		return errors.New("task journal: unavailable")
	}
	entry, event, err := j.store.Update(taskID, nil, j.now())
	if err != nil {
		j.RecordFailure(err)
		return err
	}
	if event.EventID != 0 {
		event.Task = j.derive(entry)
		j.publish(event)
	}
	return nil
}

func (j *Journal) Snapshot(limit int) ([]Entry, error) {
	page, err := j.Page(limit, "")
	return page.Entries, err
}

func (j *Journal) Page(limit int, cursor string) (Page, error) {
	return j.PageFiltered(limit, cursor, TaskFilter{})
}

func (j *Journal) PageFiltered(limit int, cursor string, filter TaskFilter) (Page, error) {
	if j == nil {
		return Page{}, errors.New("task journal: unavailable")
	}
	page, err := j.store.ListPageFiltered(limit, cursor, filter)
	if err != nil {
		j.RecordFailure(err)
		return Page{}, err
	}
	for index := range page.Entries {
		page.Entries[index] = j.derive(page.Entries[index])
	}
	return page, nil
}

func (j *Journal) Replay(after int64, limit int) ([]Event, bool, error) {
	if j == nil {
		return nil, false, errors.New("task journal: unavailable")
	}
	events, gap, err := j.store.Replay(after, limit)
	if err != nil {
		j.RecordFailure(err)
		return nil, false, err
	}
	for index := range events {
		events[index].Task = j.derive(events[index].Task)
	}
	return events, gap, nil
}

func (j *Journal) derive(entry Entry) Entry {
	if canDisconnect(entry.State) && j.now().UTC().Sub(entry.HeartbeatAt) > j.staleAfter {
		entry.State = StateDisconnected
		entry.DerivedState = true
	}
	return entry
}

func (j *Journal) Subscribe() (<-chan Event, func()) {
	channel := make(chan Event, 32)
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

func (j *Journal) publish(event Event) {
	j.subsMu.Lock()
	defer j.subsMu.Unlock()
	for subscriber := range j.subs {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (j *Journal) RecordFailure(err error) {
	if j == nil || err == nil {
		return
	}
	j.statusMu.Lock()
	j.degraded = "persistence failure"
	j.statusMu.Unlock()
}

func (j *Journal) Status() Status {
	if j == nil {
		return Status{Storage: StorageDegraded, Detail: "journal unavailable"}
	}
	j.statusMu.Lock()
	detail := j.degraded
	j.statusMu.Unlock()
	status := j.store.Status(detail)
	if detail != "" {
		status.Storage = StorageDegraded
	}
	return status
}
