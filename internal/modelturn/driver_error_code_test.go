package modelturn

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDriverInternalErrorCodeIsClosedAndRedacted(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "none"},
		{errors.New("model turn transaction failed"), "transaction_begin"},
		{errors.New("model body cleanup failed"), "cleanup"},
		{errors.New("model turn quota check failed"), "quota_read"},
		{errors.New("model turn quota eviction failed"), "quota_eviction"},
		{errors.New("model request body persistence failed"), "request_persist"},
		{errors.New("model turn commit failed"), "commit"},
		{errors.New("model runtime persistence failed"), "runtime_persist"},
		{errors.New("model runtime activation failed"), "runtime_activate"},
		{errors.New("model runtime read failed"), "runtime_read"},
		{errors.New("model runtime turn read failed"), "runtime_turn_read"},
		{errors.New("model turn sequence read failed"), "sequence_read"},
		{errors.New("model request body read failed"), "request_read"},
		{errors.New("model request reference binding check failed"), "reference_binding_read"},
		{errors.New("model turn persistence failed"), "turn_persist"},
		{errors.New("model turn activation failed"), "turn_activate"},
		{errors.New("model runtime turn binding failed"), "runtime_bind"},
		{errors.New("model turn read failed"), "turn_read"},
		{errors.New("model response transaction failed"), "response_transaction"},
		{errors.New("model response persistence failed"), "response_persist"},
		{errors.New("model response compare-and-swap failed"), "response_cas"},
		{errors.New("model response commit failed"), "response_commit"},
		{errors.New("model response read failed"), "response_read"},
		{errors.New("model response body unavailable"), "response_body"},
		{errors.New("model response consume failed"), "response_consume"},
		{errors.New("model turn cancellation failed"), "turn_cancel"},
		{errors.New("model turn failure transition failed"), "turn_fail"},
		{errors.New("model turn disconnect failed"), "turn_disconnect"},
		{errors.New("model runtime resume failed"), "runtime_resume"},
		{errors.New("model runtime transition failed"), "runtime_transition"},
		{errors.New("model runtime heartbeat failed"), "runtime_heartbeat"},
		{errors.New("model runtime result update failed"), "runtime_result"},
		{errors.New("model runtime cancellation commit failed"), "runtime_cancel"},
		{errors.New("model turn id generation failed"), "id_generation"},
		{errors.New("model body statistics failed"), "statistics"},
		{errors.New("unexpected private path /home/user/repo"), "unknown"},
	}
	for _, test := range cases {
		got := driverInternalErrorCode(test.err)
		if got != test.want {
			t.Fatalf("error=%v code=%q want=%q", test.err, got, test.want)
		}
		if strings.Contains(got, "/home/") || strings.Contains(got, "private") {
			t.Fatalf("driver error code leaked details: %q", got)
		}
	}
}

func TestNormalizeDriverInternalErrorCodeRejectsUntrustedValues(t *testing.T) {
	for code := range safeDriverInternalErrorCodes {
		if got := NormalizeDriverInternalErrorCode(code); got != code {
			t.Fatalf("allowlisted code=%q normalized=%q", code, got)
		}
	}
	for _, value := range []string{
		"", "response_cas/private", "SELECT model_turns", "payload", "prompt", "body", "../../secret", "response-cas",
	} {
		if got := NormalizeDriverInternalErrorCode(value); got != "unknown" {
			t.Fatalf("untrusted code=%q normalized=%q", value, got)
		}
	}
}

func TestDriverInternalErrorResponseContainsOnlySafeCode(t *testing.T) {
	driver := &Driver{}
	driver.lastInternalErrorCode.Store("")
	recorder := httptest.NewRecorder()
	driver.writeStoreError(recorder, errors.New("model runtime read failed"))
	if recorder.Code != 500 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response driverError
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "internal_runtime_read" || response.Error != "model turn driver operation failed: runtime_read" {
		t.Fatalf("response=%+v", response)
	}
	if got := driver.lastInternalErrorCode.Load().(string); got != "runtime_read" {
		t.Fatalf("last code=%q", got)
	}
}
