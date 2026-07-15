package modelturn

import (
	"errors"
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
