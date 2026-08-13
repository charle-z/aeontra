package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClientRetriesExternalAuthFailureWithPrimaryToken(t *testing.T) {
	for _, firstStatus := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(firstStatus), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/repos/openai/tunnel-client/issues/33/comments" {
					t.Fatalf("path = %q", r.URL.Path)
				}
				switch calls {
				case 1:
					if got := r.Header.Get("Authorization"); got != "Bearer oss-fixture" {
						t.Fatalf("first Authorization = %q, want OSS token", got)
					}
					w.WriteHeader(firstStatus)
				case 2:
					if got := r.Header.Get("Authorization"); got != "Bearer primary-fixture" {
						t.Fatalf("fallback Authorization = %q, want primary token", got)
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Fatalf("unexpected request %d", calls)
				}
			}))
			defer server.Close()

			client := NewGitHubClient(server.URL, "primary-fixture", "charle-z", "user", "private").WithOSSToken("oss-fixture")
			status, _, err := client.doJSON(context.Background(), http.MethodPost, "/repos/openai/tunnel-client/issues/33/comments", []byte(`{"body":"hello"}`))
			if err != nil {
				t.Fatal(err)
			}
			if status != http.StatusCreated || calls != 2 {
				t.Fatalf("status = %d, calls = %d; want 201, 2", status, calls)
			}
		})
	}
}
