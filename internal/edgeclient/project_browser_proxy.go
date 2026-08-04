package edgeclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type projectBrowserProxy struct {
	listener             net.Listener
	server               *http.Server
	scope, initialOrigin string
	resolver             *net.Resolver
	closeOnce            sync.Once
}

func startProjectBrowserProxy(scope, initialOrigin string) (*projectBrowserProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("project browser proxy unavailable")
	}
	proxy := &projectBrowserProxy{listener: listener, scope: scope, initialOrigin: initialOrigin, resolver: net.DefaultResolver}
	proxy.server = &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() { _ = proxy.server.Serve(listener) }()
	return proxy, nil
}

func (p *projectBrowserProxy) URL() string { return "http://" + p.listener.Addr().String() }
func (p *projectBrowserProxy) Close() error {
	var err error
	p.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = p.server.Shutdown(ctx)
		_ = p.listener.Close()
	})
	return err
}

func (p *projectBrowserProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r)
		return
	}
	if r.URL == nil || !r.URL.IsAbs() {
		http.Error(w, "browser proxy request invalid", http.StatusBadRequest)
		return
	}
	address, serverName, err := p.resolveDestination(r.Context(), r.URL)
	if err != nil {
		http.Error(w, "browser destination blocked", http.StatusForbidden)
		return
	}
	request := r.Clone(r.Context())
	request.RequestURI = ""
	request.Header = request.Header.Clone()
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
	}, TLSClientConfig: &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}}
	response, err := transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "browser destination unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 64<<20))
}

func (p *projectBrowserProxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	raw := p.connectURL(r.Host)
	parsed, err := url.Parse(raw)
	if err != nil {
		http.Error(w, "browser CONNECT invalid", http.StatusBadRequest)
		return
	}
	address, _, err := p.resolveDestination(r.Context(), parsed)
	if err != nil {
		http.Error(w, "browser destination blocked", http.StatusForbidden)
		return
	}
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(r.Context(), "tcp", address)
	if err != nil {
		http.Error(w, "browser destination unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "browser proxy unavailable", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = buffer.Flush()
	if buffered := buffer.Reader.Buffered(); buffered > 0 {
		_, _ = io.CopyN(upstream, buffer.Reader, int64(buffered))
	}
	go func() { defer upstream.Close(); defer client.Close(); _, _ = io.Copy(upstream, client) }()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	_ = upstream.Close()
}

func (p *projectBrowserProxy) connectURL(authority string) string {
	scheme := "https"
	if p.scope == "loopback" {
		if initial, err := url.Parse(p.initialOrigin); err == nil && initial.Scheme != "" {
			scheme = initial.Scheme
		}
	}
	return scheme + "://" + authority + "/"
}

func (p *projectBrowserProxy) resolveDestination(ctx context.Context, u *url.URL) (string, string, error) {
	if err := ValidateBrowserURL(ctx, p.scope, p.initialOrigin, u.String(), nil); err != nil {
		return "", "", err
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	if p.scope == "public" && port != "80" && port != "443" {
		return "", "", errors.New("browser public port is not allowed")
	}
	var ips []net.IP
	if parsed := net.ParseIP(host); parsed != nil {
		ips = []net.IP{parsed}
	} else {
		resolved, err := p.resolver.LookupIP(ctx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return "", "", errors.New("browser DNS resolution failed")
		}
		ips = resolved
	}
	for _, ip := range ips {
		if p.scope == "public" {
			if validatePublicBrowserIP(ip) != nil {
				return "", "", fmt.Errorf("browser destination address blocked")
			}
		} else if !ip.IsLoopback() {
			return "", "", fmt.Errorf("browser destination address blocked")
		}
	}
	return net.JoinHostPort(ips[0].String(), port), host, nil
}
