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
func NewHTTPHandler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PairPath, store.handlePair)
	return mux
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSignedBody)
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
