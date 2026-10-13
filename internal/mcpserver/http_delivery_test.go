package mcpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestHTTPDeliveryObservability(t *testing.T) {
	writeErr := errors.New("private write failure /private/response token-secret")
	for _, test := range []struct {
		name         string
		status       int
		writeErr     error
		shortWrite   bool
		recoverWrite bool
		cancel       bool
		expired      bool
		empty        bool
		outcome      observability.Outcome
		errorClass   observability.ErrorClass
	}{
		{name: "successful write", status: http.StatusOK, outcome: observability.OutcomeSuccess},
		{name: "successful empty accepted", status: http.StatusAccepted, empty: true, outcome: observability.OutcomeAccepted},
		{name: "explicit write error after 200", status: http.StatusOK, writeErr: writeErr, outcome: observability.OutcomeError, errorClass: observability.ErrorTransport},
		{name: "implicit 200 write error", writeErr: writeErr, outcome: observability.OutcomeError, errorClass: observability.ErrorTransport},
		{name: "write error after 202", status: http.StatusAccepted, writeErr: writeErr, outcome: observability.OutcomeError, errorClass: observability.ErrorTransport},
		{name: "short write without error", status: http.StatusOK, shortWrite: true, outcome: observability.OutcomeError, errorClass: observability.ErrorTransport},
		{name: "first failure survives later success", status: http.StatusOK, writeErr: writeErr, recoverWrite: true, outcome: observability.OutcomeError, errorClass: observability.ErrorTransport},
		{name: "cancelled after write", status: http.StatusOK, cancel: true, outcome: observability.OutcomeCancelled, errorClass: observability.ErrorTransport},
		{name: "cancelled empty accepted", status: http.StatusAccepted, empty: true, cancel: true, outcome: observability.OutcomeCancelled, errorClass: observability.ErrorTransport},
		{name: "cancelled with write failure", status: http.StatusOK, writeErr: writeErr, cancel: true, outcome: observability.OutcomeCancelled, errorClass: observability.ErrorTransport},
		{name: "expired request deadline", status: http.StatusOK, expired: true, outcome: observability.OutcomeCancelled, errorClass: observability.ErrorTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, _, events := newObservedServer(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.expired {
				deadlineCtx, deadlineCancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer deadlineCancel()
				ctx = deadlineCtx
			}
			writes := 0
			writer := &httpDeliveryTestWriter{ResponseRecorder: httptest.NewRecorder()}
			writer.write = func(data []byte) (int, error) {
				writes++
				if test.recoverWrite && writes > 1 {
					return len(data), nil
				}
				if test.writeErr != nil {
					return 0, test.writeErr
				}
				if test.shortWrite {
					return len(data) - 1, nil
				}
				return len(data), nil
			}
			handler := server.withHTTPObservability(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				if !test.empty {
					_, _ = w.Write([]byte("private-response-payload"))
					if test.recoverWrite {
						_, _ = w.Write([]byte("later-payload"))
					}
				}
				if test.cancel {
					cancel()
				}
			}))
			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, DefaultMCPPath, nil).WithContext(ctx))
			decoded := decodeEvents(t, events.String())
			if len(decoded) != 1 {
				t.Fatalf("events = %+v", decoded)
			}
			event := decoded[0]
			if event.Name != observability.EventHTTPRequest || event.Outcome != test.outcome || event.ErrorClass != test.errorClass {
				t.Fatalf("delivery outcome = %+v; want %s/%s", event, test.outcome, test.errorClass)
			}
			wantStatus := test.status
			if wantStatus == 0 {
				wantStatus = http.StatusOK
			}
			if event.StatusCode != wantStatus || writer.Code != wantStatus {
				t.Fatalf("status changed: event=%d response=%d want=%d", event.StatusCode, writer.Code, wantStatus)
			}
			wantLevel := observability.LevelInfo
			if test.errorClass != observability.ErrorNone {
				wantLevel = observability.LevelError
			}
			if event.Level != wantLevel {
				t.Fatalf("event level = %s; want %s", event.Level, wantLevel)
			}
			for _, forbidden := range []string{writeErr.Error(), "/private/response", "token-secret", "private-response-payload", "later-payload"} {
				if strings.Contains(events.String(), forbidden) {
					t.Fatalf("delivery event leaked %q", forbidden)
				}
			}
		})
	}
}

func TestHTTPDeliveryWriterPreservesWriteResults(t *testing.T) {
	writeErr := errors.New("write failed")
	for _, test := range []struct {
		name string
		n    int
		err  error
		want error
	}{
		{name: "success", n: 4},
		{name: "partial failure", n: 2, err: writeErr, want: writeErr},
		{name: "short write", n: 2, want: io.ErrShortWrite},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &httpDeliveryTestWriter{
				ResponseRecorder: httptest.NewRecorder(),
				write:            func([]byte) (int, error) { return test.n, test.err },
			}
			recorder := &observabilityResponseWriter{ResponseWriter: writer}
			n, err := recorder.Write([]byte("body"))
			if n != test.n || err != test.err {
				t.Fatalf("Write = %d, %v; want %d, %v", n, err, test.n, test.err)
			}
			if recorder.writeErr != test.want {
				t.Fatalf("recorded failure = %v; want %v", recorder.writeErr, test.want)
			}
			if test.want != nil {
				writer.write = func([]byte) (int, error) { return 0, errors.New("later failure") }
				_, _ = recorder.Write([]byte("body"))
				if recorder.writeErr != test.want {
					t.Fatal("later failure replaced first write failure")
				}
			}
		})
	}
}

func TestHTTPDeliveryWriterPreservesFlushAndResponseController(t *testing.T) {
	writer := &httpDeliveryTestWriter{ResponseRecorder: httptest.NewRecorder()}
	var wrapped http.ResponseWriter = &observabilityResponseWriter{ResponseWriter: writer}
	flusher, ok := wrapped.(http.Flusher)
	if !ok {
		t.Fatal("HTTP Flusher not preserved")
	}
	flusher.Flush()
	if writer.Code != http.StatusOK || !writer.Flushed {
		t.Fatal("Flush did not reach underlying writer with implicit 200")
	}
	controller := http.NewResponseController(wrapped)
	deadline := time.Now().Add(time.Second)
	if err := controller.SetWriteDeadline(deadline); err != nil {
		t.Fatalf("ResponseController deadline: %v", err)
	}
	if !writer.deadline.Equal(deadline) {
		t.Fatal("ResponseController did not unwrap writer")
	}
	if err := controller.Flush(); err != nil {
		t.Fatalf("ResponseController flush: %v", err)
	}
}

func TestHTTPDeliveryPostResponseWriteFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "single request", body: `{"jsonrpc":"2.0","id":2,"method":"ping"}`},
		{name: "batch", body: `[{"jsonrpc":"2.0","id":2,"method":"ping"},{"jsonrpc":"2.0","id":3,"method":"ping"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, _, events := newObservedServer(t)
			handler := server.HTTPHandler(testToken, nil)
			sessionID := initializeHandlerSession(t, handler, "Bearer "+testToken)
			events.Reset()
			writeErr := errors.New("private POST write error")
			writer := &httpDeliveryTestWriter{
				ResponseRecorder: httptest.NewRecorder(),
				write:            func([]byte) (int, error) { return 0, writeErr },
			}
			request := httptest.NewRequest(http.MethodPost, DefaultMCPPath, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+testToken)
			request.Header.Set("Mcp-Session-Id", sessionID)
			handler.ServeHTTP(writer, request)
			decoded := decodeEvents(t, events.String())
			event := decoded[len(decoded)-1]
			if event.Name != observability.EventHTTPRequest || event.StatusCode != http.StatusOK || event.Outcome != observability.OutcomeError || event.ErrorClass != observability.ErrorTransport {
				t.Fatalf("POST delivery outcome = %+v", event)
			}
			if strings.Contains(events.String(), writeErr.Error()) {
				t.Fatal("POST write error leaked")
			}
		})
	}
}

func TestHTTPDeliverySSECompletion(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		name := "cancelled stream"
		if writeFails {
			name = "failed stream write"
		}
		t.Run(name, func(t *testing.T) {
			server, _, events := newObservedServer(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &httpDeliveryTestWriter{ResponseRecorder: httptest.NewRecorder()}
			writer.write = func(data []byte) (int, error) {
				if writeFails {
					return 0, errors.New("private SSE write error")
				}
				cancel()
				return len(data), nil
			}
			handler := server.withHTTPObservability(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handleHTTPGetSSE(w, r, newHTTPServerLifecycle())
			}))
			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, DefaultMCPPath, nil).WithContext(ctx))
			event := decodeEvents(t, events.String())[0]
			wantOutcome := observability.OutcomeCancelled
			if writeFails {
				wantOutcome = observability.OutcomeError
			}
			if event.Outcome != wantOutcome || event.ErrorClass != observability.ErrorTransport || event.StatusCode != http.StatusOK {
				t.Fatalf("SSE delivery outcome = %+v", event)
			}
			if !writeFails && !writer.Flushed {
				t.Fatal("SSE chunk was not flushed")
			}
		})
	}
}

type httpDeliveryTestWriter struct {
	*httptest.ResponseRecorder
	write    func([]byte) (int, error)
	deadline time.Time
}

func (w *httpDeliveryTestWriter) Write(data []byte) (int, error) {
	return w.write(data)
}

func (w *httpDeliveryTestWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
