package frontdoor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var errSSEPayloadUnsupported = errors.New("backend SSE emitted a non-comment event")

func isMCPStreamRequest(r *http.Request) bool {
	return r != nil && r.Method == http.MethodGet && r.URL.Path == "/mcp" &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func (f *FrontDoor) serveMCPStream(w http.ResponseWriter, r *http.Request) {
	started := false
	for {
		response, err := f.awaitMCPStream(w, r, started)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			if started {
				f.sseReconnectFails.Add(1)
				return
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "MCP backend is temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if response.StatusCode != http.StatusOK {
			if started {
				_ = response.Body.Close()
				f.sseReconnectFails.Add(1)
				return
			}
			copyResponseHeaders(w.Header(), response.Header)
			w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, response.Body)
			_ = response.Body.Close()
			return
		}
		if !started {
			copyResponseHeaders(w.Header(), response.Header)
			w.Header().Del("Content-Length")
			w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
			w.WriteHeader(http.StatusOK)
			if !flushResponse(w) {
				_ = response.Body.Close()
				return
			}
			started = true
		} else {
			f.sseReconnects.Add(1)
		}
		err = copyCommentSSE(w, response.Body)
		_ = response.Body.Close()
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, errSSEPayloadUnsupported) {
			f.sseReconnectFails.Add(1)
			return
		}
		f.storeUnavailable(errors.New("backend SSE connection ended"))
	}
}

func (f *FrontDoor) awaitMCPStream(w http.ResponseWriter, r *http.Request, started bool) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(r.Context(), f.admissionTimeout)
	defer cancel()
	var heartbeat func() error
	if started {
		heartbeat = func() error {
			if _, err := io.WriteString(w, ": front-door waiting\n\n"); err != nil {
				return err
			}
			if !flushResponse(w) {
				return context.Canceled
			}
			return nil
		}
	}
	for {
		if err := f.waitForBackend(ctx, heartbeat); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, transient, err := f.openMCPStream(r.Context(), r)
		if err == nil {
			return response, nil
		}
		if !transient {
			return nil, err
		}
	}
}

func (f *FrontDoor) openMCPStream(ctx context.Context, incoming *http.Request) (*http.Response, bool, error) {
	target := f.backend.ResolveReference(&url.URL{Path: incoming.URL.Path, RawPath: incoming.URL.RawPath, RawQuery: incoming.URL.RawQuery})
	request := incoming.Clone(ctx)
	request.URL = target
	request.RequestURI = ""
	request.Host = f.backend.Host
	request.Body = nil
	request.GetBody = nil
	request.Header = incoming.Header.Clone()
	removeHopHeaders(request.Header)
	response, err := f.streamClient.Do(request)
	if err != nil {
		f.storeUnavailable(errors.New("backend SSE transport failed"))
		return nil, true, err
	}
	if response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		f.storeUnavailable(errors.New("backend SSE upstream unavailable"))
		return nil, true, errBackendUnavailable
	}
	if response.StatusCode == http.StatusOK {
		if response.Header.Get("X-MCP-Catalog-Hash") != f.expectedCatalogHash ||
			!strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
			_ = response.Body.Close()
			f.storeUnavailable(errors.New("backend compatibility response mismatch"))
			return nil, false, errors.New("backend SSE contract is incompatible")
		}
	}
	return response, false, nil
}

func copyCommentSSE(w http.ResponseWriter, body io.Reader) error {
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed != "" && !strings.HasPrefix(trimmed, ":") {
				return errSSEPayloadUnsupported
			}
			if _, writeErr := io.WriteString(w, line); writeErr != nil {
				return writeErr
			}
			if !flushResponse(w) {
				return context.Canceled
			}
		}
		if err != nil {
			return err
		}
	}
}

func flushResponse(w http.ResponseWriter) bool {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return false
	}
	flusher.Flush()
	return true
}

func copyResponseHeaders(destination, source http.Header) {
	for key := range destination {
		destination.Del(key)
	}
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
	removeHopHeaders(destination)
}

func removeHopHeaders(header http.Header) {
	for _, key := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(key)
	}
}
