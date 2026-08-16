package frontdoor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	errSSEPayloadUnsupported = errors.New("backend SSE emitted a non-comment event")
	errSSEDownstreamClosed   = errors.New("downstream SSE connection closed")
)

func isMCPStreamRequest(r *http.Request) bool {
	return r != nil && r.Method == http.MethodGet && r.URL.Path == "/mcp" &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func (f *FrontDoor) serveMCPStream(w http.ResponseWriter, r *http.Request) {
	response, err := f.awaitMCPStream(r)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, "MCP backend is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if response.StatusCode != http.StatusOK {
		copyResponseHeaders(w.Header(), response.Header)
		w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
		_ = response.Body.Close()
		return
	}
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Del("Content-Length")
	w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
	w.WriteHeader(http.StatusOK)
	if !flushResponse(w) {
		_ = response.Body.Close()
		return
	}
	err = copyCommentSSE(w, response.Body)
	_ = response.Body.Close()
	if r.Context().Err() != nil || errors.Is(err, errSSEDownstreamClosed) {
		return
	}
	if errors.Is(err, errSSEPayloadUnsupported) {
		f.sseReconnectFails.Add(1)
		return
	}
	f.storeUnavailable(errors.New("backend SSE connection ended"))
	f.recoverAcceptedMCPStream(w, r)
}

// recoverAcceptedMCPStream preserves the already authenticated downstream stream
// while compatible backend instances are replaced. The current MCP SSE contract is
// deliberately comment-only, so replaying the original Authorization header would
// add no information and would incorrectly depend on a short-lived OAuth bearer.
// Readiness and exact catalog probes remain authoritative for every recovery. New
// connections and every POST/DELETE request still authenticate at the backend.
func (f *FrontDoor) recoverAcceptedMCPStream(w http.ResponseWriter, r *http.Request) {
	for {
		waitCtx, cancel := context.WithTimeout(r.Context(), f.admissionTimeout)
		err := f.waitForBackend(waitCtx, func() error {
			return writeSSEComment(w, "front-door waiting")
		})
		cancel()
		if r.Context().Err() != nil || errors.Is(err, errSSEDownstreamClosed) {
			return
		}
		if err != nil {
			f.sseReconnectFails.Add(1)
			return
		}
		if err := writeSSEComment(w, "front-door stream recovered"); err != nil {
			return
		}
		f.sseReconnects.Add(1)

		if err := f.keepRecoveredMCPStream(w, r); err != nil {
			if !errors.Is(err, errBackendUnavailable) && !errors.Is(err, errSSEDownstreamClosed) && r.Context().Err() == nil {
				f.sseReconnectFails.Add(1)
			}
			if errors.Is(err, errBackendUnavailable) {
				continue
			}
			return
		}
		return
	}
}

func (f *FrontDoor) keepRecoveredMCPStream(w http.ResponseWriter, r *http.Request) error {
	ticker := time.NewTicker(sseWaitKeepalive)
	defer ticker.Stop()
	observedGeneration := f.backendGeneration.Load()
	for {
		changed := f.stateChangeChannel()
		state := f.state.Load()
		if state == nil || !state.Ready {
			return errBackendUnavailable
		}
		if f.backendGeneration.Load() != observedGeneration {
			return errBackendUnavailable
		}
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		case <-changed:
		case <-ticker.C:
			if err := writeSSEComment(w, "front-door ping"); err != nil {
				return err
			}
		}
	}
}

func writeSSEComment(w http.ResponseWriter, comment string) error {
	if _, err := io.WriteString(w, ": "+comment+"\n\n"); err != nil {
		return errSSEDownstreamClosed
	}
	if !flushResponse(w) {
		return errSSEDownstreamClosed
	}
	return nil
}

func (f *FrontDoor) awaitMCPStream(r *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(r.Context(), f.admissionTimeout)
	defer cancel()
	for {
		if err := f.waitForBackend(ctx, nil); err != nil {
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
		if !f.acceptsCatalog(response.Header.Get("X-MCP-Catalog-Hash")) ||
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
				return errSSEDownstreamClosed
			}
			if !flushResponse(w) {
				return errSSEDownstreamClosed
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
	for _, connection := range header.Values("Connection") {
		for _, name := range strings.Split(connection, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				header.Del(name)
			}
		}
	}
	for _, key := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(key)
	}
}
