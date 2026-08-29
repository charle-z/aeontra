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
		"style-src-attr 'none'",
		"script-src 'self'",
		"script-src-attr 'none'",
		"connect-src 'self'",
		"img-src 'self'",
		"object-src 'none'",
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

func TestPublicLandingCSPAllowsOnlySameOriginPublicReads(t *testing.T) {
	const want = "default-src 'none'; connect-src 'self'; script-src 'self'; script-src-attr 'none'; style-src 'self'; style-src-attr 'none'; img-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
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
		t.Fatalf("GET / CSP does not permit the same-origin public reads: %s", csp)
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
		"/landing/assets/app.css":         "text/css",
		"/landing/assets/app.js":          "text/javascript",
		"/landing/assets/social-card.svg": "image/svg+xml",
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

func TestPublicLandingDoesNotServeHistoricalShowcaseAssets(t *testing.T) {
	handler, err := New()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)

	for _, path := range []string{
		"/landing/assets/request-path.svg",
		"/showcase/pixelgrama-evidence.json",
	} {
		response := serveLanding(mux, http.MethodGet, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status=%d want 404", path, response.Code)
		}
	}
}

func serveLanding(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
