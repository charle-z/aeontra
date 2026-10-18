package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	DriverProtocolVersion  = "mcp-devbox.model-turn-driver.v1"
	maxDriverEnvelopeBytes = int64(256 << 10)
)

type StoreStats struct {
	RuntimeCount   int64 `json:"runtime_count"`
	TurnCount      int64 `json:"turn_count"`
	AwaitingCount  int64 `json:"awaiting_count"`
	RespondedCount int64 `json:"responded_count"`
	ConsumedCount  int64 `json:"consumed_count"`
	BodyBytes      int64 `json:"result_store_bytes"`
}

type DriverMetrics struct {
	ProtocolVersion       string     `json:"protocol_version"`
	StageCalls            uint64     `json:"stage_calls"`
	CreateCalls           uint64     `json:"create_calls"`
	WaitCalls             uint64     `json:"wait_calls"`
	CancelCalls           uint64     `json:"cancel_calls"`
	StatusCalls           uint64     `json:"status_calls"`
	BytesReceived         uint64     `json:"bytes_received"`
	BytesSent             uint64     `json:"bytes_sent"`
	Store                 StoreStats `json:"store"`
	LastInternalErrorCode string     `json:"last_internal_error_code,omitempty"`
}

type Driver struct {
	transport             ModelTurnTransport
	stageCalls            atomic.Uint64
	createCalls           atomic.Uint64
	waitCalls             atomic.Uint64
	cancelCalls           atomic.Uint64
	statusCalls           atomic.Uint64
	bytesReceived         atomic.Uint64
	bytesSent             atomic.Uint64
	lastInternalErrorCode atomic.Value
}

type driverReferenceTransport interface {
	ModelTurnTransport
	StageRequestBody(context.Context, json.RawMessage, bool, time.Duration) (RequestBodyReference, error)
	CreateTurnFromReference(context.Context, ModelRequest) (Turn, error)
}

type driverRuntimeReader interface {
	Runtime(context.Context, string) (Runtime, error)
}

type driverStatsReader interface {
	Stats(context.Context) (StoreStats, error)
}

type driverDisconnectMarker interface {
	MarkDisconnected(context.Context, TurnID) error
}

type driverCreateRequest struct {
	RuntimeID     string           `json:"runtime_id"`
	Sequence      uint64           `json:"sequence"`
	RequestDigest string           `json:"request_digest"`
	Payload       json.RawMessage  `json:"payload,omitempty"`
	RequestRef    string           `json:"request_ref,omitempty"`
	OfferedTools  []ToolDefinition `json:"offered_tools,omitempty"`
	TTLMillis     int64            `json:"ttl_ms,omitempty"`
}

type driverError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func NewDriver(store *Store) (*Driver, error) {
	return NewDriverTransport(store)
}

func NewDriverTransport(transport ModelTurnTransport) (*Driver, error) {
	if transport == nil {
		return nil, ErrNilRendezvous
	}
	driver := &Driver{transport: transport}
	driver.lastInternalErrorCode.Store("")
	return driver, nil
}

func (d *Driver) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.health)
	mux.HandleFunc("POST /v1/request-bodies", d.stageRequestBody)
	mux.HandleFunc("POST /v1/turns", d.createTurn)
	mux.HandleFunc("GET /v1/turns/{turnID}/response", d.waitResponse)
	mux.HandleFunc("DELETE /v1/turns/{turnID}", d.cancelTurn)
	mux.HandleFunc("GET /v1/runtimes/{runtimeID}", d.runtimeStatus)
	mux.HandleFunc("GET /v1/metrics", d.metrics)
	return securityHeaders(mux)
}

func (d *Driver) health(w http.ResponseWriter, _ *http.Request) {
	d.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "protocol_version": DriverProtocolVersion})
}

func (d *Driver) createTurn(w http.ResponseWriter, r *http.Request) {
	d.createCalls.Add(1)
	var request driverCreateRequest
	bytesRead, err := decodeDriverJSON(w, r, &request)
	d.bytesReceived.Add(uint64(bytesRead))
	if err != nil {
		d.writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	hasPayload := len(bytes.TrimSpace(request.Payload)) > 0
	hasReference := strings.TrimSpace(request.RequestRef) != ""
	if !validDigest(request.RequestDigest) || hasPayload == hasReference {
		d.writeError(w, http.StatusBadRequest, "invalid_request", ErrInvalidRequest)
		return
	}
	if hasPayload && int64(len(request.Payload)) > MaxInlineRequestBytes {
		d.writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", ErrBodyTooLarge)
		return
	}
	ttl := DefaultTurnTTL
	if request.TTLMillis != 0 {
		ttl = time.Duration(request.TTLMillis) * time.Millisecond
	}
	if ttl <= 0 || ttl > MaxTurnTTL {
		d.writeError(w, http.StatusBadRequest, "invalid_request", ErrInvalidRequest)
		return
	}
	modelRequest := ModelRequest{
		RuntimeID:        request.RuntimeID,
		Sequence:         request.Sequence,
		Payload:          request.Payload,
		CanonicalPayload: true,
		RequestRef:       request.RequestRef,
		RequestDigest:    request.RequestDigest,
		OfferedTools:     request.OfferedTools,
		TTL:              ttl,
	}
	var turn Turn
	if hasReference {
		referenced, ok := d.transport.(driverReferenceTransport)
		if !ok {
			d.writeError(w, http.StatusConflict, "reference_unsupported", ErrRequestRefConflict)
			return
		}
		turn, err = referenced.CreateTurnFromReference(r.Context(), modelRequest)
	} else {
		turn, err = d.transport.CreateTurn(r.Context(), modelRequest)
	}
	if err != nil {
		d.writeStoreError(w, err)
		return
	}
	if turn.RequestDigest != request.RequestDigest {
		_ = d.transport.Cancel(context.Background(), turn.ID)
		d.writeError(w, http.StatusConflict, "digest_mismatch", ErrSequenceMismatch)
		return
	}
	d.writeJSON(w, http.StatusCreated, turn)
}

func (d *Driver) waitResponse(w http.ResponseWriter, r *http.Request) {
	d.waitCalls.Add(1)
	turnID := TurnID(r.PathValue("turnID"))
	if !strings.HasPrefix(string(turnID), "mt_") {
		d.writeError(w, http.StatusBadRequest, "invalid_turn_id", ErrInvalidRequest)
		return
	}
	response, err := d.transport.WaitResponse(r.Context(), turnID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if marker, ok := d.transport.(driverDisconnectMarker); ok {
				_ = marker.MarkDisconnected(context.Background(), turnID)
			}
			panic(http.ErrAbortHandler)
		}
		d.writeStoreError(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, response)
}

func (d *Driver) cancelTurn(w http.ResponseWriter, r *http.Request) {
	d.cancelCalls.Add(1)
	turnID := TurnID(r.PathValue("turnID"))
	if err := d.transport.Cancel(r.Context(), turnID); err != nil {
		d.writeStoreError(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, map[string]any{"turn_id": turnID, "status": StatusCancelled})
}

func (d *Driver) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	d.statusCalls.Add(1)
	runtimeID := r.PathValue("runtimeID")
	reader, ok := d.transport.(driverRuntimeReader)
	if !ok {
		d.writeError(w, http.StatusNotFound, "not_found", ErrTurnNotFound)
		return
	}
	runtime, err := reader.Runtime(r.Context(), runtimeID)
	if err != nil {
		d.writeStoreError(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, runtime)
}

func (d *Driver) metrics(w http.ResponseWriter, r *http.Request) {
	var stats StoreStats
	if reader, ok := d.transport.(driverStatsReader); ok {
		var err error
		stats, err = reader.Stats(r.Context())
		if err != nil {
			d.writeStoreError(w, err)
			return
		}
	}
	d.writeJSON(w, http.StatusOK, DriverMetrics{
		ProtocolVersion:       DriverProtocolVersion,
		StageCalls:            d.stageCalls.Load(),
		CreateCalls:           d.createCalls.Load(),
		WaitCalls:             d.waitCalls.Load(),
		CancelCalls:           d.cancelCalls.Load(),
		StatusCalls:           d.statusCalls.Load(),
		BytesReceived:         d.bytesReceived.Load(),
		BytesSent:             d.bytesSent.Load(),
		Store:                 stats,
		LastInternalErrorCode: d.lastInternalErrorCode.Load().(string),
	})
}

func (s *Store) Stats(ctx context.Context) (StoreStats, error) {
	var stats StoreStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_runtimes`).Scan(&stats.RuntimeCount); err != nil {
		return StoreStats{}, errors.New("model runtime statistics failed")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('created','awaiting_model','disconnected') THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='responded' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='consumed' THEN 1 ELSE 0 END),0) FROM model_turns`).Scan(&stats.TurnCount, &stats.AwaitingCount, &stats.RespondedCount, &stats.ConsumedCount); err != nil {
		return StoreStats{}, errors.New("model turn statistics failed")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(content_bytes),0) FROM turn_bodies`).Scan(&stats.BodyBytes); err != nil {
		return StoreStats{}, errors.New("model body statistics failed")
	}
	return stats, nil
}

func decodeDriverJSON(w http.ResponseWriter, r *http.Request, target any) (int64, error) {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return 0, errors.New("content type must be application/json")
	}
	reader := http.MaxBytesReader(w, r.Body, maxDriverEnvelopeBytes)
	defer reader.Close()
	counting := &countingReader{reader: reader}
	decoder := json.NewDecoder(counting)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return counting.count, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return counting.count, errors.New("multiple JSON values are not allowed")
	}
	return counting.count, nil
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (d *Driver) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		d.writeError(w, http.StatusBadRequest, "invalid_request", err)
	case errors.Is(err, ErrBodyTooLarge):
		d.writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", err)
	case errors.Is(err, ErrTurnNotFound):
		d.writeError(w, http.StatusNotFound, "not_found", err)
	case errors.Is(err, ErrSequenceMismatch), errors.Is(err, ErrTurnConflict), errors.Is(err, ErrResponseReplay), errors.Is(err, ErrLateResponse), errors.Is(err, ErrToolNotOffered), errors.Is(err, ErrRequestRefConflict):
		d.writeError(w, http.StatusConflict, "conflict", err)
	case errors.Is(err, ErrTurnQuotaExceeded):
		d.writeError(w, http.StatusInsufficientStorage, "quota_exceeded", err)
	default:
		internalCode := driverInternalErrorCode(err)
		d.lastInternalErrorCode.Store(internalCode)
		d.writeError(w, http.StatusInternalServerError, "internal_"+internalCode, errors.New("model turn driver operation failed: "+internalCode))
	}
}

func driverInternalErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	switch err.Error() {
	case "model body transaction failed", "model turn transaction failed":
		return "transaction_begin"
	case "model body cleanup failed", "model turn cleanup failed", "model turn expiry failed", "model runtime expiry failed", "runtime body cleanup failed":
		return "cleanup"
	case "model turn quota check failed", "model body quota check failed":
		return "quota_read"
	case "model turn quota eviction failed":
		return "quota_eviction"
	case "model request body persistence failed", "model request persistence failed":
		return "request_persist"
	case "model request body commit failed", "model turn commit failed":
		return "commit"
	case "model runtime persistence failed":
		return "runtime_persist"
	case "model runtime activation failed":
		return "runtime_activate"
	case "model runtime read failed":
		return "runtime_read"
	case "model runtime turn read failed":
		return "runtime_turn_read"
	case "model turn sequence read failed":
		return "sequence_read"
	case "model request body read failed", "model body unavailable":
		return "request_read"
	case "model request reference binding check failed":
		return "reference_binding_read"
	case "model turn persistence failed":
		return "turn_persist"
	case "model turn activation failed":
		return "turn_activate"
	case "model runtime turn binding failed":
		return "runtime_bind"
	case "model turn read failed":
		return "turn_read"
	case "model response transaction failed":
		return "response_transaction"
	case "model response persistence failed":
		return "response_persist"
	case "model response compare-and-swap failed":
		return "response_cas"
	case "model response commit failed":
		return "response_commit"
	case "model response read failed":
		return "response_read"
	case "model response body unavailable":
		return "response_body"
	case "model response consume failed":
		return "response_consume"
	case "model turn cancellation failed":
		return "turn_cancel"
	case "model turn failure transition failed":
		return "turn_fail"
	case "model turn disconnect failed":
		return "turn_disconnect"
	case "model runtime resume failed":
		return "runtime_resume"
	case "model runtime transition failed":
		return "runtime_transition"
	case "model runtime heartbeat failed":
		return "runtime_heartbeat"
	case "model runtime result update failed":
		return "runtime_result"
	case "model runtime cancellation failed", "model runtime turn cancellation failed", "model runtime cancellation commit failed":
		return "runtime_cancel"
	case "model body id generation failed", "model turn id generation failed", "model runtime id generation failed":
		return "id_generation"
	case "model runtime statistics failed", "model turn statistics failed", "model body statistics failed":
		return "statistics"
	default:
		return "unknown"
	}
}

var safeDriverInternalErrorCodes = map[string]struct{}{
	"none":                   {},
	"transaction_begin":      {},
	"cleanup":                {},
	"quota_read":             {},
	"quota_eviction":         {},
	"request_persist":        {},
	"commit":                 {},
	"runtime_persist":        {},
	"runtime_activate":       {},
	"runtime_read":           {},
	"runtime_turn_read":      {},
	"sequence_read":          {},
	"request_read":           {},
	"reference_binding_read": {},
	"turn_persist":           {},
	"turn_activate":          {},
	"runtime_bind":           {},
	"turn_read":              {},
	"response_transaction":   {},
	"response_persist":       {},
	"response_cas":           {},
	"response_commit":        {},
	"response_read":          {},
	"response_body":          {},
	"response_consume":       {},
	"turn_cancel":            {},
	"turn_fail":              {},
	"turn_disconnect":        {},
	"runtime_resume":         {},
	"runtime_transition":     {},
	"runtime_heartbeat":      {},
	"runtime_result":         {},
	"runtime_cancel":         {},
	"id_generation":          {},
	"statistics":             {},
	"unknown":                {},
}

// NormalizeDriverInternalErrorCode returns only a closed, payload-independent
// diagnostic code. Unknown or attacker-controlled values collapse to unknown.
func NormalizeDriverInternalErrorCode(code string) string {
	if _, ok := safeDriverInternalErrorCodes[code]; ok {
		return code
	}
	return "unknown"
}

func (d *Driver) writeError(w http.ResponseWriter, status int, code string, err error) {
	d.writeJSON(w, status, driverError{Error: err.Error(), Code: code})
}

func (d *Driver) writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	d.bytesSent.Add(uint64(len(encoded)))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host == "" {
			http.Error(w, "host required", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
