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
