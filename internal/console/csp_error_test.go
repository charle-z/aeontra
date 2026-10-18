package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginProtocolErrorsUseCSPWithoutStyleNonce(t *testing.T) {
	handler := newTestHandler(t)
	cases := []struct {
		name    string
		request *http.Request
		want    int
	}{
		{
			name:    "unsupported content type",
			request: httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader("token=x")),
			want:    http.StatusUnsupportedMediaType,
		},
		{
			name: "oversized body",
			request: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(strings.Repeat("x", maxLoginBody+1)))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return r
			}(),
			want: http.StatusRequestEntityTooLarge,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := serveConsole(t, handler, tc.request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d", response.Code, tc.want)
			}
			csp := response.Header().Get("Content-Security-Policy")
			if strings.Contains(csp, "style-src") || strings.Contains(csp, "nonce-") {
				t.Fatalf("error response CSP contains a style nonce: %s", csp)
			}
		})
	}
}
