package publicsite

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewRejectsUnsafeRuntimeURLs(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://runtime.example.test/version",
		"https://user:pass@runtime.example.test/version",
		"https://runtime.example.test:8443/version",
		"https://runtime.example.test/",
		"https://runtime.example.test/version?full=true",
		"https://runtime.example.test/version#identity",
		"https://runtime..example.test/version",
		"https://runtime_example.test/version",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := New(Options{RuntimeURL: raw}); err == nil {
				t.Fatalf("New(%q) succeeded, want fail-closed rejection", raw)
			}
		})
	}
}

func TestHandlerServesSiteHealthAndSanitizedRuntimeIdentity(t *testing.T) {
	const runtimeBody = `{"status":"ok","version":"0.2.0","protocol_version":"2024-11-05","commit":"537ae17d31eeb1bf3260e053bab2314400a99efc","built_at":"2026-08-29T04:00:00Z","tool_count":176,"catalog_hash":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.URL.String(); got != "https://runtime.example.test/version" {
			t.Fatalf("runtime request URL = %q", got)
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Fatal("runtime probe forwarded credentials")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(runtimeBody)),
			Request:    request,
		}, nil
	})}
	handler, err := New(Options{RuntimeURL: "https://runtime.example.test/version", Client: client})
	if err != nil {
		t.Fatal(err)
	}

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), "Connect your MCP client to the repositories and tools you choose") {
		t.Fatalf("landing response = %d %q", root.Code, root.Body.String())
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), "ok aeontra-site") {
		t.Fatalf("health response = %d %q", health.Code, health.Body.String())
	}

	version := httptest.NewRecorder()
	handler.ServeHTTP(version, httptest.NewRequest(http.MethodGet, "/version", nil))
	if version.Code != http.StatusOK {
		t.Fatalf("version response = %d %q", version.Code, version.Body.String())
	}
	for _, value := range []string{"\"status\":\"ok\"", "\"tool_count\":176", "537ae17d31eeb1bf3260e053bab2314400a99efc"} {
		if !strings.Contains(version.Body.String(), value) {
			t.Fatalf("version response lacks %q: %s", value, version.Body.String())
		}
	}
	if strings.Contains(version.Body.String(), "built_at") {
		t.Fatalf("public version response exposes unused upstream build metadata: %s", version.Body.String())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control", "Strict-Transport-Security"} {
		if version.Header().Get(header) == "" {
			t.Fatalf("version response lacks %s", header)
		}
	}
}

func TestHandlerDoesNotExposeControlPlaneRoutes(t *testing.T) {
	handler := newTestHandler(t, validRuntimeResponse())
	for _, path := range []string{"/mcp", "/console", "/oauth/authorize", "/.well-known/oauth-authorization-server"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, response.Code)
		}
	}
}

func TestRuntimeProxyFailsClosed(t *testing.T) {
	tests := map[string]roundTripFunc{
		"transport error": func(*http.Request) (*http.Response, error) {
			return nil, errors.New("private upstream detail")
		},
		"non-200": func(request *http.Request) (*http.Response, error) {
			return runtimeResponse(request, http.StatusBadGateway, `upstream secret`), nil
		},
		"unknown field": func(request *http.Request) (*http.Response, error) {
			return runtimeResponse(request, http.StatusOK, strings.TrimSuffix(validRuntimeResponse(), "}")+`,"private":"value"}`), nil
		},
		"concatenated JSON": func(request *http.Request) (*http.Response, error) {
			return runtimeResponse(request, http.StatusOK, validRuntimeResponse()+` {}`), nil
		},
		"invalid commit": func(request *http.Request) (*http.Response, error) {
			return runtimeResponse(request, http.StatusOK, strings.Replace(validRuntimeResponse(), strings.Repeat("a", 40), "unknown", 1)), nil
		},
	}
	for name, transport := range tests {
		t.Run(name, func(t *testing.T) {
			handler, err := New(Options{RuntimeURL: "https://runtime.example.test/version", Client: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil))
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestRuntimeProxyRejectsNonGETMethods(t *testing.T) {
	handler := newTestHandler(t, validRuntimeResponse())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/version", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST /version = %d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func newTestHandler(t *testing.T, body string) http.Handler {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return runtimeResponse(request, http.StatusOK, body), nil
	})}
	handler, err := New(Options{RuntimeURL: "https://runtime.example.test/version", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func runtimeResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func validRuntimeResponse() string {
	return `{"status":"ok","version":"0.2.0","protocol_version":"2024-11-05","commit":"` + strings.Repeat("a", 40) + `","built_at":"unknown","tool_count":176,"catalog_hash":"sha256:` + strings.Repeat("b", 64) + `"}`
}

func TestTransportCancellationDoesNotLeakContext(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	handler, err := New(Options{RuntimeURL: "https://runtime.example.test/version", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil).WithContext(ctx))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancelled request = %d", response.Code)
	}
}
