package sandboxexecutor

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const maxRequestBodyBytes = 512 << 10

func (e *Executor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", e.handleStatus)
	mux.HandleFunc("/v1/run", e.handleRun)
	return mux
}

func (e *Executor) authorized(header string) bool {
	prefix := "Bearer "
	if !strings.HasPrefix(header, prefix) || e.config.Token == "" {
		return false
	}
	provided := []byte(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	expected := []byte(e.config.Token)
	return len(provided) == len(expected) && subtle.ConstantTimeCompare(provided, expected) == 1
}

func (e *Executor) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !e.authorized(r.Header.Get("Authorization")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := contextWithMaximum(r, 10*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e.Status(ctx))
}

func (e *Executor) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !e.authorized(r.Header.Get("Authorization")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var request sandboxprotocol.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	response, err := e.Execute(r.Context(), request)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "indeterminate") || strings.Contains(err.Error(), "conflicts") {
			status = http.StatusConflict
		} else if errors.Is(err, r.Context().Err()) || strings.Contains(err.Error(), "attestation") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, "sandbox request rejected", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
