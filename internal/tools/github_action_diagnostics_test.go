package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

const diagnosticsHeadSHA = "1111111111111111111111111111111111111111"

func TestSourcePullRequestJobLogReadsBoundedChunksWithoutForwardingAuthorization(t *testing.T) {
	secret := "ghp_" + strings.Repeat("A", 36)
	logBody := strings.Repeat("prefix-line\n", 100) + "token=" + secret + "\n" + strings.Repeat("tail-line\n", 200)
	var signedCalls atomic.Int32
	server := newGitHubDiagnosticsServer(t, logBody, func(r *http.Request) {
		signedCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("authorization leaked to signed log host: %q", got)
		}
	})
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))

	first, err := svc.SourcePullRequestJobLog("demo", 48, "CI", "Package", 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "offset_bytes: 0") || !strings.Contains(first, "next_offset: 1024") || !strings.Contains(first, "complete: false") {
		t.Fatalf("unexpected first chunk metadata: %s", first)
	}
	second, err := svc.SourcePullRequestJobLog("demo", 48, "CI", "Package", 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second, "offset_bytes: 1024") || !strings.Contains(second, "next_offset: 2048") {
		t.Fatalf("unexpected second chunk metadata: %s", second)
	}
	for _, output := range []string{first, second} {
		if strings.Contains(output, "configured-token") || strings.Contains(output, secret) || strings.Contains(output, "sig=temporary") {
			t.Fatalf("sensitive material escaped redaction: %s", output)
		}
	}
	if signedCalls.Load() != 2 {
		t.Fatalf("signed log calls=%d, want 2", signedCalls.Load())
	}
}

func TestSourcePullRequestFailureDiagnosticsLocatesFailedStepAnnotationAndLogLine(t *testing.T) {
	logBody := "setup complete\ncompile package\nERROR post-installation script failed with exit status 1\ncleanup\n"
	server := newGitHubDiagnosticsServer(t, logBody, nil)
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))
	output, err := svc.SourcePullRequestFailureDiagnostics("demo", 48, "CI", "Package", 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"head_sha: " + diagnosticsHeadSHA,
		"workflow: CI",
		"job: Package",
		"failed_step: 2 | Exercise package | conclusion=failure",
		"annotation: packaging/debian/postinst:42",
		"postinst failed",
		"log_line 3: ERROR post-installation script failed with exit status 1",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("diagnostic missing %q: %s", required, output)
		}
	}
}

func TestSourcePullRequestJobLogRejectsUnsafeRedirect(t *testing.T) {
	var signedCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo/pulls/48":
			writeDiagnosticsPull(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs":
			writeDiagnosticsRuns(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs/10/jobs":
			writeDiagnosticsJobs(w)
		case r.URL.Path == "/repos/acme/demo/actions/jobs/20/logs":
			w.Header().Set("Location", "http://127.0.0.1:9/private")
			w.WriteHeader(http.StatusFound)
		default:
			signedCalled.Store(true)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))
	_, err := svc.SourcePullRequestJobLog("demo", 48, "CI", "Package", 0, 1024)
	if err == nil || !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "HTTPS") && !strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe redirect error=%v", err)
	}
	if signedCalled.Load() {
		t.Fatal("unsafe signed URL was requested")
	}
}

func TestSourcePullRequestJobLogRequiresWorkflowWhenJobNameIsAmbiguous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo/pulls/48":
			writeDiagnosticsPull(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs":
			_ = json.NewEncoder(w).Encode(githubActionsRunsResponse{TotalCount: 2, WorkflowRuns: []githubActionsRun{
				{ID: 10, WorkflowID: 100, RunAttempt: 1, Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: diagnosticsHeadSHA},
				{ID: 11, WorkflowID: 101, RunAttempt: 1, Name: "Security", Status: "completed", Conclusion: "failure", HeadSHA: diagnosticsHeadSHA},
			}})
		case r.URL.Path == "/repos/acme/demo/actions/runs/10/jobs" || r.URL.Path == "/repos/acme/demo/actions/runs/11/jobs":
			writeDiagnosticsJobs(w)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))
	_, err := svc.SourcePullRequestJobLog("demo", 48, "", "Package", 0, 1024)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "CI") || !strings.Contains(err.Error(), "Security") {
		t.Fatalf("ambiguous job error=%v", err)
	}
}

func TestGitHubJobLogReportsMissingActionsReadPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo/pulls/48":
			writeDiagnosticsPull(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs":
			writeDiagnosticsRuns(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs/10/jobs":
			writeDiagnosticsJobs(w)
		case r.URL.Path == "/repos/acme/demo/actions/jobs/20/logs":
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))
	_, err := svc.SourcePullRequestJobLog("demo", 48, "CI", "Package", 0, 1024)
	if err == nil || !strings.Contains(err.Error(), "Actions: Read") {
		t.Fatalf("permission error=%v", err)
	}
}

func newGitHubDiagnosticsServer(t *testing.T, logBody string, signedHook func(*http.Request)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo/pulls/48":
			writeDiagnosticsPull(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs":
			writeDiagnosticsRuns(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs/10/jobs":
			writeDiagnosticsJobs(w)
		case r.URL.Path == "/repos/acme/demo/check-runs/20/annotations":
			_ = json.NewEncoder(w).Encode([]githubCheckAnnotation{{Path: "packaging/debian/postinst", StartLine: 42, EndLine: 42, AnnotationLevel: "failure", Title: "Package fixture", Message: "postinst failed"}})
		case r.URL.Path == "/repos/acme/demo/actions/jobs/20/logs":
			if got := r.Header.Get("Authorization"); got != "Bearer configured-token" {
				t.Fatalf("API authorization=%q", got)
			}
			w.Header().Set("Location", server.URL+"/signed/job.log?sig=temporary")
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/signed/job.log":
			if signedHook != nil {
				signedHook(r)
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(w, logBody)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func writeDiagnosticsPull(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(githubPullResponse{Number: 48, Head: struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}{Ref: "feature", SHA: diagnosticsHeadSHA}})
}

func writeDiagnosticsRuns(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(githubActionsRunsResponse{TotalCount: 1, WorkflowRuns: []githubActionsRun{{
		ID: 10, WorkflowID: 100, RunAttempt: 1, Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: diagnosticsHeadSHA,
	}}})
}

func writeDiagnosticsJobs(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(githubActionsJobsResponse{TotalCount: 1, Jobs: []githubActionsJob{{
		ID: 20, Name: "Package", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.example/job/20",
		Steps: []githubActionsStep{{Number: 1, Name: "Checkout", Status: "completed", Conclusion: "success"}, {Number: 2, Name: "Exercise package", Status: "completed", Conclusion: "failure"}},
	}}})
}
