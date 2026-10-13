package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestTransportErrorClassIsClosedAndPreservesCauses(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "none"}, {context.Canceled, "cancelled"},
		{&edgeEndpointUnavailableError{cause: context.DeadlineExceeded}, "timeout"},
		{&edgeEndpointUnavailableError{cause: &net.DNSError{IsTimeout: true, Name: "private.example"}}, "timeout"},
		{&edgeEndpointUnavailableError{cause: errors.New("secret socket")}, "unavailable"},
		{&operationHTTPError{phase: "progress", status: 401}, "authentication"},
		{&operationHTTPError{phase: "progress", status: 403}, "authentication"},
		{&operationHTTPError{phase: "progress", status: 409}, "conflict"},
		{&operationHTTPError{phase: "completion", status: 429}, "rate_limited"},
		{&operationHTTPError{phase: "completion", status: 503}, "server_error"},
		{&operationHTTPError{phase: "completion", status: 400}, "http_rejected"},
		{fmt.Errorf("wrapped: %w", errEdgeResponseInvalid), "invalid_response"},
		{errors.New("arbitrary secret content"), "unknown"},
	}
	for _, c := range cases {
		if got := TransportErrorClass(c.err); got != c.want {
			t.Errorf("got %s want %s", got, c.want)
		}
	}
}
