package authfirmware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirmwareStylesheetIsFixedLocalAndPixelSafe(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, Path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("content-type=%q", recorder.Header().Get("Content-Type"))
	}
	body := strings.ToLower(recorder.Body.String())
	for _, required := range []string{"#0000a8", "border-radius: 0", "focus-visible", "prefers-reduced-motion", "font-family"} {
		if !strings.Contains(body, required) {
			t.Fatalf("stylesheet missing %q", required)
		}
	}
	for _, forbidden := range []string{"url(", "@import", "gradient(", "border-radius: 1", "animation-name"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stylesheet contains forbidden marker %q", forbidden)
		}
	}
}

func TestFirmwareStylesheetRejectsWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, Path, nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestHardenKeepsOrdinaryPagesOpenerIsolated(t *testing.T) {
	recorder := httptest.NewRecorder()
	Harden(recorder, CSP)
	if got := recorder.Header().Get("Cross-Origin-Opener-Policy"); got != DefaultCOOP {
		t.Fatalf("COOP=%q", got)
	}
}

func TestHardenOAuthPreservesPopupOpener(t *testing.T) {
	recorder := httptest.NewRecorder()
	HardenOAuth(recorder, CSP)
	if got := recorder.Header().Get("Cross-Origin-Opener-Policy"); got != OAuthCOOP {
		t.Fatalf("COOP=%q", got)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != CSP {
		t.Fatalf("CSP=%q", got)
	}
	if got := recorder.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options=%q", got)
	}
}
