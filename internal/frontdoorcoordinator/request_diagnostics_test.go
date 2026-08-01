package frontdoorcoordinator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type diagnosticRoundTripper func(*http.Request) (*http.Response, error)

func (fn diagnosticRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type diagnosticReadCloser struct{}

func (diagnosticReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("raw response read secret")
}

func (diagnosticReadCloser) Close() error { return nil }

func newDiagnosticClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(validClientConfig("https://coolify.example"))
	if err != nil {
		t.Fatal(err)
	}
	client.http = &http.Client{Transport: transport}
	return client
}

func requireClosedDiagnostic(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || err.Error() != want.Error() {
		t.Fatalf("error=%v want=%v", err, want)
	}
	for _, forbidden := range []string{"raw", "secret", "coolify.example"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("diagnostic leaked %q: %q", forbidden, err.Error())
		}
	}
}

func TestRequestJSONReturnsOnlyClosedFailureClasses(t *testing.T) {
	t.Parallel()

	client, err := NewClient(validClientConfig("https://coolify.example"))
	if err != nil {
		t.Fatal(err)
	}
	requireClosedDiagnostic(t,
		client.requestJSON(context.Background(), http.MethodPost, "/api/v1/test", func() {}, nil),
		ErrCoolifyRequestBuild,
	)

	client = newDiagnosticClient(t, diagnosticRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("raw transport secret")
	}))
	requireClosedDiagnostic(t,
		client.requestJSON(context.Background(), http.MethodGet, "/api/v1/test", nil, nil),
		ErrCoolifyRequestTransport,
	)

	client = newDiagnosticClient(t, diagnosticRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       diagnosticReadCloser{},
			Request:    request,
		}, nil
	}))
	requireClosedDiagnostic(t,
		client.requestJSON(context.Background(), http.MethodGet, "/api/v1/test", nil, nil),
		ErrCoolifyResponseRead,
	)

	client = newDiagnosticClient(t, diagnosticRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("raw HTTP secret")),
			Request:    request,
		}, nil
	}))
	requireClosedDiagnostic(t,
		client.requestJSON(context.Background(), http.MethodGet, "/api/v1/test", nil, nil),
		ErrCoolifyResponseHTTP,
	)

	client = newDiagnosticClient(t, diagnosticRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("raw invalid JSON secret")),
			Request:    request,
		}, nil
	}))
	var decoded application
	requireClosedDiagnostic(t,
		client.requestJSON(context.Background(), http.MethodGet, "/api/v1/test", nil, &decoded),
		ErrCoolifyResponseDecode,
	)
}

func TestRequestJSONPreservesOnlyClosedPrivateTransportDetail(t *testing.T) {
	t.Parallel()
	client := newDiagnosticClient(t, diagnosticRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.Join(ErrCoolifyPrivateRefused, errors.New("raw transport secret"))
	}))

	err := client.requestJSON(context.Background(), http.MethodGet, "/api/v1/test", nil, nil)
	if !errors.Is(err, ErrCoolifyRequestTransport) {
		t.Fatalf("transport class missing: %v", err)
	}
	if !errors.Is(err, ErrCoolifyPrivateRefused) {
		t.Fatalf("private transport detail missing: %v", err)
	}
	for _, forbidden := range []string{"raw", "secret", "coolify.example"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("transport diagnostic leaked %q: %q", forbidden, err.Error())
		}
	}
}

func TestApplicationIdentityFailureIsClosed(t *testing.T) {
	t.Parallel()
	client := newDiagnosticClient(t, diagnosticRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"uuid":"wrong-secret-id"}`)),
			Request:    request,
		}, nil
	}))
	_, err := client.application(context.Background(), "front1")
	requireClosedDiagnostic(t, err, ErrCoolifyIdentity)
}
