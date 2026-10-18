package frontdoorcoordinator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureRoundTripper func(*http.Request) (*http.Response, error)

func (fn captureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestPrivateGatewayPreservesOriginalControlHost(t *testing.T) {
	t.Parallel()
	client, err := NewClient(validClientConfig("http+host-gateway://coolify.example:8000"))
	if err != nil {
		t.Fatal(err)
	}
	if client.config.CoolifyURL != "http://coolify.example:8000" {
		t.Fatalf("normalized control origin=%q", client.config.CoolifyURL)
	}
	client.http = &http.Client{Transport: captureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "http" || request.URL.Host != "coolify.example:8000" {
			t.Fatalf("request origin=%s://%s", request.URL.Scheme, request.URL.Host)
		}
		if request.URL.Path != "/api/v1/applications/front1" {
			t.Fatalf("request path=%q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"uuid":"front1"}`)),
			Request:    request,
		}, nil
	})}
	var result application
	if err := client.requestJSON(context.Background(), http.MethodGet, "/api/v1/applications/front1", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.UUID != "front1" {
		t.Fatalf("result=%+v", result)
	}
}
