// Package landing serves the unauthenticated, presentation-only MCP Devbox showcase.
// It owns only static embedded assets. It cannot execute tools, inspect repositories,
// approve plans, read audit data, or access console sessions and credentials.
package landing

import (
	"embed"
	"errors"
	"fmt"
	"net/http"

	showcase "github.com/charle-z/mcp-devbox/docs/showcase"
)

const (
	rootPath       = "/"
	cssPath        = "/landing/assets/app.css"
	jsPath         = "/landing/assets/app.js"
	requestPathSVG = "/landing/assets/request-path.svg"
	socialCardSVG  = "/landing/assets/social-card.svg"
	evidencePath   = "/showcase/pixelgrama-evidence.json"
)

const landingPageCSP = "default-src 'none'; connect-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

//go:embed assets/index.html assets/app.css assets/app.js assets/request-path.svg assets/social-card.svg
var embeddedAssets embed.FS

// Handler owns the immutable public landing assets.
type Handler struct {
	indexHTML      []byte
	css            []byte
	js             []byte
	requestPathSVG []byte
	socialCardSVG  []byte
	evidence       []byte
}

// New loads the embedded assets once so missing build inputs fail at startup.
func New() (*Handler, error) {
	return newHandler(showcase.PixelgramaEvidence)
}

func newHandler(loadEvidence func() ([]byte, error)) (*Handler, error) {
	indexHTML, err := readAsset("assets/index.html", "landing index")
	if err != nil {
		return nil, err
	}
	css, err := readAsset("assets/app.css", "landing stylesheet")
	if err != nil {
		return nil, err
	}
	js, err := readAsset("assets/app.js", "landing script")
	if err != nil {
		return nil, err
	}
	requestPath, err := readAsset("assets/request-path.svg", "landing request-path graphic")
	if err != nil {
		return nil, err
	}
	socialCard, err := readAsset("assets/social-card.svg", "landing social card")
	if err != nil {
		return nil, err
	}
	if loadEvidence == nil {
		return nil, errors.New("Pixelgrama evidence is unavailable")
	}
	evidence, err := loadEvidence()
	if err != nil || len(evidence) == 0 {
		return nil, errors.New("Pixelgrama evidence is unavailable")
	}
	return &Handler{
		indexHTML:      indexHTML,
		css:            css,
		js:             js,
		requestPathSVG: requestPath,
		socialCardSVG:  socialCard,
		evidence:       evidence,
	}, nil
}

func readAsset(path, label string) ([]byte, error) {
	content, err := embeddedAssets.ReadFile(path)
	if err != nil {
		return nil, errors.New(label + " is unavailable")
	}
	return content, nil
}

// Register mounts the exact public assets and a hardened catch-all rooted at /.
// More specific MCP, console, OAuth, health, and version routes keep precedence in
// net/http.ServeMux and remain owned by their existing handlers.
func (h *Handler) Register(mux *http.ServeMux) {
	if h == nil || mux == nil {
		return
	}
	mux.HandleFunc(cssPath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "text/css; charset=utf-8", h.css)
	})
	mux.HandleFunc(jsPath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "text/javascript; charset=utf-8", h.js)
	})
	mux.HandleFunc(requestPathSVG, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "image/svg+xml; charset=utf-8", h.requestPathSVG)
	})
	mux.HandleFunc(socialCardSVG, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "image/svg+xml; charset=utf-8", h.socialCardSVG)
	})
	mux.HandleFunc(evidencePath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "application/json; charset=utf-8", h.evidence)
	})
	mux.HandleFunc(rootPath, h.handleRoot)
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != rootPath {
		hardenLandingResponse(w, errorCSP())
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		landingMethodNotAllowed(w, http.MethodGet)
		return
	}
	hardenLandingResponse(w, landingPageCSP)
	writeLandingContent(w, "text/html; charset=utf-8", h.indexHTML)
}

func (h *Handler) handleAsset(w http.ResponseWriter, r *http.Request, contentType string, content []byte) {
	if r.Method != http.MethodGet {
		landingMethodNotAllowed(w, http.MethodGet)
		return
	}
	hardenLandingResponse(w, errorCSP())
	writeLandingContent(w, contentType, content)
}

func writeLandingContent(w http.ResponseWriter, contentType string, content []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func hardenLandingResponse(w http.ResponseWriter, csp string) {
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

func errorCSP() string {
	return "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}

func landingMethodNotAllowed(w http.ResponseWriter, allowed string) {
	hardenLandingResponse(w, errorCSP())
	w.Header().Set("Allow", allowed)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
