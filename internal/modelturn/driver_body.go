package modelturn

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const requestDigestHeader = "X-MCP-Request-Digest"
const requestTTLHeader = "X-MCP-TTL-Ms"

func (d *Driver) stageRequestBody(w http.ResponseWriter, r *http.Request) {
	d.stageCalls.Add(1)
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		d.writeError(w, http.StatusBadRequest, "invalid_request", errors.New("content type must be application/json"))
		return
	}
	expectedDigest := strings.TrimSpace(r.Header.Get(requestDigestHeader))
	if !validDigest(expectedDigest) {
		d.writeError(w, http.StatusBadRequest, "invalid_request", ErrInvalidRequest)
		return
	}
	ttl, err := parseDriverTTL(r.Header.Get(requestTTLHeader))
	if err != nil {
		d.writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	reader := http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	d.bytesReceived.Add(uint64(len(body)))
	if err != nil {
		d.writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", ErrBodyTooLarge)
		return
	}
	if int64(len(body)) <= MaxInlineRequestBytes || int64(len(body)) > MaxRequestBodyBytes {
		d.writeError(w, http.StatusBadRequest, "invalid_request", ErrBodyTooLarge)
		return
	}
	if digestBytes(body) != expectedDigest {
		d.writeError(w, http.StatusConflict, "digest_mismatch", ErrSequenceMismatch)
		return
	}
	reference, err := d.store.StageRequestBody(r.Context(), body, true, ttl)
	if err != nil {
		d.writeStoreError(w, err)
		return
	}
	if reference.RequestDigest != expectedDigest {
		d.writeError(w, http.StatusConflict, "digest_mismatch", ErrSequenceMismatch)
		return
	}
	d.writeJSON(w, http.StatusCreated, reference)
}

func parseDriverTTL(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultTurnTTL, nil
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidRequest
	}
	ttl := time.Duration(milliseconds) * time.Millisecond
	if ttl <= 0 || ttl > MaxTurnTTL {
		return 0, ErrInvalidRequest
	}
	return ttl, nil
}
