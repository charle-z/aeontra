package mcpserver

import (
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

const journalHeartbeatInterval = 10 * time.Second

// WithTaskJournal attaches the optional durable journal without changing any MCP
// tool registration or wire schema.
func (s *Server) WithTaskJournal(journal *taskjournal.Journal) *Server {
	if s != nil {
		s.journal = journal
	}
	return s
}

func (s *Server) startTaskJournal(operation string, transport observability.Transport) (string, func()) {
	if s == nil || s.journal == nil {
		return "", func() {}
	}
	taskID := observability.NewRequestID()
	controller := "internal"
	switch transport {
	case observability.TransportHTTP:
		controller = "http"
	case observability.TransportStdio:
		controller = "stdio"
	}
	if err := s.journal.Start(taskID, operation, controller); err != nil {
		s.journal.RecordFailure(err)
		return "", func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(journalHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := s.journal.Heartbeat(taskID); err != nil {
					s.journal.RecordFailure(err)
				}
			}
		}
	}()
	return taskID, func() {
		close(stop)
		<-done
	}
}

func (s *Server) finishTaskJournal(taskID, operation string, toolErr error) {
	if s == nil || s.journal == nil || taskID == "" {
		return
	}
	if err := s.journal.Transition(taskID, taskjournal.StateValidating); err != nil {
		s.journal.RecordFailure(err)
	}
	if toolErr != nil {
		if err := s.journal.Transition(taskID, taskjournal.StateFailed); err != nil {
			s.journal.RecordFailure(err)
		}
		return
	}
	if strings.HasSuffix(operation, "_preview") {
		if err := s.journal.Transition(taskID, taskjournal.StatePlanned); err != nil {
			s.journal.RecordFailure(err)
		}
		return
	}
	if err := s.journal.Transition(taskID, taskjournal.StateCompleted); err != nil {
		s.journal.RecordFailure(err)
	}
}
