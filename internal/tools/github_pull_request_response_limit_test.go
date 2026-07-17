package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func largePullPayload(t *testing.T, number int, headSHA, baseSHA string, paddingBytes int) []byte {
	t.Helper()
	payload := map[string]any{
		"number":    number,
		"state":     "open",
		"merged":    false,
		"mergeable": true,
		"html_url":  "https://github.com/acme/demo/pull/7",
		"body":      strings.Repeat("x", paddingBytes),
		"head":      map[string]string{"ref": "mvp", "sha": headSHA},
		"base":      map[string]string{"ref": "main", "sha": baseSHA},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= int(githubDefaultResponseLimit) {
		t.Fatalf("fixture size=%d must exceed default limit", len(encoded))
	}
	return encoded
}

func TestGitHubPullRequestEndpointsAcceptValidResponsesLargerThanDefaultLimit(t *testing.T) {
	headSHA := strings.Repeat("a", 40)
	baseSHA := strings.Repeat("b", 40)
	pullBody := largePullPayload(t, 7, headSHA, baseSHA, int(githubDefaultResponseLimit)+4096)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/demo/pulls":
			_, _ = w.Write(pullBody)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls":
			_, _ = w.Write(append(append([]byte{'['}, pullBody...), ']'))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls/7":
			_, _ = w.Write(pullBody)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	created, err := client.createPullRequest(context.Background(), "demo", "mvp", "main", "MVP", "body")
	if err != nil || created.Number != 7 || created.Head.SHA != headSHA {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	found, err := client.findPullRequest(context.Background(), "demo", "mvp", "main")
	if err != nil || found == nil || found.Number != 7 || found.Base.SHA != baseSHA {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	read, err := client.pullRequest(context.Background(), "demo", 7)
	if err != nil || read.Number != 7 || read.Head.SHA != headSHA {
		t.Fatalf("read=%+v err=%v", read, err)
	}
}

func TestGitHubPullRequestPreviewRecoversExistingAfterCreateResponseIsLost(t *testing.T) {
	headSHA := strings.Repeat("a", 40)
	baseSHA := strings.Repeat("b", 40)
	created := false
	postCount := 0
	pullBody := largePullPayload(t, 1, headSHA, baseSHA, int(githubDefaultResponseLimit)+4096)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/mvp":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls":
			if created {
				_, _ = w.Write(append(append([]byte{'['}, pullBody...), ']'))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/demo/pulls":
			postCount++
			created = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourcePullRequestCreatePreview("demo", "mvp", "main", "MVP", "safe body")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SourcePullRequestCreate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "decoding created") {
		t.Fatalf("expected lost create response error, got %v", err)
	}
	recovered, err := svc.SourcePullRequestCreatePreview("demo", "mvp", "main", "MVP", "safe body")
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(recovered, "pull_request: 1") || !strings.Contains(recovered, "existing: true") {
		t.Fatalf("recovered=%q err=%v", recovered, err)
	}
	if postCount != 1 {
		t.Fatalf("POST count=%d want=1", postCount)
	}
}

func TestGitHubPullRequestLimitRejectsOversizedBodyExplicitly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(githubPullResponseLimit)+1)))
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	if _, err := client.pullRequest(context.Background(), "demo", 7); err == nil || !strings.Contains(err.Error(), "exceeded 524288-byte limit") {
		t.Fatalf("expected explicit pull response limit error, got %v", err)
	}
}
