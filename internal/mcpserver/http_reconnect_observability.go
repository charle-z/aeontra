package mcpserver

import (
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

// httpTransportTelemetry contains only process-local aggregate counters. It never
// stores session identifiers, credentials, client identity, network addresses, or
// request payloads.
type httpTransportTelemetry struct {
	reconnections atomic.Uint64
}

func newHTTPTransportTelemetry() *httpTransportTelemetry {
	return &httpTransportTelemetry{}
}

func (t *httpTransportTelemetry) ReconnectCount() uint64 {
	if t == nil {
		return 0
	}
	return t.reconnections.Load()
}

func (t *httpTransportTelemetry) recordInitialize(r *http.Request) (observability.EventName, uint64) {
	if t == nil {
		return observability.EventMCPSessionCreated, 0
	}
	// A reinitialization is recognized only when the client explicitly carries a
	// previous session header into initialize. We do not correlate clients, IPs,
	// credentials, or opaque session values.
	if r != nil && strings.TrimSpace(r.Header.Get("Mcp-Session-Id")) != "" {
		return observability.EventMCPSessionReinitialized, t.reconnections.Add(1)
	}
	return observability.EventMCPSessionCreated, t.reconnections.Load()
}

func sessionValidationErrorClass(validation httpSessionValidation) observability.ErrorClass {
	switch validation {
	case httpSessionMissing:
		return observability.ErrorSessionMissing
	case httpSessionExpired:
		return observability.ErrorSessionExpired
	default:
		return observability.ErrorSessionUnknown
	}
}

func (s *Server) emitHTTPSessionEvent(r *http.Request, name observability.EventName, outcome observability.Outcome, errorClass observability.ErrorClass, reconnectCount uint64) {
	if s == nil || s.observer == nil {
		return
	}
	info := s.mustRuntimeInfo()
	event := observability.Event{
		Level:          observability.LevelInfo,
		Component:      observability.ComponentMCP,
		Name:           name,
		Transport:      observability.TransportHTTP,
		Route:          observability.RouteMCP,
		Method:         observability.MethodInitialize,
		Outcome:        outcome,
		ErrorClass:     errorClass,
		Commit:         info.Commit,
		ToolCount:      info.ToolCount,
		CatalogHash:    info.CatalogHash,
		BootID:         s.BootID(),
		ReconnectCount: reconnectCount,
	}
	if r != nil {
		event.RequestID = requestIDFromContext(r.Context())
	}
	if outcome == observability.OutcomeError || outcome == observability.OutcomeDenied {
		event.Level = observability.LevelError
	}
	_ = s.observer.Emit(event)
}

func (s *Server) emitHTTPDrainEvent(name observability.EventName, duration time.Duration, outcome observability.Outcome, errorClass observability.ErrorClass, reconnectCount uint64) {
	if s == nil || s.observer == nil {
		return
	}
	info := s.mustRuntimeInfo()
	event := observability.Event{
		Level:          observability.LevelInfo,
		Component:      observability.ComponentServer,
		Name:           name,
		Transport:      observability.TransportHTTP,
		Outcome:        outcome,
		DurationMS:     duration.Milliseconds(),
		ErrorClass:     errorClass,
		Commit:         info.Commit,
		ToolCount:      info.ToolCount,
		CatalogHash:    info.CatalogHash,
		BootID:         s.BootID(),
		ReconnectCount: reconnectCount,
	}
	if outcome == observability.OutcomeError {
		event.Level = observability.LevelError
	}
	_ = s.observer.Emit(event)
}
