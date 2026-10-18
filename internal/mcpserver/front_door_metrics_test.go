package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/frontdoor"
)

type continuityFrontDoorMetrics struct {
	AdmissionWaits       int64 `json:"admission_waits"`
	AdmissionRecoveries  int64 `json:"admission_recoveries"`
	AdmissionTimeouts    int64 `json:"admission_timeouts"`
	SSEReconnects        int64 `json:"sse_reconnects"`
	SSEReconnectFailures int64 `json:"sse_reconnect_failures"`
}

func requireFrontDoorRecoveryMetrics(t *testing.T, door *frontdoor.FrontDoor) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/front-door/version", nil))
		var metrics continuityFrontDoorMetrics
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &metrics) != nil {
			t.Fatalf("front-door metrics status=%d body=%s", response.Code, response.Body.String())
		}
		if metrics.AdmissionWaits > 0 && metrics.AdmissionRecoveries > 0 && metrics.SSEReconnects > 0 {
			if metrics.AdmissionTimeouts != 0 || metrics.SSEReconnectFailures != 0 {
				t.Fatalf("front-door failure metrics=%+v", metrics)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("front-door recovery metrics did not converge: %+v", metrics)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
