package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func TestSetEnvironmentVariablesUsesLiteralRuntimeContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		existing   string
		wantMethod string
	}{
		{name: "create", existing: `[]`, wantMethod: http.MethodPost},
		{name: "update", existing: `[{"uuid":"env1","key":"MCP_FRONT_DOOR_BACKEND_URL","value":"old","is_preview":false}]`, wantMethod: http.MethodPatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewCoolifyClient("https://coolify.example", "token", nil)
			requests := 0
			client.do = func(request *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					if request.Method != http.MethodGet || request.URL.Path != "/api/v1/applications/app1/envs" {
						t.Fatalf("unexpected list request: %s %s", request.Method, request.URL.Path)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.existing)), Header: make(http.Header)}, nil
				}
				if request.Method != test.wantMethod || request.URL.Path != "/api/v1/applications/app1/envs" {
					t.Fatalf("unexpected write request: %s %s", request.Method, request.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatalf("decode payload: %v", err)
				}
				for key, want := range map[string]any{
					"key": "MCP_FRONT_DOOR_BACKEND_URL", "value": "https://backend.example",
					"comment":    frontdoorcoordinator.ManagedEnvironmentComment("token", "MCP_FRONT_DOOR_BACKEND_URL", "https://backend.example"),
					"is_preview": false, "is_literal": true, "is_runtime": true, "is_buildtime": false,
				} {
					if got := payload[key]; got != want {
						t.Fatalf("payload %s = %#v, want %#v", key, got, want)
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			}

			if _, err := client.setEnvironmentVariables(context.Background(), "app1", map[string]string{
				"MCP_FRONT_DOOR_BACKEND_URL": "https://backend.example",
			}, []string{"MCP_FRONT_DOOR_BACKEND_URL"}); err != nil {
				t.Fatalf("set environment variables: %v", err)
			}
			if requests != 2 {
				t.Fatalf("requests = %d, want 2", requests)
			}
		})
	}
}
