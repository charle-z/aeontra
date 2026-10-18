package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestStartupDiagnosticOmitsRootsPathsAddressesAndAuthDetails(t *testing.T) {
	runtime := &appRuntime{
		PrimaryRoot: "/private/repos/customer-secret",
		AuditPath:   "/private/state/audit.jsonl",
	}
	for _, transport := range []transportConfig{
		{Mode: transportStdio},
		{Mode: transportHTTP, Addr: "10.20.30.40:8765", AuthDescription: "OAuth private@example.com"},
	} {
		line := startupDiagnostic(runtime, transport, "1.2.3")
		for _, forbidden := range []string{"customer-secret", "/private", "10.20.30.40", "private@example.com", "audit.jsonl"} {
			if strings.Contains(line, forbidden) {
				t.Fatalf("diagnostic leaked %q: %s", forbidden, line)
			}
		}
		for _, required := range []string{"mcp-devbox", "1.2.3", "root_count="} {
			if !strings.Contains(line, required) {
				t.Fatalf("diagnostic missing %q: %s", required, line)
			}
		}
	}
}

func TestLifecycleEventsContainOnlyPublicIdentityAndCounts(t *testing.T) {
	var output bytes.Buffer
	observer, err := observability.Open(observability.Config{Mode: observability.ModeStderr, MaxBytes: observability.DefaultMaxBytes}, &output)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &appRuntime{Observer: observer, PrimaryRoot: "/private/root", AuditPath: "/private/audit.jsonl"}
	runtime.Server = nil

	emitLifecycleEvent(runtime, observability.EventServerStart, observability.TransportHTTP, observability.OutcomeSuccess, observability.ErrorNone)
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, forbidden := range []string{"/private/root", "/private/audit.jsonl", "path", "repo", "target"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("lifecycle event leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"event":"server_start"`, `"transport":"http"`, `"root_count":1`} {
		if !strings.Contains(text, required) {
			t.Fatalf("lifecycle event missing %q: %s", required, text)
		}
	}
}
