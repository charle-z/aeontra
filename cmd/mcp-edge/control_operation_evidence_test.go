package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestControlCompletionRequiresExecutorCompletedMarker(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("initial/running progress failed")} {
		if controlResultCanComplete(err) {
			t.Fatalf("allowed incomplete execution: %v", err)
		}
	}
	if !controlResultCanComplete(nil) || !controlResultCanComplete(&controlFinalizingError{cause: context.DeadlineExceeded}) {
		t.Fatal("completed result unavailable")
	}
}

func TestControlFailureEvidenceDoesNotExposeRawContent(t *testing.T) {
	var b bytes.Buffer
	logControlOperationFailure(&b, "finalizing", "eo_0123456789abcdef0123456789abcdef", &controlFinalizingError{cause: context.DeadlineExceeded})
	if !strings.Contains(b.String(), "phase=finalizing operation=eo_0123456789abcdef0123456789abcdef error_class=timeout") {
		t.Fatal(b.String())
	}
	b.Reset()
	logControlOperationFailure(&b, "secret-phase", "secret-id", errors.New("secret-token https://private.invalid/path"))
	if strings.Contains(b.String(), "secret") || strings.Contains(b.String(), "private.invalid") || !strings.Contains(b.String(), "phase=unknown operation=unknown error_class=unknown") {
		t.Fatal(b.String())
	}
}
