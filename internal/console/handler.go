// Package console provides an authenticated, presentation-only web surface for the
// safe public runtime identity. It has no dependency on MCP tools, repositories,
// audit, observability history, source hosting, or deployment integrations.
package console

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

const (
	consolePath       = "/console"
	loginPath         = "/console/login"
	logoutPath        = "/console/logout"
	statusPath        = "/console/status"
	oauthStartPath    = "/console/auth/start"
	oauthCallbackPath = "/console/auth/callback"
	cssPath           = "/console/assets/app.css"
	jsPath            = "/console/assets/app.js"
	cookieName        = "mcpdevbox_console"
	maxLoginBody      = 4 << 10
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
	StaticToken     string
	Runtime         Status
	Authorize       func(*http.Request) bool
	OAuthProvider   *oauth.Provider
	TaskJournal     *taskjournal.Journal
	DataProvider    DataProvider
	Session         SessionConfig
	DefaultTimezone string
}

// Handler owns presentation assets and the configured digest-only session store.
type Handler struct {
	staticToken     string
	runtime         Status
	authorize       func(*http.Request) bool
	sessions        *SessionStore
	oauthClient     *oauth.ConsoleClient
	oauthFlows      *oauthFlowStore
	taskJournal     *taskjournal.Journal
	dataProvider    DataProvider
	defaultTimezone string
	indexHTML       []byte
	css             []byte
	js              []byte
}

func New(cfg Config) (*Handler, error) {
	defaultTimezone, err := ValidateTimezone(cfg.DefaultTimezone)
	if err != nil {
		return nil, err
	}
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
	var oauthClient *oauth.ConsoleClient
	if cfg.OAuthProvider != nil {
		oauthClient, err = cfg.OAuthProvider.NewConsoleClient(oauthCallbackPath)
		if err != nil {
			return nil, err
		}
	}
	cfg.Runtime.Authenticated = true
	cfg.Runtime.Surface = "presentation-only"
	return &Handler{
		staticToken:     cfg.StaticToken,
		runtime:         cfg.Runtime,
		authorize:       cfg.Authorize,
		sessions:        sessions,
		oauthClient:     oauthClient,
		oauthFlows:      newOAuthFlowStore(),
		taskJournal:     cfg.TaskJournal,
		dataProvider:    cfg.DataProvider,
		defaultTimezone: defaultTimezone,
		indexHTML:       indexHTML,
		css:             css,
		js:              js,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	if h == nil || mux == nil {
		return
	}
	mux.HandleFunc(authfirmware.Path, authfirmware.ServeHTTP)
	mux.HandleFunc(consolePath, h.handleConsole)
	mux.HandleFunc(loginPath, h.handleLogin)
	mux.HandleFunc(logoutPath, h.handleLogout)
	mux.HandleFunc(statusPath, h.handleStatus)
	mux.HandleFunc(tasksPath, h.handleTasks)
	mux.HandleFunc(eventLogPath, h.handleEventLog)
	mux.HandleFunc(taskEventsPath, h.handleTaskEvents)
	mux.HandleFunc(dataPath, h.handleData)
	mux.HandleFunc(preferencesPath, h.handlePreferences)
	if h.oauthClient != nil {
		mux.HandleFunc(oauthStartPath, h.handleOAuthStart)
		mux.HandleFunc(oauthCallbackPath, h.handleOAuthCallback)
	}
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

func (h *Handler) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.oauthClient == nil || h.oauthFlows == nil {
		writeUnauthorized(w)
		return
	}
	state, _, challenge, err := h.oauthFlows.create()
	if err != nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	authorizeURL, err := h.oauthClient.AuthorizationURL(state, challenge)
	if err != nil {
		_, _ = h.oauthFlows.consume(state)
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	hardenRedirect(w)
	http.Redirect(w, r, authorizeURL, http.StatusSeeOther)
}

func (h *Handler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.oauthClient == nil || h.oauthFlows == nil {
		writeUnauthorized(w)
		return
	}
	query := r.URL.Query()
	codes, states := query["code"], query["state"]
	if len(query) != 2 || len(codes) != 1 || len(states) != 1 || codes[0] == "" || states[0] == "" {
		writeUnauthorized(w)
		return
	}
	verifier, ok := h.oauthFlows.consume(states[0])
	if !ok || !h.oauthClient.Complete(codes[0], verifier) {
		writeUnauthorized(w)
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

func (h *Handler) bootstrapSession(w http.ResponseWriter, _ *http.Request) error {
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
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(h.sessions.ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (h *Handler) clearCookie(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
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
