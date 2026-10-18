package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestCoolifySetEnvIgnoresPreviewProjectionForProductionUpdate(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	const value = "value-must-not-leak"
	var methods []string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
				{"uuid":"production","key":"EXISTING","is_preview":false},
				{"uuid":"preview","key":"EXISTING","is_preview":true}
			]`))}, nil
		case http.MethodPatch:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["key"] != "EXISTING" || body["value"] != value {
				t.Fatalf("bad patch payload: %#v", body)
			}
			return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"key":"EXISTING"}`))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	}))

	out, err := svc.CoolifySetEnv("app1", map[string]string{"EXISTING": value}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, ","); got != "GET,PATCH" {
		t.Fatalf("methods=%q want GET,PATCH", got)
	}
	if strings.Contains(out, value) || !strings.Contains(out, "updated") {
		t.Fatalf("unexpected or leaking output: %q", out)
	}
}
