package sandboxexecutor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	var public sandboxprotocol.Error
	if json.Unmarshal(recorder.Body.Bytes(), &public) != nil || public.Code != "invalid_request" || public.Message == "" {
		t.Fatalf("invalid request did not return a semantic error: %s", recorder.Body.String())
	}
}

func TestHandlerMapsWorkspaceAndExecutorFailuresToSemanticErrors(t *testing.T) {
	for name, setup := range map[string]struct {
		mutate func(*Executor, *sandboxprotocol.Request)
		status int
		code   string
	}{
		"secret": {
			mutate: func(executor *Executor, request *sandboxprotocol.Request) {
				if err := os.WriteFile(filepath.Join(executor.config.WorkspaceRoot, ".env"), []byte("TOKEN=x"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			status: http.StatusUnprocessableEntity, code: "workspace_secret_denied",
		},
		"attestation": {
			mutate: func(_ *Executor, _ *sandboxprotocol.Request) {},
			status: http.StatusServiceUnavailable, code: "executor_unavailable",
		},
		"engine": {
			mutate: func(executor *Executor, _ *sandboxprotocol.Request) {
				engine := executor.config.Engine.(*fakeEngine)
				engine.err = errors.New("rootless Podman create failed: podman API returned status 400")
			},
			status: http.StatusBadGateway, code: "executor_failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0}}
			if name == "attestation" {
				engine.attest = errors.New("drift")
			}
			executor := testExecutor(t, engine)
			executor.config.Token = strings.Repeat("t", 32)
			request := validRequest(t, executor)
			setup.mutate(executor, &request)
			request.RequestDigest, _ = sandboxprotocol.Digest(request)
			body, _ := json.Marshal(request)
			httpRequest := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewReader(body))
			httpRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
			recorder := httptest.NewRecorder()
			executor.Handler().ServeHTTP(recorder, httpRequest)
			var public sandboxprotocol.Error
			if recorder.Code != setup.status || json.Unmarshal(recorder.Body.Bytes(), &public) != nil || public.Code != setup.code {
				t.Fatalf("semantic response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHandlerRejectsMalformedJSONWithBoundedSemanticError(t *testing.T) {
	executor := testExecutor(t, &fakeEngine{})
	executor.config.Token = strings.Repeat("t", 32)
	request := httptest.NewRequest(http.MethodPost, "/v1/run", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer "+executor.config.Token)
	recorder := httptest.NewRecorder()
	executor.Handler().ServeHTTP(recorder, request)
	var public sandboxprotocol.Error
	if recorder.Code != http.StatusBadRequest || json.Unmarshal(recorder.Body.Bytes(), &public) != nil ||
		public.Code != "invalid_request" || !sandboxprotocol.ValidError(public) {
		t.Fatalf("malformed request response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestClassifyExecutorErrorDoesNotUseEngineMessageAsPolicy(t *testing.T) {
	status, public := classifyExecutorError(errors.New("podman API returned status 400"), nil)
	if status != http.StatusInternalServerError || public.Code != "internal_error" {
		t.Fatalf("untyped engine failure was classified as a request policy error: %d %#v", status, public)
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
	if statusRecorder.Code != http.StatusOK || json.Unmarshal(statusRecorder.Body.Bytes(), &status) != nil || !status.Available || !status.Rootless ||
		status.ProfileVersion != sandboxprotocol.LegacyProfileVersion {
		t.Fatalf("status response = %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	negotiatedRequest := httptest.NewRequest(http.MethodGet, "/v1/status?profile_version="+sandboxprotocol.ProfileVersion, nil)
	negotiatedRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
	negotiatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(negotiatedRecorder, negotiatedRequest)
	if json.Unmarshal(negotiatedRecorder.Body.Bytes(), &status) != nil || negotiatedRecorder.Code != http.StatusOK ||
		status.ProfileVersion != sandboxprotocol.ProfileVersion {
		t.Fatalf("negotiated status response = %d %s", negotiatedRecorder.Code, negotiatedRecorder.Body.String())
	}

	unsupportedRequest := httptest.NewRequest(http.MethodGet, "/v1/status?profile_version=l3-v99", nil)
	unsupportedRequest.Header.Set("Authorization", "Bearer "+executor.config.Token)
	unsupportedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unsupportedRecorder, unsupportedRequest)
	var public sandboxprotocol.Error
	if json.Unmarshal(unsupportedRecorder.Body.Bytes(), &public) != nil || unsupportedRecorder.Code != http.StatusBadRequest ||
		public.Code != "invalid_request" {
		t.Fatalf("unsupported status response = %d %s", unsupportedRecorder.Code, unsupportedRecorder.Body.String())
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
