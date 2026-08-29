// Package publicsite serves the public Aeontra product site as an isolated process.
// It can read one administrator-owned public runtime identity endpoint. It has no MCP,
// OAuth, console, repository, credential, deployment, or Edge authority.
package publicsite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/landing"
)

const (
	runtimeRequestTimeout = 5 * time.Second
	maxRuntimeBodyBytes   = 64 << 10
	siteErrorCSP          = "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

var (
	versionPattern  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,31}$`)
	protocolPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	commitPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	catalogPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Options contains the complete public-site authority. RuntimeURL is server-owned
// configuration and must identify one public HTTPS /version endpoint.
type Options struct {
	RuntimeURL string
	Client     *http.Client
}

type handler struct {
	runtimeURL *url.URL
	client     *http.Client
	landing    *landing.Handler
}

type runtimeIdentity struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

type publicRuntimeIdentity struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

// New builds the public site handler without starting a listener.
func New(options Options) (http.Handler, error) {
	runtimeURL, err := normalizeRuntimeURL(options.RuntimeURL)
	if err != nil {
		return nil, err
	}
	landingHandler, err := landing.New()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if options.Client != nil {
		copy := *options.Client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	h := &handler{runtimeURL: runtimeURL, client: client, landing: landingHandler}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.health)
	mux.HandleFunc("/readyz", h.health)
	mux.HandleFunc("/version", h.version)
	landingHandler.Register(mux)
	return mux, nil
}

func normalizeRuntimeURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Port() != "" || parsed.User != nil || parsed.Path != "/version" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("runtime URL must be one HTTPS /version endpoint without credentials, port, query, or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	labels := strings.Split(host, ".")
	if net.ParseIP(host) != nil || len(host) > 253 || len(labels) < 2 {
		return nil, errors.New("runtime URL hostname must be a fully qualified DNS name")
	}
	for _, label := range labels {
		if !dnsLabelPattern.MatchString(label) {
			return nil, errors.New("runtime URL hostname contains an invalid DNS label")
		}
	}
	parsed.Host = host
	return parsed, nil
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	harden(w, siteErrorCSP)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "ok aeontra-site %s %s\n", buildinfo.Version, buildinfo.Commit)
}

func (h *handler) version(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	identity, err := h.readRuntimeIdentity(r.Context())
	if err != nil {
		writeUnavailable(w)
		return
	}
	harden(w, siteErrorCSP)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(publicRuntimeIdentity{
		Status:          identity.Status,
		Version:         identity.Version,
		ProtocolVersion: identity.ProtocolVersion,
		Commit:          identity.Commit,
		ToolCount:       identity.ToolCount,
		CatalogHash:     identity.CatalogHash,
	})
}

func (h *handler) readRuntimeIdentity(parent context.Context) (runtimeIdentity, error) {
	ctx, cancel := context.WithTimeout(parent, runtimeRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.runtimeURL.String(), nil)
	if err != nil {
		return runtimeIdentity{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "aeontra-site/"+buildinfo.Version)
	response, err := h.client.Do(request)
	if err != nil {
		return runtimeIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return runtimeIdentity{}, fmt.Errorf("runtime identity returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return runtimeIdentity{}, errors.New("runtime identity content type is invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRuntimeBodyBytes+1))
	if err != nil || len(body) > maxRuntimeBodyBytes {
		return runtimeIdentity{}, errors.New("runtime identity body is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var identity runtimeIdentity
	if err := decoder.Decode(&identity); err != nil {
		return runtimeIdentity{}, errors.New("runtime identity is invalid")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return runtimeIdentity{}, errors.New("runtime identity contains trailing data")
	}
	if identity.Status != "ok" || !versionPattern.MatchString(identity.Version) || !protocolPattern.MatchString(identity.ProtocolVersion) || !commitPattern.MatchString(identity.Commit) || identity.ToolCount < 1 || identity.ToolCount > 10_000 || !catalogPattern.MatchString(identity.CatalogHash) {
		return runtimeIdentity{}, errors.New("runtime identity failed validation")
	}
	return identity, nil
}

func methodNotAllowed(w http.ResponseWriter) {
	harden(w, siteErrorCSP)
	w.Header().Set("Allow", http.MethodGet)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeUnavailable(w http.ResponseWriter) {
	harden(w, siteErrorCSP)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "{\"status\":\"unavailable\"}\n")
}

func harden(w http.ResponseWriter, csp string) {
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
