package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateListenAddressRejectsPublicOrUnspecifiedBinds(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8770", "10.0.0.2:8770", "[fd00::1]:8770"} {
		if err := validateListenAddress(address); err != nil {
			t.Errorf("private address %q was rejected: %v", address, err)
		}
	}
	for _, address := range []string{
		":8770",
		"0.0.0.0:8770",
		"[::]:8770",
		"8.8.8.8:8770",
		"localhost:8770",
		"mcp-sandbox-runner:8770",
		"public.example.invalid:8770",
	} {
		if err := validateListenAddress(address); err == nil {
			t.Errorf("unsafe address %q was accepted", address)
		}
	}
}

func TestRunnerHealthcheckUsesConfiguredPrivateAddress(t *testing.T) {
	baseURL, err := runnerHealthcheckURL("10.0.1.2:8770")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "http://10.0.1.2:8770" {
		t.Fatalf("healthcheck base URL=%q", baseURL)
	}
}

func TestCheckRunnerHealthRequiresAuthenticatedAvailableRootlessStatus(t *testing.T) {
	const token = "01234567890123456789012345678901"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"available":true,"rootless":true}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := checkRunnerHealth(ctx, server.URL, token, server.Client()); err != nil {
		t.Fatal(err)
	}
	if err := checkRunnerHealth(ctx, server.URL, "wrong", server.Client()); err == nil {
		t.Fatal("healthcheck accepted an unauthorized response")
	}
}

func TestCheckRunnerHealthRejectsUnavailableOrUnknownResponses(t *testing.T) {
	for _, body := range []string{
		`{"available":false,"rootless":true}`,
		`{"available":true,"rootless":false}`,
		`{"available":true,"rootless":true,"unknown":true}`,
		`{"available":true,"rootless":true}{}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := checkRunnerHealth(ctx, server.URL, "token", server.Client())
		cancel()
		server.Close()
		if err == nil {
			t.Fatalf("healthcheck accepted %s", body)
		}
	}
}
