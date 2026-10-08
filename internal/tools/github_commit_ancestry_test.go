package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommitIsAncestorAcceptsCompareResponseLargerThanRefLimit(t *testing.T) {
	ancestor := strings.Repeat("a", 40)
	descendant := strings.Repeat("b", 40)
	largePatch := strings.Repeat("x", int(githubRefAndMergeResponseLimit)+4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/repos/acme/mcp-devbox/compare/" + ancestor + "..." + descendant
		if r.Method != http.MethodGet || r.URL.Path != wantPath || r.URL.Query().Get("per_page") != "1" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = fmt.Fprintf(w, `{"status":"ahead","files":[{"patch":%q}]}`, largePatch)
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "fixture-token", "acme", "org", "private")
	ancestorOK, err := client.commitIsAncestor(context.Background(), "mcp-devbox", ancestor, descendant)
	if err != nil {
		t.Fatal(err)
	}
	if !ancestorOK {
		t.Fatal("valid ancestor response was rejected")
	}
}

func TestCommitIsAncestorRejectsResponseBeyondCompareLimit(t *testing.T) {
	ancestor := strings.Repeat("a", 40)
	descendant := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(githubCompareResponseLimit)+1)))
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "fixture-token", "acme", "org", "private")
	_, err := client.commitIsAncestor(context.Background(), "mcp-devbox", ancestor, descendant)
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error=%v", err)
	}
}
