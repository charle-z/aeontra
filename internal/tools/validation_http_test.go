package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidationHTTPRunnerSendsOnlyRepositoryIDAndClosedProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/run" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
			t.Error("missing validation runner authorization")
		}
		var payload map[string]string
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(payload) != 2 || payload["repo_id"] != "repo-one" || payload["profile"] != "pnpm-validate" {
			t.Errorf("unexpected validation request shape: %#v", payload)
		}
		for _, forbidden := range []string{"repo", "path", "host_path", "container_path", "mount"} {
			if _, exists := payload[forbidden]; exists {
				t.Errorf("validation request exposed forbidden field %q", forbidden)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ValidationResult{ExitCode: 0, Output: "ok"})
	}))
	defer server.Close()

	runner := NewValidationRunner(server.URL, "01234567890123456789012345678901")
	result, err := runner.Run(context.Background(), "repo-one", "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Output != "ok" {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}
