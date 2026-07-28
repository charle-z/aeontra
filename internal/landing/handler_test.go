package landing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicLandingRoutesAndSecurityHeaders(t *testing.T) {
	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)

	response := serveLanding(mux, http.MethodGet, "/")
	if response.Code != http.StatusOK {
		t.Fatalf("GET / status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("GET / content-type=%q", contentType)
	}
	for _, header := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
		"Origin-Agent-Cluster",
	} {
		if response.Header().Get(header) == "" {
			t.Errorf("GET / missing %s", header)
		}
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, required := range []string{
		"default-src 'none'",
		"style-src 'self'",
		"script-src 'self'",
		"connect-src 'self'",
		"img-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	} {
		if !strings.Contains(csp, required) {
			t.Errorf("landing CSP missing %q: %s", required, csp)
		}
	}
	for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "data:", "https:"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("landing CSP contains forbidden source %q: %s", forbidden, csp)
		}
	}

	method := serveLanding(mux, http.MethodPost, "/")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST / status=%d allow=%q", method.Code, method.Header().Get("Allow"))
	}

	missing := serveLanding(mux, http.MethodGet, "/private/control")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", missing.Code)
	}
	if !strings.Contains(missing.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("unknown route is not hardened")
	}
}

func TestPublicLandingCSPAllowsSameOriginRuntimeIdentity(t *testing.T) {
	const want = "default-src 'none'; connect-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	if landingPageCSP != want {
		t.Fatalf("landing page CSP=%q want %q", landingPageCSP, want)
	}

	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)

	response := serveLanding(mux, http.MethodGet, "/")
	if response.Code != http.StatusOK {
		t.Fatalf("GET / status=%d body=%s", response.Code, response.Body.String())
	}
	csp := response.Header().Get("Content-Security-Policy")
	if csp != want {
		t.Fatalf("GET / CSP=%q want %q", csp, want)
	}
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("GET / CSP does not permit the same-origin /version request: %s", csp)
	}
}

func TestPublicLandingAssetsAreEmbeddedAndHardened(t *testing.T) {
	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)

	for path, wantType := range map[string]string{
		"/landing/assets/app.css":            "text/css",
		"/landing/assets/app.js":             "text/javascript",
		"/landing/assets/request-path.svg":   "image/svg+xml",
		"/landing/assets/social-card.svg":    "image/svg+xml",
		"/showcase/pixelgrama-evidence.json": "application/json",
	} {
		response := serveLanding(mux, http.MethodGet, path)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status=%d", path, response.Code)
			continue
		}
		if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, wantType) {
			t.Errorf("GET %s content-type=%q want %q", path, contentType, wantType)
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s missing nosniff", path)
		}
	}
}

func TestPublicLandingFailsClosedWhenEvidenceIsUnavailable(t *testing.T) {
	loaders := []func() ([]byte, error){
		nil,
		func() ([]byte, error) { return nil, http.ErrAbortHandler },
		func() ([]byte, error) { return nil, nil },
	}
	for index, loader := range loaders {
		if _, err := newHandler(loader); err == nil || err.Error() != "Pixelgrama evidence is unavailable" {
			t.Fatalf("loader %d error=%v", index, err)
		}
	}
}

func serveLanding(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
