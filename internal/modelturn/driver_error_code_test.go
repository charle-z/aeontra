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
		{errors.New("model runtime activation failed"), "runtime_activate"},
		{errors.New("model runtime read failed"), "runtime_read"},
		{errors.New("model runtime turn read failed"), "runtime_turn_read"},
		{errors.New("model turn sequence read failed"), "sequence_read"},
		{errors.New("model request body read failed"), "request_read"},
		{errors.New("model request reference binding check failed"), "reference_binding_read"},
		{errors.New("model turn persistence failed"), "turn_persist"},
		{errors.New("model runtime turn binding failed"), "runtime_bind"},
		{errors.New("model response consume failed"), "response_consume"},
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
