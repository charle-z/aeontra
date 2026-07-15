package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPayloadCountersMeasureAuthenticatedMCPBodiesOnly(t *testing.T) {
	server, _, _ := newObservedServer(t)
	handler := server.HTTPHandler(testToken, nil)

	unauthorizedBody := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(unauthorizedBody)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	if got := server.payload.snapshot(); got != (payloadSnapshot{}) {
		t.Fatalf("unauthorized request counted: %+v", got)
	}

	authorizedBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	request := httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(authorizedBody))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", response.Code, response.Body.String())
	}
	got := server.payload.snapshot()
	if got.RequestCount != 1 || got.InputBytes != uint64(len(authorizedBody)) || got.OutputBytes != uint64(response.Body.Len()) {
		t.Fatalf("payload=%+v response=%d", got, response.Body.Len())
	}

	notificationBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	notification := httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(notificationBody))
	notification.Header.Set("Authorization", "Bearer "+testToken)
	notificationResponse := httptest.NewRecorder()
	handler.ServeHTTP(notificationResponse, notification)
	if notificationResponse.Code != http.StatusAccepted {
		t.Fatalf("notification status=%d", notificationResponse.Code)
	}
	got = server.payload.snapshot()
	if got.RequestCount != 2 || got.InputBytes != uint64(len(authorizedBody)+len(notificationBody)) || got.OutputBytes != uint64(response.Body.Len()) {
		t.Fatalf("payload after notification=%+v", got)
	}
}

func TestPayloadCountersHandleNilAndIgnoreNegativeLengths(t *testing.T) {
	var nilCounters *payloadCounters
	nilCounters.record(10, 10)
	if got := nilCounters.snapshot(); got != (payloadSnapshot{}) {
		t.Fatalf("nil snapshot=%+v", got)
	}
	var counters payloadCounters
	counters.record(-1, -1)
	if got := counters.snapshot(); got.RequestCount != 1 || got.InputBytes != 0 || got.OutputBytes != 0 {
		t.Fatalf("negative snapshot=%+v", got)
	}
}
