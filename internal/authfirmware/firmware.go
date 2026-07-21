// Package authfirmware provides one fixed, dependency-free stylesheet shared by
// the console login and OAuth authorization surfaces.
package authfirmware

import (
	_ "embed"
	"fmt"
	"net/http"
)

const (
	Path        = "/auth/assets/firmware.css"
	CSP         = "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	DefaultCOOP = "same-origin"
	OAuthCOOP   = "unsafe-none"
)

//go:embed assets/firmware.css
var stylesheet []byte

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		Harden(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	Harden(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(stylesheet)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stylesheet)
}

// Harden applies the isolated browser boundary used by ordinary same-origin
// authentication and console responses.
func Harden(w http.ResponseWriter, csp string) {
	harden(w, csp, DefaultCOOP)
}

// HardenOAuth preserves the opener relationship required by popup-based OAuth
// clients while retaining the remaining firmware security headers. CSP and
// X-Frame-Options still prevent framing, and the page contains no script.
func HardenOAuth(w http.ResponseWriter, csp string) {
	harden(w, csp, OAuthCOOP)
}

func harden(w http.ResponseWriter, csp, coop string) {
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "accelerometer=(), autoplay=(), camera=(), display-capture=(), encrypted-media=(), fullscreen=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), picture-in-picture=(), publickey-credentials-get=(), screen-wake-lock=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", coop)
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}
