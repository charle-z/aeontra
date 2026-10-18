package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestHTTPConsoleFailedLoginEmitsOnlySafeClassification(t *testing.T) {
	server, _, events := newObservedServer(t)
	handler := server.HTTPHandlerWithOptions(testToken, nil, HTTPOptions{})
	secret := "wrong-console-token-customer-secret"
	form := url.Values{"token": {secret}}
	request := httptest.NewRequest(http.MethodPost, "/console/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
	if strings.Contains(events.String(), secret) || strings.Contains(events.String(), testToken) {
		t.Fatalf("login secret leaked to observability: %s", events.String())
	}
	decoded := decodeEvents(t, events.String())
	if len(decoded) != 1 {
		t.Fatalf("events=%+v", decoded)
	}
	event := decoded[0]
	if event.Component != observability.ComponentHTTP || event.Route != observability.RouteConsole || event.Outcome != observability.OutcomeDenied || event.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected event=%+v", event)
	}
}
