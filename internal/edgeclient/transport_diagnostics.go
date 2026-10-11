package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

var errEdgeResponseInvalid = errors.New("edge response is invalid")

type operationHTTPError struct {
	phase  string
	status int
}

func (e *operationHTTPError) Error() string {
	return fmt.Sprintf("edge operation %s rejected with HTTP %d", e.phase, e.status)
}

// TransportErrorClass returns only a closed, content-free diagnostic category.
// It never returns a URL, server response, request, credential or raw error.
func TransportErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	var rejected *operationHTTPError
	if errors.As(err, &rejected) {
		switch {
		case rejected.status == http.StatusUnauthorized || rejected.status == http.StatusForbidden:
			return "authentication"
		case rejected.status == http.StatusConflict:
			return "conflict"
		case rejected.status == http.StatusTooManyRequests:
			return "rate_limited"
		case rejected.status >= 500:
			return "server_error"
		default:
			return "http_rejected"
		}
	}
	if errors.Is(err, errEdgeResponseInvalid) {
		return "invalid_response"
	}
	var unavailable *edgeEndpointUnavailableError
	if errors.As(err, &unavailable) {
		return "unavailable"
	}
	return "unknown"
}
