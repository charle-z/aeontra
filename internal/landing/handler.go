// Package landing serves the unauthenticated Aeontra public product site.
// It owns only static embedded assets. It cannot execute tools, inspect repositories,
// approve plans, read audit data, or access console sessions and credentials.
package landing

import (
	"embed"
	"errors"
	"fmt"
	"net/http"
)

const (
	rootPath      = "/"
	cssPath       = "/landing/assets/app.css"
	jsPath        = "/landing/assets/app.js"
	socialCardSVG = "/landing/assets/social-card.svg"
	socialCardPNG = "/landing/assets/social-card.png"
	faviconPath   = "/favicon.svg"
	robotsPath    = "/robots.txt"
	sitemapPath   = "/sitemap.xml"
)

const landingPageCSP = "default-src 'none'; connect-src 'self'; script-src 'self'; script-src-attr 'none'; style-src 'self'; style-src-attr 'none'; img-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

//go:embed assets/index.html assets/app.css assets/app.js assets/social-card.svg assets/social-card.png assets/favicon.svg assets/robots.txt assets/sitemap.xml
var embeddedAssets embed.FS

// Handler owns the immutable public landing assets.
type Handler struct {
	indexHTML     []byte
	css           []byte
	js            []byte
	socialCardSVG []byte
	socialCardPNG []byte
	favicon       []byte
	robots        []byte
	sitemap       []byte
}

// New loads the embedded assets once so missing build inputs fail at startup.
func New() (*Handler, error) {
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
	socialCard, err := readAsset("assets/social-card.svg", "landing social card")
	if err != nil {
		return nil, err
	}
	socialCardPNGBytes, err := readAsset("assets/social-card.png", "landing social card PNG")
	if err != nil {
		return nil, err
	}
	favicon, err := readAsset("assets/favicon.svg", "landing favicon")
	if err != nil {
		return nil, err
	}
	robots, err := readAsset("assets/robots.txt", "landing robots policy")
	if err != nil {
		return nil, err
	}
	sitemap, err := readAsset("assets/sitemap.xml", "landing sitemap")
	if err != nil {
		return nil, err
	}
	return &Handler{
		indexHTML:     indexHTML,
		css:           css,
		js:            js,
		socialCardSVG: socialCard,
		socialCardPNG: socialCardPNGBytes,
		favicon:       favicon,
		robots:        robots,
		sitemap:       sitemap,
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
	mux.HandleFunc(socialCardSVG, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "image/svg+xml; charset=utf-8", h.socialCardSVG)
	})
	mux.HandleFunc(socialCardPNG, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "image/png", h.socialCardPNG)
	})
	mux.HandleFunc(faviconPath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "image/svg+xml; charset=utf-8", h.favicon)
	})
	mux.HandleFunc(robotsPath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "text/plain; charset=utf-8", h.robots)
	})
	mux.HandleFunc(sitemapPath, func(w http.ResponseWriter, r *http.Request) {
		h.handleAsset(w, r, "application/xml; charset=utf-8", h.sitemap)
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
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
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
