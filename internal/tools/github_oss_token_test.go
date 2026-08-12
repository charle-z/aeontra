package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClientUsesOSSTokenOnlyForExternalRepoRoutes(t *testing.T) {
	expected := map[string]string{
		"/repos/charle-z/demo":                         "Bearer primary-fixture",
		"/repos/CHARLE-Z/demo":                         "Bearer primary-fixture",
		"/repos/openai/tunnel-client":                  "Bearer oss-fixture",
		"/repos/openai/tunnel-client/pulls?state=open": "Bearer oss-fixture",
		"/user/repos":                                  "Bearer primary-fixture",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want, ok := expected[r.URL.RequestURI()]
		if !ok {
			t.Errorf("unexpected request %s", r.URL.RequestURI())
		} else if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization for %s = %q, want %q", r.URL.RequestURI(), got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "primary-fixture", "charle-z", "user", "private").WithOSSToken("oss-fixture")
	for path := range expected {
		status, _, err := client.doJSON(context.Background(), http.MethodGet, path, nil)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if status != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, status, http.StatusOK)
		}
	}
}

func TestGitHubClientExternalRepoRouteFallsBackWithoutOSSToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer primary-fixture" {
			t.Errorf("Authorization = %q, want primary token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "primary-fixture", "charle-z", "user", "private")
	status, _, err := client.doJSON(context.Background(), http.MethodGet, "/repos/openai/tunnel-client/pulls", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
}
