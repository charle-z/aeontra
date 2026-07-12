package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyRetireEndpointIsDisabledUnlessExplicitlyEnabled(t *testing.T) {
	cfg := config{token: "01234567890123456789012345678901"}
	req := httptest.NewRequest(http.MethodPost, legacyRetirePath, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	res := httptest.NewRecorder()
	newRunnerMux(cfg, false).ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("disabled endpoint returned %d, want 404", res.Code)
	}
}

func TestLegacyRetireEndpointRunsOnlyFixedAction(t *testing.T) {
	called := false
	cfg := config{
		token: "01234567890123456789012345678901",
		retireLegacy: func(context.Context) error {
			called = true
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, legacyRetirePath, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	res := httptest.NewRecorder()
	newRunnerMux(cfg, true).ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("enabled endpoint returned %d: %s", res.Code, res.Body.String())
	}
	if !called {
		t.Fatal("fixed retirement action was not called")
	}
	if !strings.Contains(res.Body.String(), `"stopped":true`) {
		t.Fatalf("unexpected response: %s", res.Body.String())
	}
}

func TestLegacyRetireEndpointRequiresPostAndBearer(t *testing.T) {
	cfg := config{
		token:        "01234567890123456789012345678901",
		retireLegacy: func(context.Context) error { return nil },
	}
	for _, tc := range []struct {
		method string
		token  string
	}{
		{method: http.MethodGet, token: cfg.token},
		{method: http.MethodPost, token: "wrong"},
	} {
		req := httptest.NewRequest(tc.method, legacyRetirePath, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		res := httptest.NewRecorder()
		newRunnerMux(cfg, true).ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s with token %q returned %d, want 401", tc.method, tc.token, res.Code)
		}
	}
}
