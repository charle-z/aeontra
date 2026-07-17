package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestDataEndpointUsesExactNestedAllowlist(t *testing.T) {
	provider := func(context.Context) (DataSnapshot, error) {
		return DataSnapshot{
			System:  SystemData{Available: true, CPUCount: 2, MemoryTotalBytes: 4 << 30, MemoryAvailableBytes: 2 << 30, DiskTotalBytes: 80 << 30, DiskAvailableBytes: 40 << 30, Load1: 0.1, Load5: 0.2, Load15: 0.3},
			Payload: PayloadData{ProcessStartedAt: "2026-07-17T12:00:00Z", RequestCount: 3, ToolCallCount: 2, InputBytes: 400, OutputBytes: 200, EstimatedPayloadTokens: 150, InputTokensEstimate: 100, OutputTokensEstimate: 50, Formula: "bytes / 4 (estimate)", Warning: "estimate, not provider billing"},
			DurableActivity: DurableActivityData{
				Last24Hours: ActivityWindowData{Requests: 3, ToolCalls: 2, InputBytes: 400, OutputBytes: 200, EstimatedPayloadTokens: 150, ClientErrors: 1, ExternalWaitMS: 9, UpdatedAt: "2026-07-17T12:00:00Z"},
				Lifetime:    ActivityWindowData{Requests: 10},
			},
			Controllers:   []ControllerData{{Kind: "http", State: "connected", LastSeenAt: "2026-07-17T12:00:00Z", ActiveOperations: 1}},
			Runtimes:      []RuntimeData{{RuntimeID: "mr_0123456789abcdef0123456789abcdef", State: "awaiting_model", Controller: "pull_rendezvous", LastActivity: "2026-07-17T12:00:00Z"}},
			Brain:         BrainData{Available: true, Ready: true, SchemaVersion: 1, NoteCount: 2, SourceBytes: 100, LinkCount: 1, BrokenLinkCount: 0, IndexedAt: "2026-07-14T20:00:00Z", Nodes: []BrainNode{{ID: "n0001", Trust: "curated", Degree: 1}}, Edges: []BrainEdge{{Source: "n0001", Target: "n0001"}}},
			Observability: ObservabilityData{Enabled: true, Failures: 0, Routes: []ObservabilityRoute{{Route: "mcp", Requests: 4, Client4XX: 1, Server5XX: 0, P95MS: 12}}},
			Security:      SecurityData{OAuthEnabled: true, BearerRecovery: true, QueryAuth: "rejected", FreeShell: "absent", Cookie: "Secure; HttpOnly; SameSite=Strict", ConsoleAuthority: "presentation-only"},
			Edge:          EdgeData{State: "not_paired"},
		}, nil
	}
	handler, err := New(Config{Authorize: func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer test" }, DataProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, dataPath, nil)
	request.Header.Set("Authorization", "Bearer test")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &root); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, root, "brain", "controllers", "durable_activity", "edge", "observability", "payload", "runtimes", "schema_version", "security", "system")
	assertRawArrayObjectKeys(t, root["controllers"], "active_operations", "active_runtimes", "kind", "last_seen_at", "state")
	assertRawArrayObjectKeys(t, root["runtimes"], "controller", "last_activity", "runtime_id", "state")
	assertObjectKeys(t, root["system"], "available", "cpu_count", "disk_available_bytes", "disk_total_bytes", "load_1", "load_15", "load_5", "memory_available_bytes", "memory_total_bytes")
	assertObjectKeys(t, root["payload"], "estimated_payload_tokens", "formula", "input_bytes", "input_tokens_estimate", "output_bytes", "output_tokens_estimate", "process_started_at", "request_count", "tool_call_count", "warning")
	assertObjectKeys(t, root["durable_activity"], "last_24_hours", "last_30_days", "last_7_days", "last_90_days", "lifetime")
	assertNestedObjectKeys(t, root["durable_activity"], "last_24_hours", "client_errors", "estimated_payload_tokens", "external_wait_ms", "input_bytes", "output_bytes", "requests", "server_errors", "tool_calls", "updated_at")
	assertObjectKeys(t, root["brain"], "available", "broken_link_count", "edges", "graph_truncated", "indexed_at", "link_count", "nodes", "note_count", "ready", "schema_version", "source_bytes")
	assertObjectKeys(t, root["observability"], "enabled", "failures", "routes")
	assertObjectKeys(t, root["security"], "bearer_recovery", "console_authority", "cookie", "free_shell", "oauth_enabled", "query_auth")
	assertObjectKeys(t, root["edge"], "state")
	assertArrayObjectKeys(t, root["brain"], "nodes", "degree", "id", "trust")
	assertArrayObjectKeys(t, root["brain"], "edges", "source", "target")
	assertArrayObjectKeys(t, root["observability"], "routes", "client_4xx", "p95_ms", "requests", "route", "server_5xx")

	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"repository", "repo_name", "path", "prompt", "params", "result", "access_token", "refresh_token", "bearer_token", "ip_address"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("data payload contains forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestDataEndpointRequiresAuthProviderAndGET(t *testing.T) {
	providerError := func(context.Context) (DataSnapshot, error) { return DataSnapshot{}, errors.New("private failure") }
	for _, test := range []struct {
		name   string
		cfg    Config
		method string
		auth   bool
		want   int
	}{
		{name: "method", cfg: Config{Authorize: func(*http.Request) bool { return true }, DataProvider: func(context.Context) (DataSnapshot, error) { return DataSnapshot{}, nil }}, method: http.MethodPost, want: http.StatusMethodNotAllowed},
		{name: "auth", cfg: Config{Authorize: func(*http.Request) bool { return false }, DataProvider: func(context.Context) (DataSnapshot, error) { return DataSnapshot{}, nil }}, method: http.MethodGet, want: http.StatusUnauthorized},
		{name: "missing provider", cfg: Config{Authorize: func(*http.Request) bool { return true }}, method: http.MethodGet, want: http.StatusServiceUnavailable},
		{name: "provider error", cfg: Config{Authorize: func(*http.Request) bool { return true }, DataProvider: providerError}, method: http.MethodGet, want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := New(test.cfg)
			if err != nil {
				t.Fatal(err)
			}
			response := serveConsole(t, handler, httptest.NewRequest(test.method, dataPath, nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private failure") {
				t.Fatalf("provider error leaked: %s", response.Body.String())
			}
		})
	}
}

func assertObjectKeys(t *testing.T, raw json.RawMessage, expected ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, object, expected...)
}

func assertArrayObjectKeys(t *testing.T, parentRaw json.RawMessage, field string, expected ...string) {
	t.Helper()
	var parent map[string]json.RawMessage
	if err := json.Unmarshal(parentRaw, &parent); err != nil {
		t.Fatal(err)
	}
	assertRawArrayObjectKeys(t, parent[field], expected...)
}

func assertRawArrayObjectKeys(t *testing.T, raw json.RawMessage, expected ...string) {
	t.Helper()
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objects); err != nil || len(objects) != 1 {
		t.Fatalf("objects=%v err=%v", objects, err)
	}
	assertExactKeys(t, objects[0], expected...)
}

func assertExactKeys(t *testing.T, values map[string]json.RawMessage, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(values))
	for key := range values {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("keys=%v want=%v", actual, expected)
	}
}
func assertNestedObjectKeys(t *testing.T, parentRaw json.RawMessage, field string, expected ...string) {
	t.Helper()
	var parent map[string]json.RawMessage
	if err := json.Unmarshal(parentRaw, &parent); err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(parent[field], &object); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, object, expected...)
}
