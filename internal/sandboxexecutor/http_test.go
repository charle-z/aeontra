package sandboxexecutor

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

func TestHandlerRequiresBearerAndStrictSchema(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0}}
	executor := testExecutor(t, engine)
	executor.config.Token = strings.Repeat("t", 32)
	handler := executor.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := validRequest(t, executor)
	body, _ := json.Marshal(request)
	body = bytes.TrimSuffix(body, []byte("}"))
	body = append(body, []byte(`,"unexpected":true}`)...)
	httpRequest := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewReader(body))
	httpRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpRequest)
	if recorder.Code != http.StatusBadRequest || engine.runs != 0 {
		t.Fatalf("unknown field was accepted: status=%d runs=%d body=%q", recorder.Code, engine.runs, recorder.Body.String())
	}
}

func TestHandlerReturnsAttestedStatusAndBoundedResult(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0, Stdout: "ok"}}
	executor := testExecutor(t, engine)
	executor.config.Token = strings.Repeat("t", 32)
	handler := executor.Handler()

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	var status sandboxprotocol.Status
	if statusRecorder.Code != http.StatusOK || json.Unmarshal(statusRecorder.Body.Bytes(), &status) != nil || !status.Available || !status.Rootless {
		t.Fatalf("status response = %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	request := validRequest(t, executor)
	body, _ := json.Marshal(request)
	httpRequest := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewReader(body)).WithContext(context.Background())
	httpRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpRequest)
	var response sandboxprotocol.Response
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil || response.Stdout != "ok" {
		t.Fatalf("run response = %d %s", recorder.Code, recorder.Body.String())
	}
}
