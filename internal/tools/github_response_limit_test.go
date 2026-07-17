package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubCheckSummaryAcceptsBoundedResponsesLargerThanDefaultLimit(t *testing.T) {
	headSHA := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			checks := make([]map[string]string, 100)
			for index := range checks {
				checks[index] = map[string]string{
					"name":       "check-" + strings.Repeat("n", 80),
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/acme/demo/actions/" + strings.Repeat("1", 120),
				}
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"total_count": len(checks), "check_runs": checks}); err != nil {
				t.Fatal(err)
			}
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"state":"success","statuses":[]}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	summary, err := client.checkSummary(context.Background(), "demo", headSHA)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 100 || summary.Passed != 100 || summary.Pending != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestGitHubDefaultResponseLimitRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(githubDefaultResponseLimit)+1)))
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	if _, _, err := client.doJSON(context.Background(), http.MethodGet, "/oversized", nil); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected bounded response error, got %v", err)
	}
}
