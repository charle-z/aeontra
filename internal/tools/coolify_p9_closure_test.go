package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestCoolifySetEnvCreatesMissingAndPatchesExistingWithoutReturningValues(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	const existingValue = "existing-value-must-not-leak"
	const createdValue = "created-value-must-not-leak"
	var methods []string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/app1/envs":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"uuid":"env-existing","key":"EXISTING","value":"old"}]`))}, nil
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/app1/envs":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["key"] != "EXISTING" || body["value"] != existingValue {
				t.Fatalf("bad patch payload: %#v", body)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"key":"EXISTING","value":"existing-value-must-not-leak"}`))}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/app1/envs":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["key"] != "CREATED" || body["value"] != createdValue {
				t.Fatalf("bad create payload: %#v", body)
			}
			return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"key":"CREATED","value":"created-value-must-not-leak"}`))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	}))

	out, err := svc.CoolifySetEnv("app1", map[string]string{
		"EXISTING": existingValue,
		"CREATED":  createdValue,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "GET,POST,PATCH" {
		t.Fatalf("methods=%q, want GET,POST,PATCH", got)
	}
	if strings.Contains(out, existingValue) || strings.Contains(out, createdValue) {
		t.Fatalf("environment value leaked in output: %q", out)
	}
	if !strings.Contains(out, "CREATED -> created") || !strings.Contains(out, "EXISTING -> updated") {
		t.Fatalf("unexpected safe summary: %q", out)
	}
}

func TestCoolifySetEnvRejectsDuplicateExistingKeyBeforeWriting(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	var writes int
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			writes++
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
			{"uuid":"env-1","key":"DUPLICATE","value":"one"},
			{"uuid":"env-2","key":"DUPLICATE","value":"two"}
		]`))}, nil
	}))

	if _, err := svc.CoolifySetEnv("app1", map[string]string{"DUPLICATE": "new"}, false); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("duplicate existing key must be rejected as conflict: %v", err)
	}
	if writes != 0 {
		t.Fatalf("conflict must be detected before writes, got %d writes", writes)
	}
}

func TestEnsureP9BrainStorageExistingExactStorageIsIdempotent(t *testing.T) {
	var calls int
	c := fakeCoolify(t, "https://coolify.example.com", "tok", []string{"jqf7qz5ensoqtvl1tb197gcv"}, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/jqf7qz5ensoqtvl1tb197gcv/storages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
			{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
		]`))}, nil
	})

	out, err := c.ensureP9BrainStorage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(out, "verified") {
		t.Fatalf("exact storage should be idempotent: calls=%d out=%q", calls, out)
	}
}

func TestEnsureP9BrainStorageCreatesManagedVolumeAndReverifies(t *testing.T) {
	var calls int
	c := fakeCoolify(t, "https://coolify.example.com", "tok", []string{"jqf7qz5ensoqtvl1tb197gcv"}, func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			if r.Method != http.MethodGet {
				t.Fatalf("first request must list storages, got %s", r.Method)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/applications/jqf7qz5ensoqtvl1tb197gcv/storages" {
				t.Fatalf("unexpected create request: %s %s", r.Method, r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 3 || body["type"] != "persistent" || body["name"] != "mcp-devbox-brain" || body["mount_path"] != "/brain" {
				t.Fatalf("storage payload must be exact and omit host_path: %#v", body)
			}
			if _, exists := body["host_path"]; exists {
				t.Fatalf("host_path must be omitted: %#v", body)
			}
			return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"uuid":"storage-1"}`))}, nil
		case 3:
			if r.Method != http.MethodGet {
				t.Fatalf("third request must re-list storages, got %s", r.Method)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
				{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
			]`))}, nil
		default:
			t.Fatalf("unexpected extra request %d", calls)
			return nil, nil
		}
	})

	out, err := c.ensureP9BrainStorage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !strings.Contains(out, "created and verified") {
		t.Fatalf("storage creation not verified: calls=%d out=%q", calls, out)
	}
}

func TestEnsureP9BrainStorageRejectsConflictsWithoutWriting(t *testing.T) {
	bodies := []string{
		`[{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/wrong"}]`,
		`[{"uuid":"storage-1","type":"persistent","name":"other","mount_path":"/brain"}]`,
		`[{"uuid":"storage-1","type":"bind","name":"mcp-devbox-brain","mount_path":"/brain"}]`,
		`[
			{"uuid":"storage-1","type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"},
			{"uuid":"storage-2","type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
		]`,
	}
	for i, body := range bodies {
		body := body
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			var writes int
			c := fakeCoolify(t, "https://coolify.example.com", "tok", []string{"jqf7qz5ensoqtvl1tb197gcv"}, func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					writes++
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			if _, err := c.ensureP9BrainStorage(context.Background()); err == nil || !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("storage conflict must be rejected: %v", err)
			}
			if writes != 0 {
				t.Fatalf("conflict must never trigger a write, got %d", writes)
			}
		})
	}
}
