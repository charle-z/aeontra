package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	PairPath = "/edge/v1/pair"

	HeaderDevice    = "X-Edge-Device"
	HeaderTimestamp = "X-Edge-Timestamp"
	HeaderNonce     = "X-Edge-Nonce"
	HeaderSignature = "X-Edge-Signature"

	maxPairBody   = 4 << 10
	maxSignedBody = 1 << 20
)

type deviceContextKey struct{}

type pairRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// NewHTTPHandler exposes only the unauthenticated, one-time pairing exchange.
// Device-authenticated routes are added explicitly with RequireDevice.
func NewHTTPHandler(store *Store, modelTurns ...*modelturn.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PairPath, store.handlePair)
	mux.Handle("/edge/v1/control-key", store.RequireDevice(http.HandlerFunc(store.handleControlKey)))
	mux.Handle("/edge/v1/workspaces/register", store.RequireDevice(http.HandlerFunc(store.handleWorkspaceRegistration)))
	mux.Handle("/edge/v1/tasks/lease", store.RequireDevice(http.HandlerFunc(store.handleLease)))
	mux.Handle("/edge/v1/tasks/", store.RequireDevice(http.HandlerFunc(store.handleTaskAction)))
	mux.Handle("/edge/v1/operations/lease", store.RequireDevice(http.HandlerFunc(store.handleOperationLease)))
	mux.Handle("/edge/v1/operations/", store.RequireDevice(http.HandlerFunc(store.handleOperationAction)))
	mux.Handle("/edge/v1/autopilot/report", store.RequireDevice(http.HandlerFunc(store.handleAutopilotReport)))
	if len(modelTurns) > 0 && modelTurns[0] != nil {
		registerModelRelayRoutes(mux, store, modelTurns[0])
	}
	return mux
}

func (s *Store) handleAutopilotReport(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var result OperationResult
	if !decodeStrictJSON(w, r, &result) {
		return
	}
	if err := s.ReportAutopilot(DeviceFromContext(r.Context()).ID, result); err != nil {
		http.Error(w, "autopilot report rejected", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type operationLeaseRequest struct {
	LeaseSeconds int `json:"lease_seconds"`
}
type operationCompletionRequest struct {
	LeaseID  string          `json:"lease_id"`
	Result   OperationResult `json:"result"`
	SafeCode string          `json:"safe_code,omitempty"`
}

func (s *Store) handleOperationLease(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var request operationLeaseRequest
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	lease, err := s.LeaseOperation(DeviceFromContext(r.Context()).ID, time.Duration(request.LeaseSeconds)*time.Second)
	if errors.Is(err, ErrNoTaskAvailable) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "operation lease rejected", http.StatusBadRequest)
		return
	}
	lease, err = s.SignOperationLease(lease)
	if err != nil {
		http.Error(w, "operation lease rejected", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

type controlKeyResponse struct {
	PublicKey string `json:"public_key"`
}

func (s *Store) handleControlKey(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if !decodeStrictJSON(w, r, &struct{}{}) {
		return
	}
	writeJSON(w, http.StatusOK, controlKeyResponse{PublicKey: EncodePublicKey(s.ControlPublicKey())})
}

func (s *Store) handleOperationAction(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/edge/v1/operations/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[1] != "complete" || !operationIDPattern.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	var request operationCompletionRequest
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	op, err := s.CompleteOperation(DeviceFromContext(r.Context()).ID, parts[0], request.LeaseID, request.Result, request.SafeCode)
	if err != nil {
		http.Error(w, "operation completion rejected", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Store) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	request, err := decodePairRequest(w, r)
	if err != nil {
		return
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(request.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		http.Error(w, "invalid pairing request", http.StatusBadRequest)
		return
	}
	device, err := s.Pair(request.Code, request.Name, ed25519.PublicKey(publicKey))
	if err != nil {
		http.Error(w, "pairing code is invalid or expired", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(device)
}

func decodePairRequest(w http.ResponseWriter, r *http.Request) (pairRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPairBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request pairRequest
	if err := decoder.Decode(&request); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid pairing request", http.StatusBadRequest)
		}
		return pairRequest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid pairing request", http.StatusBadRequest)
		return pairRequest{}, errors.New("trailing pairing data")
	}
	return request, nil
}

// RequireDevice authenticates a bounded HTTP request using the paired device's
// Ed25519 key and rejects reused nonces before invoking the next handler.
func (s *Store) RequireDevice(next http.Handler) http.Handler {
	return s.requireDevice(next, maxSignedBody)
}

func (s *Store) requireDevice(next http.Handler, maxBody int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "edge authentication failed", http.StatusUnauthorized)
			return
		}
		timestamp, err := strconv.ParseInt(r.Header.Get(HeaderTimestamp), 10, 64)
		signature, signatureErr := base64.RawURLEncoding.DecodeString(r.Header.Get(HeaderSignature))
		if err != nil || signatureErr != nil {
			http.Error(w, "edge authentication failed", http.StatusUnauthorized)
			return
		}
		request := SignedRequest{
			DeviceID:  r.Header.Get(HeaderDevice),
			Timestamp: timestamp,
			Nonce:     r.Header.Get(HeaderNonce),
			Method:    r.Method,
			Path:      r.URL.EscapedPath(),
			Body:      body,
			Signature: signature,
		}
		device, err := s.Authenticate(request)
		if err != nil {
			http.Error(w, "edge authentication failed", http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceContextKey{}, device)))
	})
}

func DeviceFromContext(ctx context.Context) Device {
	device, _ := ctx.Value(deviceContextKey{}).(Device)
	return device
}

func encodeSignature(signature []byte) string {
	return base64.RawURLEncoding.EncodeToString(signature)
}

type leaseRequest struct {
	Workcell     string `json:"workcell"`
	Holder       string `json:"holder"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type heartbeatRequest struct {
	LeaseID      string `json:"lease_id"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type completionRequest struct {
	LeaseID   string  `json:"lease_id"`
	Outcome   Outcome `json:"outcome"`
	Summary   string  `json:"summary"`
	ResultRef string  `json:"result_ref,omitempty"`
}

func (s *Store) handleLease(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var request leaseRequest
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	device := DeviceFromContext(r.Context())
	lease, err := s.LeaseNext(device.ID, request.Workcell, request.Holder, time.Duration(request.LeaseSeconds)*time.Second)
	if errors.Is(err, ErrNoTaskAvailable) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "lease request rejected", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

func (s *Store) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/edge/v1/tasks/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || !taskIDPattern.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	device := DeviceFromContext(r.Context())
	switch parts[1] {
	case "heartbeat":
		var request heartbeatRequest
		if !decodeStrictJSON(w, r, &request) {
			return
		}
		status, err := s.Heartbeat(device.ID, parts[0], request.LeaseID, time.Duration(request.LeaseSeconds)*time.Second)
		if err != nil {
			http.Error(w, "active lease not found", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case "complete":
		var request completionRequest
		if !decodeStrictJSON(w, r, &request) {
			return
		}
		task, err := s.Complete(device.ID, parts[0], request.LeaseID, TaskResult{Outcome: request.Outcome, Summary: request.Summary, ResultRef: request.ResultRef})
		if err != nil {
			http.Error(w, "task completion rejected", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, task)
	default:
		http.NotFound(w, r)
	}
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	w.Header().Set("Allow", http.MethodPost)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type workspaceRegistrationRequest struct {
	Workspaces []WorkspaceRegistration `json:"workspaces"`
}

func (s *Store) handleWorkspaceRegistration(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var request workspaceRegistrationRequest
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	device := DeviceFromContext(r.Context())
	status, err := s.RegisterWorkspaces(device.ID, request.Workspaces)
	if err != nil {
		http.Error(w, "workspace registration rejected", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
