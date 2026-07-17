// Package authfirmware provides one fixed, dependency-free stylesheet shared by
// the console login and OAuth authorization surfaces.
package authfirmware

import (
	_ "embed"
	"fmt"
	"net/http"
)

const (
	Path = "/auth/assets/firmware.css"
	CSP  = "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
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

func Harden(w http.ResponseWriter, csp string) {
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "accelerometer=(), autoplay=(), camera=(), display-capture=(), encrypted-media=(), fullscreen=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), picture-in-picture=(), publickey-credentials-get=(), screen-wake-lock=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}
