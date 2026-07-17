package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
)

func TestConsoleLoginUsesSharedFirmwareWithoutInlineCode(t *testing.T) {
	handler := newTestHandler(t)
	response := serveConsole(t, handler, httptest.NewRequest(http.MethodGet, consolePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	body := strings.ToLower(response.Body.String())
	if !strings.Contains(body, `href="`+strings.ToLower(authfirmware.Path)+`"`) {
		t.Fatalf("shared stylesheet missing: %s", body)
	}
	for _, forbidden := range []string{"<style", "style=", "<script", "onclick=", "onload=", "unsafe-inline", "border-radius", "gradient("} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("login contains forbidden marker %q", forbidden)
		}
	}
	for _, required := range []string{"role=\"alert\"", "for=\"token\"", "autocomplete=\"current-password\"", "[ recovery ]", "[ offline ]"} {
		if !strings.Contains(body, required) && required != "role=\"alert\"" {
			t.Fatalf("login missing %q", required)
		}
	}
	if got := response.Header().Get("Content-Security-Policy"); got != authfirmware.CSP {
		t.Fatalf("CSP=%q", got)
	}
}

func TestConsoleFirmwareAssetIsAvailableBeforeAuthentication(t *testing.T) {
	handler := newTestHandler(t)
	response := serveConsole(t, handler, httptest.NewRequest(http.MethodGet, authfirmware.Path, nil))
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "http://") || strings.Contains(strings.ToLower(response.Body.String()), "https://") {
		t.Fatal("firmware asset references an external origin")
	}
}

func TestConsoleLoginFailureIsAccessibleFirmwareAlert(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader("token=wrong"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := serveConsole(t, handler, request)
	body := strings.ToLower(response.Body.String())
	if response.Code != http.StatusUnauthorized || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "[ denied ]") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
}
