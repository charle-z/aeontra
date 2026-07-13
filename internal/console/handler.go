// Package console provides an authenticated, presentation-only web surface for the
// safe public runtime identity. It has no dependency on MCP tools, repositories,
// audit, observability history, source hosting, or deployment integrations.
package console

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	consolePath  = "/console"
	loginPath    = "/console/login"
	logoutPath   = "/console/logout"
	statusPath   = "/console/status"
	cssPath      = "/console/assets/app.css"
	jsPath       = "/console/assets/app.js"
	cookieName   = "mcpdevbox_console"
	maxLoginBody = 4 << 10
)

//go:embed assets/index.html assets/app.css assets/app.js
var embeddedAssets embed.FS

// Status is the complete allowlisted console status schema.
type Status struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
	Authenticated   bool   `json:"authenticated"`
	Surface         string `json:"surface"`
}

// Config contains only the existing static token, safe runtime identity, direct
// authorization callback, cookie posture, and bounded session configuration.
type Config struct {
	StaticToken   string
	Runtime       Status
	Authorize     func(*http.Request) bool
	SecureCookies bool
	Session       SessionConfig
}

// Handler owns only presentation assets and an in-memory digest-only session store.
type Handler struct {
	staticToken   string
	runtime       Status
	authorize     func(*http.Request) bool
	secureCookies bool
	sessions      *SessionStore
	indexHTML     []byte
	css           []byte
	js            []byte
}

func New(cfg Config) (*Handler, error) {
	sessions, err := NewSessionStore(cfg.Session)
	if err != nil {
		return nil, err
	}
	indexHTML, err := embeddedAssets.ReadFile("assets/index.html")
	if err != nil {
		return nil, errors.New("console index asset is unavailable")
	}
	css, err := embeddedAssets.ReadFile("assets/app.css")
	if err != nil {
		return nil, errors.New("console stylesheet asset is unavailable")
	}
	js, err := embeddedAssets.ReadFile("assets/app.js")
	if err != nil {
		return nil, errors.New("console script asset is unavailable")
	}
	cfg.Runtime.Authenticated = true
	cfg.Runtime.Surface = "presentation-only"
	return &Handler{
		staticToken:   cfg.StaticToken,
		runtime:       cfg.Runtime,
		authorize:     cfg.Authorize,
		secureCookies: cfg.SecureCookies,
		sessions:      sessions,
		indexHTML:     indexHTML,
		css:           css,
		js:            js,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	if h == nil || mux == nil {
		return
	}
	mux.HandleFunc(consolePath, h.handleConsole)
	mux.HandleFunc(loginPath, h.handleLogin)
	mux.HandleFunc(logoutPath, h.handleLogout)
	mux.HandleFunc(statusPath, h.handleStatus)
	mux.HandleFunc(cssPath, h.handleCSS)
	mux.HandleFunc(jsPath, h.handleJS)
	mux.HandleFunc(consolePath+"/", h.handleConsoleSubpath)
}

func (h *Handler) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.sessionAuthorized(r) {
		h.writeConsolePage(w)
		return
	}
	if h.directAuthorized(r) {
		if err := h.bootstrapSession(w, r); err != nil {
			writeGenericError(w, http.StatusServiceUnavailable)
			return
		}
		hardenRedirect(w)
		http.Redirect(w, r, consolePath, http.StatusSeeOther)
		return
	}
	h.writeLoginPage(w, false)
}

func (h *Handler) handleConsoleSubpath(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == consolePath+"/" && r.Method == http.MethodGet {
		hardenRedirect(w)
		http.Redirect(w, r, consolePath, http.StatusPermanentRedirect)
		return
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	http.NotFound(w, r)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		hardenResponse(w, errorCSP())
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLoginBody))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			hardenResponse(w, errorCSP())
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		writeGenericError(w, http.StatusBadRequest)
		return
	}
	values, err := url.ParseQuery(string(body))
	provided := values["token"]
	if err != nil || len(values) != 1 || len(provided) != 1 || !h.validStaticToken(provided[0]) {
		h.writeLoginPage(w, true)
		return
	}
	if err := h.bootstrapSession(w, r); err != nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	hardenRedirect(w)
	http.Redirect(w, r, consolePath, http.StatusSeeOther)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if cookie, err := r.Cookie(cookieName); err == nil {
		h.sessions.Revoke(cookie.Value)
	}
	h.clearCookie(w, r)
	hardenRedirect(w)
	http.Redirect(w, r, consolePath, http.StatusSeeOther)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(h.runtime)
}

func (h *Handler) handleCSS(w http.ResponseWriter, r *http.Request) {
	h.handleAsset(w, r, "text/css; charset=utf-8", h.css)
}

func (h *Handler) handleJS(w http.ResponseWriter, r *http.Request) {
	h.handleAsset(w, r, "text/javascript; charset=utf-8", h.js)
}

func (h *Handler) handleAsset(w http.ResponseWriter, r *http.Request, contentType string, content []byte) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) writeConsolePage(w http.ResponseWriter) {
	hardenResponse(w, strings.Join([]string{
		"default-src 'none'",
		"style-src 'self'",
		"script-src 'self'",
		"connect-src 'self'",
		"img-src 'self' data:",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(h.indexHTML)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.indexHTML)
}

func (h *Handler) writeLoginPage(w http.ResponseWriter, failed bool) {
	nonce, err := randomHex(16)
	if err != nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	hardenResponse(w, loginCSP(nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := http.StatusOK
	message := ""
	if failed {
		status = http.StatusUnauthorized
		message = `<p class="error" role="alert">Authentication failed.</p>`
	}
	loginControl := `<p class="note">OAuth or bearer authentication is required. Open the console through an authenticated client.</p>`
	if h.staticToken != "" {
		loginControl = `<form method="post" action="/console/login" autocomplete="off"><label for="token">Access token</label><input id="token" name="token" type="password" required maxlength="4096" autocomplete="current-password"><button type="submit">Sign in</button></form><p class="note">The token is submitted in the HTTPS request body and is never stored in the browser session cookie.</p>`
	}
	page := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<title>MCP Devbox Console — Sign in</title>
<style nonce="` + nonce + `">
:root{color-scheme:dark;font-family:Inter,ui-sans-serif,system-ui,sans-serif;background:#07090d;color:#edf4ff}
*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;padding:1rem;background:radial-gradient(circle at top,#142134 0,#07090d 45%)}
main{width:min(28rem,100%);padding:2rem;border:1px solid #263044;border-radius:1rem;background:#0d1118;box-shadow:0 24px 70px #0008}
mark{display:grid;width:3rem;height:3rem;place-items:center;border:1px solid #73f4c366;border-radius:.85rem;color:#73f4c3;background:#73f4c311;font-weight:800}
h1{margin:1.25rem 0 .5rem;font-size:2rem;letter-spacing:-.04em}p{color:#9ba9bd}label{display:block;margin:1.5rem 0 .45rem;font-weight:700}
input{width:100%;padding:.8rem;border:1px solid #35415a;border-radius:.7rem;background:#07090d;color:#edf4ff;font:inherit}input:focus{outline:3px solid #8ea8ff;outline-offset:2px}
button{width:100%;margin-top:1rem;padding:.8rem;border:0;border-radius:.7rem;background:#73f4c3;color:#07100d;font:inherit;font-weight:800;cursor:pointer}
.error{padding:.75rem;border:1px solid #ff8e9e66;border-radius:.65rem;color:#ffb6c0;background:#ff8e9e10}.note{font-size:.82rem}
</style>
</head>
<body><main><mark aria-hidden="true">M</mark><h1>MCP Devbox Console</h1><p>Sign in to the private presentation surface.</p>` + message + loginControl + `</main></body></html>`
	w.WriteHeader(status)
	_, _ = io.WriteString(w, page)
}

func (h *Handler) authorized(r *http.Request) bool {
	return h.sessionAuthorized(r) || h.directAuthorized(r)
}

func (h *Handler) sessionAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	return err == nil && h.sessions.Valid(cookie.Value)
}

func (h *Handler) directAuthorized(r *http.Request) bool {
	return h.authorize != nil && h.authorize(r)
}

func (h *Handler) validStaticToken(provided string) bool {
	if h.staticToken == "" || provided == "" {
		return false
	}
	expectedDigest := sha256.Sum256([]byte(h.staticToken))
	providedDigest := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedDigest[:], providedDigest[:]) == 1
}

func (h *Handler) bootstrapSession(w http.ResponseWriter, r *http.Request) error {
	raw, err := h.sessions.Create()
	if err != nil {
		return err
	}
	expires, ok := h.sessions.Expiry(raw)
	if !ok {
		return errors.New("console session expiry is unavailable")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    raw,
		Path:     consolePath,
		Expires:  expires,
		MaxAge:   int(h.sessions.ttl.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (h *Handler) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     consolePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) cookieSecure(r *http.Request) bool {
	if h.secureCookies || r.TLS != nil {
		return true
	}
	host := r.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

func hardenResponse(w http.ResponseWriter, csp string) {
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

func loginCSP(nonce string) string {
	return "default-src 'none'; style-src 'nonce-" + nonce + "'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

func errorCSP() string {
	return "default-src 'none'; base-uri 'none'; frame-ancestors 'none'"
}

func hardenRedirect(w http.ResponseWriter) {
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Allow", allowed)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeUnauthorized(w http.ResponseWriter) {
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func writeGenericError(w http.ResponseWriter, status int) {
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	http.Error(w, "request failed", status)
}

func randomHex(size int) (string, error) {
	if size <= 0 || size > 64 {
		return "", errors.New("invalid random size")
	}
	buffer := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", errors.New("secure random generation failed")
	}
	return hex.EncodeToString(buffer), nil
}
