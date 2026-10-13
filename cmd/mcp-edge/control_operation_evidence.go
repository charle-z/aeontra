package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

// Only the executor-completed path constructs this error. A missing progress
// acknowledgement must not discard its result. Completion still requires the
// original live, uncancelled server lease; no command is replayed.
type controlFinalizingError struct{ cause error }

func (e *controlFinalizingError) Error() string {
	return "control operation finalizing acknowledgement failed"
}
func (e *controlFinalizingError) Unwrap() error { return e.cause }

func controlResultCanComplete(err error) bool {
	var finalizing *controlFinalizingError
	return err == nil || errors.As(err, &finalizing)
}

var controlEvidenceOperationID = regexp.MustCompile(`^eo_[0-9a-f]{32}$`)

func logControlOperationFailure(w io.Writer, phase, operationID string, err error) {
	switch phase {
	case "polling", "progress", "finalizing", "completion", "cancellation", "registration":
	default:
		phase = "unknown"
	}
	if !controlEvidenceOperationID.MatchString(operationID) {
		operationID = "unknown"
	}
	fmt.Fprintf(w, "mcp-edge: control operation failed phase=%s operation=%s error_class=%s\n", phase, operationID, edgeclient.TransportErrorClass(err))
}
