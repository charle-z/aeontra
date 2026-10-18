// Package audit provides an append-only audit log for every tool call:
// who/what/when/files/decision/duration (constitution Article I.7). The log is a
// stream of JSON objects, one per line (JSONL), so it is greppable and append-safe.
package audit

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	DefaultMaxBytes = int64(32 << 20)
	DefaultSegments = 4
)

// Decision is the policy outcome recorded for a tool call.
type Decision string

const (
	Allow Decision = "allow" // executed
	Deny  Decision = "deny"  // blocked by policy
	Ask   Decision = "ask"   // returned approval-required, not executed
	Error Decision = "error" // failed during execution
)

// Entry is a single audit record.
type Entry struct {
	Time       string   `json:"time"`            // RFC3339 UTC
	Tool       string   `json:"tool"`            // MCP tool name
	Decision   Decision `json:"decision"`        // allow/deny/ask/error
	Args       string   `json:"args,omitempty"`  // short, redacted summary
	Files      []string `json:"files,omitempty"` // files touched
	DurationMS int64    `json:"duration_ms"`     // wall-clock duration
	Error      string   `json:"error,omitempty"` // redacted error text
}

// Logger appends entries to an io.Writer in a concurrency-safe way.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
	now    func() time.Time // injectable clock for tests
}

// New returns a Logger writing to w.
func New(w io.Writer) *Logger {
	return &Logger{w: w, now: time.Now}
}

// Open opens (creating if needed) an append-only audit file at path.
func Open(path string) (*Logger, error) {
	return OpenWithLimit(path, DefaultMaxBytes, DefaultSegments)
}

// OpenWithLimit is the bounded form used by tests and controlled runtime wiring.
func OpenWithLimit(path string, maxBytes int64, segments int) (*Logger, error) {
	f, err := observability.OpenRotatingWriter(path, maxBytes, segments)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, closer: f, now: time.Now}, nil
}

// Close closes the underlying file if Open was used.
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// Log writes an entry. Time is set if empty; Args, Error, and each Files entry
// are secret-scrubbed so the audit log itself can never become a place secrets leak.
func (l *Logger) Log(e Entry) error {
	if e.Time == "" {
		e.Time = l.now().UTC().Format(time.RFC3339Nano)
	}
	e.Args, _ = policy.Redact(e.Args)
	e.Error, _ = policy.Redact(e.Error)
	if len(e.Files) > 0 {
		e.Files = append([]string(nil), e.Files...)
		for i := range e.Files {
			e.Files[i], _ = policy.Redact(e.Files[i])
		}
	}

	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(b)
	return err
}

// Span times a single tool call and records it on completion.
type Span struct {
	logger *Logger
	tool   string
	start  time.Time
}

// Start begins timing a tool call.
func (l *Logger) Start(tool string) *Span {
	return &Span{logger: l, tool: tool, start: l.now()}
}

// Finish records the span's outcome. err may be nil.
func (s *Span) Finish(decision Decision, args string, files []string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	_ = s.logger.Log(Entry{
		Tool:       s.tool,
		Decision:   decision,
		Args:       args,
		Files:      files,
		DurationMS: s.logger.now().Sub(s.start).Milliseconds(),
		Error:      errStr,
	})
}
