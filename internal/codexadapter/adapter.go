package codexadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	ProtocolVersion       = "mcp-devbox.model-turn.v1"
	defaultTurnTTL        = 15 * time.Minute
	maxResponsesBodyBytes = int64(4 << 20)
	maxModelTextBytes     = 1 << 20
	maxModelToolCalls     = 64
)

var (
	runtimeIDPattern  = regexp.MustCompile(`^mr_[a-f0-9]{32}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type runtimeReader interface {
	Runtime(context.Context, string) (modelturn.Runtime, error)
}

type Options struct {
	RuntimeID string
	ModelID   string
	Transport modelturn.ModelTurnTransport
	TTL       time.Duration
}

// Adapter exposes one private loopback Responses endpoint backed by the
// existing durable model-turn transport.
type Adapter struct {
	runtimeID string
	modelID   string
	transport modelturn.ModelTurnTransport
	runtimes  runtimeReader
	ttl       time.Duration
	mu        sync.Mutex
}

func New(options Options) (*Adapter, error) {
	if !runtimeIDPattern.MatchString(options.RuntimeID) {
		return nil, errors.New("Codex adapter runtime id is invalid")
	}
	if !identifierPattern.MatchString(options.ModelID) {
		return nil, errors.New("Codex adapter model id is invalid")
	}
	if options.Transport == nil {
		return nil, errors.New("Codex adapter transport is required")
	}
	runtimes, ok := options.Transport.(runtimeReader)
	if !ok {
		return nil, errors.New("Codex adapter transport cannot read runtime state")
	}
	ttl := options.TTL
	if ttl == 0 {
		ttl = defaultTurnTTL
	}
	if ttl < time.Second || ttl > modelturn.MaxTurnTTL {
		return nil, errors.New("Codex adapter turn TTL is invalid")
	}
	return &Adapter{
		runtimeID: options.RuntimeID,
		modelID:   options.ModelID,
		transport: options.Transport,
		runtimes:  runtimes,
		ttl:       ttl,
	}, nil
}

func (a *Adapter) Handler() http.Handler {
	return http.HandlerFunc(a.serveHTTP)
}

func (a *Adapter) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	setAdapterHeaders(writer)
	if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
		writeAdapterError(writer, http.StatusNotFound, "not_found")
		return
	}
	if !loopbackPeer(request.RemoteAddr) || !loopbackHost(request.Host) {
		writeAdapterError(writer, http.StatusForbidden, "loopback_required")
		return
	}
	if request.Header.Get("Authorization") != "" {
		writeAdapterError(writer, http.StatusBadRequest, "authorization_forbidden")
		return
	}
	if contentType := strings.ToLower(request.Header.Get("Content-Type")); !strings.HasPrefix(contentType, "application/json") {
		writeAdapterError(writer, http.StatusUnsupportedMediaType, "content_type")
		return
	}

	input, err := decodeResponsesRequest(writer, request)
	if err != nil {
		writeAdapterError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if input.Model != a.modelID || !input.Stream || input.Store || input.ParallelToolCalls {
		writeAdapterError(writer, http.StatusBadRequest, "unsupported_request")
		return
	}
	normalized, err := normalizeResponsesRequest(input)
	if err != nil {
		writeAdapterError(writer, http.StatusBadRequest, "invalid_request")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := request.Context().Err(); err != nil {
		return
	}
	runtime, err := a.runtimes.Runtime(request.Context(), a.runtimeID)
	if err != nil || runtime.RuntimeID != a.runtimeID {
		writeAdapterError(writer, http.StatusBadGateway, "runtime_unavailable")
		return
	}
	digest, err := modelturn.ExactPayloadDigest(normalized.payload)
	if err != nil {
		writeAdapterError(writer, http.StatusInternalServerError, "normalization_failed")
		return
	}
	turn, err := a.transport.CreateTurn(request.Context(), modelturn.ModelRequest{
		RuntimeID:        a.runtimeID,
		Sequence:         runtime.LastSequence + 1,
		Payload:          normalized.payload,
		CanonicalPayload: true,
		RequestDigest:    digest,
		OfferedTools:     normalized.offeredTools,
		TTL:              a.ttl,
	})
	if err != nil {
		writeAdapterError(writer, http.StatusBadGateway, "turn_create_failed")
		return
	}
	response, err := a.transport.WaitResponse(request.Context(), turn.ID)
	if err != nil {
		if request.Context().Err() != nil {
			_ = a.transport.Cancel(context.Background(), turn.ID)
			return
		}
		writeAdapterError(writer, http.StatusBadGateway, "turn_wait_failed")
		return
	}
	if response.RuntimeID != turn.RuntimeID || response.TurnID != turn.ID || response.Sequence != turn.Sequence || response.RequestDigest != turn.RequestDigest {
		_ = a.transport.Cancel(context.Background(), turn.ID)
		writeAdapterError(writer, http.StatusBadGateway, "response_identity")
		return
	}
	bounded, err := validateBoundedResponse(response.Payload, normalized.toolsByID)
	if err != nil {
		_ = a.transport.Cancel(context.Background(), turn.ID)
		writeAdapterError(writer, http.StatusBadGateway, "invalid_model_response")
		return
	}
	if err := writeResponsesSSE(writer, turn, bounded, normalized.toolsByID); err != nil {
		return
	}
}

func decodeResponsesRequest(writer http.ResponseWriter, request *http.Request) (responsesRequest, error) {
	reader := http.MaxBytesReader(writer, request.Body, maxResponsesBodyBytes)
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var input responsesRequest
	if err := decoder.Decode(&input); err != nil {
		return responsesRequest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return responsesRequest{}, errors.New("multiple Responses request values are not allowed")
	}
	if input.Input == nil {
		return responsesRequest{}, errors.New("Responses input is required")
	}
	return input, nil
}

func loopbackPeer(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackHost(hostPort string) bool {
	host := hostPort
	if parsed, _, err := net.SplitHostPort(hostPort); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func setAdapterHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
}

func writeAdapterError(writer http.ResponseWriter, status int, code string) {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "MCP Devbox Codex adapter rejected the request",
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func canonicalJSON(value any) (json.RawMessage, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimSuffix(buffer.String(), "\n")), nil
}

func decodeJSONObject(raw json.RawMessage, label string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s must be one JSON object", label)
	}
	if value == nil {
		return nil, fmt.Errorf("%s must be one JSON object", label)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must be one JSON object", label)
	}
	return value, nil
}
