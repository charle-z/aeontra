//go:build opencode_e2e && !windows

package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeControlFailureIsBoundedRedactedAndDoesNotReadMetrics(t *testing.T) {
	source, err := os.ReadFile("opencode_e2e_test.go")
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(source, []byte("if controlErr != nil {"))
	end := bytes.Index(source[start:], []byte("\n\t<-processDone"))
	if start < 0 || end < 0 {
		t.Fatal("OpenCode control failure block not found")
	}
	failureBlock := source[start : start+end]
	if bytes.Contains(failureBlock, []byte("readDriverMetrics")) || bytes.Contains(failureBlock, []byte("/v1/metrics")) {
		t.Fatalf("failure path re-enters the metrics endpoint: %s", failureBlock)
	}

	processDone := make(chan struct{})
	cancelled := make(chan struct{}, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		close(processDone)
	}()
	started := time.Now()
	diagnostic := finalizeOpenCodeControlFailureWithin(
		func() { cancelled <- struct{}{} },
		processDone,
		errors.New("controller failed with SQL SELECT * FROM model_turns"),
		`{"type":"error","error":{"data":{"message":"model turn driver operation failed: response_cas /private/path prompt payload body"}}}`,
		"secret command arguments",
		100*time.Millisecond,
	)
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("failure shutdown was not bounded: %s", elapsed)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("OpenCode process cancellation was not requested")
	}
	if got, want := diagnostic.Error(), "OpenCode control failed: driver_code=response_cas"; got != want {
		t.Fatalf("diagnostic=%q want=%q", got, want)
	}
	for _, forbidden := range []string{"SELECT", "model_turns", "/private/path", "prompt", "payload", "body", "secret", "command"} {
		if strings.Contains(diagnostic.Error(), forbidden) {
			t.Fatalf("diagnostic leaked %q: %q", forbidden, diagnostic)
		}
	}
}

func TestOpenCodeControlFailureUnknownCodeAndShutdownTimeoutAreClosed(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	unknown := finalizeOpenCodeControlFailureWithin(
		func() {},
		closed,
		errors.New("raw SQL and /private/path"),
		"model turn driver operation failed: attacker_payload_123",
		"prompt body",
		time.Millisecond,
	)
	if got, want := unknown.Error(), "OpenCode control failed: driver_code=unknown"; got != want {
		t.Fatalf("unknown diagnostic=%q want=%q", got, want)
	}

	neverDone := make(chan struct{})
	started := time.Now()
	timedOut := finalizeOpenCodeControlFailureWithin(
		func() {},
		neverDone,
		context.DeadlineExceeded,
		"",
		"",
		10*time.Millisecond,
	)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("shutdown timeout was not bounded: %s", elapsed)
	}
	if got, want := timedOut.Error(), "OpenCode control failed: process_shutdown_timeout"; got != want {
		t.Fatalf("timeout diagnostic=%q want=%q", got, want)
	}
}
