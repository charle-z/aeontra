package mcpserver

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestReconnectObservabilityDistinguishesSessionLifecycleWithoutIdentifiers(t *testing.T) {
	server, _, output := newObservedServer(t)
	telemetry := newHTTPTransportTelemetry()
	handler := server.httpHandlerWithRuntime(testToken, nil, HTTPOptions{}, newHTTPServerLifecycle(), newHTTPSessionStore(defaultHTTPSessionTTL), telemetry)

	first := do(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, rpcBody(t, 1, "initialize", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first initialize status=%d body=%s", first.Code, first.Body.String())
	}
	firstSession := first.Header().Get("Mcp-Session-Id")
	if firstSession == "" {
		t.Fatal("first initialize returned no session")
	}

	const oldSession = "previous-instance-session-value"
	rejected := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, oldSession, rpcBody(t, 2, "tools/list", nil))
	if rejected.Code != http.StatusNotFound {
		t.Fatalf("old session status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	reinitialized := doWithHeaders(t, handler, http.MethodPost, DefaultMCPPath, rpcBody(t, 3, "initialize", nil), map[string]string{
		"Authorization":  "Bearer " + testToken,
		"Mcp-Session-Id": oldSession,
	})
	if reinitialized.Code != http.StatusOK {
		t.Fatalf("reinitialize status=%d body=%s", reinitialized.Code, reinitialized.Body.String())
	}
	secondSession := reinitialized.Header().Get("Mcp-Session-Id")
	if secondSession == "" || secondSession == firstSession || secondSession == oldSession {
		t.Fatalf("reinitialize session first=%q old=%q second=%q", firstSession, oldSession, secondSession)
	}

	const authSecret = "authorization-secret-must-not-appear"
	unauthorized := do(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+authSecret, rpcBody(t, 4, "initialize", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	text := output.String()
	for _, forbidden := range []string{firstSession, secondSession, oldSession, authSecret, "Mcp-Session-Id", "Authorization"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("observability leaked %q: %s", forbidden, text)
		}
	}

	events := decodeEvents(t, text)
	var created, rejectedEvent, reinitializedEvent, authFailure bool
	for _, event := range events {
		if event.BootID == "" || event.BootID == "redacted" {
			t.Fatalf("event lacks safe boot id: %+v", event)
		}
		switch event.Name {
		case observability.EventMCPSessionCreated:
			created = event.Outcome == observability.OutcomeSuccess && event.CatalogHash != "" && event.Commit != ""
		case observability.EventMCPSessionRejected:
			rejectedEvent = event.ErrorClass == observability.ErrorSessionUnknown && event.Outcome == observability.OutcomeDenied
		case observability.EventMCPSessionReinitialized:
			reinitializedEvent = event.ReconnectCount == 1 && event.Outcome == observability.OutcomeSuccess
		case observability.EventHTTPRequest:
			if event.StatusCode == http.StatusUnauthorized {
				authFailure = event.ErrorClass == observability.ErrorAuthentication && event.Outcome == observability.OutcomeDenied
			}
		}
	}
	if !created || !rejectedEvent || !reinitializedEvent || !authFailure {
		t.Fatalf("missing lifecycle classifications created=%v rejected=%v reinitialized=%v auth=%v events=%+v", created, rejectedEvent, reinitializedEvent, authFailure, events)
	}
}

func TestDrainObservabilityCarriesBoundedDurationAndAggregateOnly(t *testing.T) {
	server, _, output := newObservedServer(t)
	telemetry := newHTTPTransportTelemetry()
	telemetry.reconnections.Add(2)
	lifecycle := newHTTPServerLifecycle().WithObserver(func(name observability.EventName, duration time.Duration, outcome observability.Outcome, errorClass observability.ErrorClass) {
		server.emitHTTPDrainEvent(name, duration, outcome, errorClass, telemetry.ReconnectCount())
	})

	lifecycle.BeginDrain()
	time.Sleep(time.Millisecond)
	lifecycle.EndDrain(observability.OutcomeSuccess, observability.ErrorNone)
	lifecycle.EndDrain(observability.OutcomeError, observability.ErrorTransport)

	events := decodeEvents(t, output.String())
	if len(events) != 2 {
		t.Fatalf("drain events=%d want=2: %+v", len(events), events)
	}
	if events[0].Name != observability.EventServerDrainStart || events[0].ReconnectCount != 2 || events[0].DurationMS != 0 {
		t.Fatalf("drain start=%+v", events[0])
	}
	if events[1].Name != observability.EventServerDrainEnd || events[1].ReconnectCount != 2 || events[1].DurationMS < 0 || events[1].Outcome != observability.OutcomeSuccess {
		t.Fatalf("drain end=%+v", events[1])
	}
	if events[0].BootID == "" || events[0].BootID != events[1].BootID {
		t.Fatalf("drain boot ids=%q %q", events[0].BootID, events[1].BootID)
	}
}
