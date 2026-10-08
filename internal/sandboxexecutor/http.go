package sandboxexecutor

import (
	"context"
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
	profileVersion := sandboxprotocol.LegacyProfileVersion
	if requested := r.URL.Query().Get("profile_version"); requested != "" {
		if requested != sandboxprotocol.ProfileVersion {
			writeExecutorError(w, http.StatusBadRequest, sandboxprotocol.Error{
				Code: "invalid_request", Message: "sandbox status profile version is unsupported", Retryable: false,
			})
			return
		}
		profileVersion = requested
	}
	ctx, cancel := contextWithMaximum(r, 10*time.Second)
	defer cancel()
	status := e.Status(ctx)
	status.ProfileVersion = profileVersion
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
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
		writeExecutorError(w, http.StatusBadRequest, sandboxprotocol.Error{Code: "invalid_request", Message: "sandbox request body is invalid", Retryable: false})
		return
	}
	response, err := e.Execute(r.Context(), request)
	if err != nil {
		status, public := classifyExecutorError(err, r.Context().Err())
		writeExecutorError(w, status, public)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func writeExecutorError(w http.ResponseWriter, status int, public sandboxprotocol.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(public)
}

func classifyExecutorError(err, contextErr error) (int, sandboxprotocol.Error) {
	if err == nil {
		return http.StatusInternalServerError, sandboxprotocol.Error{
			Code: executorErrInternal.String(), Message: "sandbox executor failed internally", Retryable: true,
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(contextErr, context.DeadlineExceeded):
		return publicExecutorError(http.StatusGatewayTimeout, executorErrTimeout, "sandbox execution exceeded its time limit", true)
	case errors.Is(err, context.Canceled), errors.Is(contextErr, context.Canceled):
		return publicExecutorError(http.StatusServiceUnavailable, executorErrUnavailable, "sandbox execution was cancelled", true)
	default:
		code, ok := executorErrorCodeOf(err)
		if !ok {
			return publicExecutorError(http.StatusInternalServerError, executorErrInternal, "sandbox executor failed internally", true)
		}
		switch code {
		case executorErrInvalidRequest:
			return publicExecutorError(http.StatusBadRequest, code, "sandbox request body is invalid", false)
		case executorErrRequestPolicy:
			return publicExecutorError(http.StatusBadRequest, code, "sandbox request violates the configured execution policy", false)
		case executorErrWorkspaceSelection:
			return publicExecutorError(http.StatusBadRequest, code, "sandbox workspace selection is invalid or unavailable", false)
		case executorErrWorkingDirectory:
			return publicExecutorError(http.StatusBadRequest, code, "sandbox working directory is invalid or unavailable", false)
		case executorErrWorkspaceSecret:
			return publicExecutorError(http.StatusUnprocessableEntity, code, "selected workspace contains a policy-denied secret path", false)
		case executorErrWorkspaceUnavailable:
			return publicExecutorError(http.StatusServiceUnavailable, code, "selected sandbox workspace is temporarily unavailable", true)
		case executorErrConflict:
			return publicExecutorError(http.StatusConflict, code, "sandbox request conflicts with an existing durable receipt", false)
		case executorErrUnavailable:
			return publicExecutorError(http.StatusServiceUnavailable, code, "sandbox executor is unavailable or failed attestation", true)
		case executorErrFailed:
			return publicExecutorError(http.StatusBadGateway, code, "sandbox engine failed to execute the request", true)
		case executorErrTimeout:
			return publicExecutorError(http.StatusGatewayTimeout, code, "sandbox execution exceeded its time limit", true)
		case executorErrInternal:
			return publicExecutorError(http.StatusInternalServerError, code, "sandbox executor failed internally", true)
		default:
			return publicExecutorError(http.StatusInternalServerError, executorErrInternal, "sandbox executor failed internally", true)
		}
	}
}

func publicExecutorError(status int, code executorErrorCode, message string, retryable bool) (int, sandboxprotocol.Error) {
	return status, sandboxprotocol.Error{Code: code.String(), Message: message, Retryable: retryable}
}
