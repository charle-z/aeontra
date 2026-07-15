package observability

import (
	"bytes"
	"testing"
)

func TestSummaryAggregatesOnlyNormalizedHTTPRouteMetadata(t *testing.T) {
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{Name: EventHTTPRequest, Route: RouteMCP, StatusCode: 200, DurationMS: 10},
		{Name: EventHTTPRequest, Route: RouteMCP, StatusCode: 404, DurationMS: 20},
		{Name: EventHTTPRequest, Route: RouteMCP, StatusCode: 500, DurationMS: 30},
		{Name: EventHTTPRequest, Route: RouteConsole, StatusCode: 200, DurationMS: 4},
		{Name: EventRPCRequest, Route: RouteMCP, StatusCode: 500, DurationMS: 999},
	} {
		if err := logger.Emit(event); err != nil {
			t.Fatal(err)
		}
	}
	summary := logger.Summary()
	if !summary.Enabled || summary.Failures != 0 || len(summary.Routes) != 2 {
		t.Fatalf("summary=%+v", summary)
	}
	mcp := summary.Routes[0]
	if mcp.Route != RouteMCP || mcp.Requests != 3 || mcp.Client4XX != 1 || mcp.Server5XX != 1 || mcp.P95MS != 30 {
		t.Fatalf("mcp=%+v", mcp)
	}
	console := summary.Routes[1]
	if console.Route != RouteConsole || console.Requests != 1 || console.P95MS != 4 {
		t.Fatalf("console=%+v", console)
	}
}

func TestSummaryBoundsDurationWindowAndHandlesDisabledOrNilLogger(t *testing.T) {
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxRouteDurationSamples+20; index++ {
		if err := logger.Emit(Event{Name: EventHTTPRequest, Route: RouteHealth, StatusCode: 200, DurationMS: int64(index)}); err != nil {
			t.Fatal(err)
		}
	}
	summary := logger.Summary()
	if len(summary.Routes) != 1 || summary.Routes[0].Requests != maxRouteDurationSamples+20 || summary.Routes[0].P95MS <= 0 {
		t.Fatalf("summary=%+v", summary)
	}
	disabled, err := Open(Config{Mode: ModeOff, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got := disabled.Summary(); got.Enabled || len(got.Routes) != 0 {
		t.Fatalf("disabled=%+v", got)
	}
	var nilLogger *Logger
	if got := nilLogger.Summary(); got.Enabled || len(got.Routes) != 0 {
		t.Fatalf("nil summary=%+v", got)
	}
}
