package autopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type LocalHTTPModel struct {
	Endpoint string
	Client   *http.Client
}

func (m LocalHTTPModel) NextAction(ctx context.Context, request LocalAgentRequest) (LocalAgentResponse, error) {
	endpoint, err := url.Parse(m.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.Path != "/v1/next-action" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return LocalAgentResponse{}, errors.New("local model endpoint is invalid")
	}
	host, _, err := net.SplitHostPort(endpoint.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return LocalAgentResponse{}, errors.New("local model endpoint must be loopback")
	}
	body, err := json.Marshal(request)
	if err != nil || len(body) > 2<<20 {
		return LocalAgentResponse{}, errors.New("local model request is invalid")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.Endpoint, bytes.NewReader(body))
	if err != nil {
		return LocalAgentResponse{}, errors.New("local model request failed")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return LocalAgentResponse{}, errors.New("local model unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnavailableForLegalReasons {
		return LocalAgentResponse{}, ErrProviderBlocked
	}
	if response.StatusCode != http.StatusOK {
		return LocalAgentResponse{}, errors.New("local model failed")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(content) > 256<<10 {
		return LocalAgentResponse{}, errors.New("local model response is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var action LocalAgentResponse
	if decoder.Decode(&action) != nil || !validAction(action) {
		return LocalAgentResponse{}, errors.New("local model response is invalid")
	}
	return action, nil
}

type DeterministicModel struct {
	Responses []LocalAgentResponse
	Index     int
}

func (m *DeterministicModel) NextAction(context.Context, LocalAgentRequest) (LocalAgentResponse, error) {
	if m == nil || m.Index >= len(m.Responses) {
		return LocalAgentResponse{}, ErrProviderBlocked
	}
	response := m.Responses[m.Index]
	m.Index++
	return response, nil
}
