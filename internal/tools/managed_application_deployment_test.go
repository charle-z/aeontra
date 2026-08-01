package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

const managedDeploymentTestTime = "2026-08-01T20:00:00Z"

func writeManagedDeploymentList(w http.ResponseWriter, appID, status, commit string) {
	writeManagedDeploymentListAt(w, appID, status, commit, managedDeploymentTestTime)
}

func writeManagedDeploymentListAt(w http.ResponseWriter, appID, status, commit, createdAt string) {
	_, _ = fmt.Fprintf(w, `[{"deployment_uuid":"dep%s","status":"%s","commit":"%s","created_at":"%s","updated_at":"%s"}]`,
		appID, status, commit, createdAt, createdAt)
}

func TestLatestManagedApplicationDeploymentGate(t *testing.T) {
	wrongCommit := strings.Repeat("b", 40)
	tests := []struct {
		name     string
		body     string
		expected string
		wantErr  string
	}{
		{
			name:     "finished",
			body:     `[{"uuid":"dep1","status":"finished","git_commit_sha":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]`,
			expected: frontDoorTestSHA,
		},
		{
			name:    "in progress",
			body:    `[{"deployment_uuid":"dep1","status":"in_progress","commit":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]`,
			wantErr: "not finished",
		},
		{
			name:    "failed",
			body:    `[{"deployment_uuid":"dep1","status":"failed","commit":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]`,
			wantErr: "not finished",
		},
		{
			name: "ambiguous latest",
			body: `[
				{"deployment_uuid":"dep1","status":"finished","commit":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"},
				{"deployment_uuid":"dep2","status":"finished","commit":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:02:00Z"}
			]`,
			wantErr: "ambiguous",
		},
		{
			name:     "wrong commit",
			body:     `[{"deployment_uuid":"dep1","status":"finished","commit":"` + wrongCommit + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]`,
			expected: frontDoorTestSHA,
			wantErr:  "approved branch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/deployments/applications/app1" || r.URL.Query().Get("skip") != "0" || r.URL.Query().Get("take") != "2" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			svc := configuredPlatformService(t, config.ModeReadOnly, server.URL)
			deployment, err := svc.latestManagedApplicationDeployment("app1")
			if err == nil {
				err = requireManagedDeployment("fixture", deployment, tc.expected)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if deployment.Commit != tc.expected || deployment.Status != "finished" {
					t.Fatalf("deployment=%+v", deployment)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error=%v want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestLatestManagedApplicationDeploymentFallsBackOnlyForEmptyPaginatedResponse(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/deployments/applications/app1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if r.URL.RawQuery != "" {
			if r.URL.Query().Get("skip") != "0" || r.URL.Query().Get("take") != "2" {
				t.Fatalf("unexpected paginated query %q", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`[{"uuid":"dep1","status":"finished","git_commit_sha":"` + frontDoorTestSHA + `","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]`))
	}))
	defer server.Close()

	svc := configuredPlatformService(t, config.ModeReadOnly, server.URL)
	deployment, err := svc.latestManagedApplicationDeployment("app1")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || deployment.Status != "finished" || deployment.Commit != frontDoorTestSHA {
		t.Fatalf("calls=%d deployment=%+v", calls, deployment)
	}
}

func TestLatestManagedApplicationDeploymentRejectsRepeatedEmptyResponse(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := configuredPlatformService(t, config.ModeReadOnly, server.URL)
	_, err := svc.latestManagedApplicationDeployment("app1")
	if err == nil || !strings.Contains(err.Error(), "response is empty") || calls != 2 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestLatestManagedApplicationDeploymentDoesNotFallbackForMalformedJSON(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RawQuery == "" {
			t.Fatal("malformed non-empty response triggered fallback")
		}
		_, _ = w.Write([]byte("{"))
	}))
	defer server.Close()

	svc := configuredPlatformService(t, config.ModeReadOnly, server.URL)
	_, err := svc.latestManagedApplicationDeployment("app1")
	if err == nil || !strings.Contains(err.Error(), "decoding managed application deployments") || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestDecodeManagedApplicationDeploymentsRejectsExcessiveRecords(t *testing.T) {
	var body strings.Builder
	body.WriteByte('[')
	for i := 0; i <= managedDeploymentDecodeLimit; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"uuid":"dep%d","status":"finished","git_commit_sha":"%s","created_at":"2026-08-01T20:00:00Z"}`, i, frontDoorTestSHA)
	}
	body.WriteByte(']')
	if _, err := decodeManagedApplicationDeployments(body.String()); err == nil || !strings.Contains(err.Error(), "max") {
		t.Fatalf("oversized deployment response error=%v", err)
	}
}

func TestLatestManagedApplicationDeploymentAcceptsLargeBoundedLogs(t *testing.T) {
	largeLog := strings.Repeat("x", (1<<20)+4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/deployments/applications/app1" || r.URL.Query().Get("take") != "2" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = fmt.Fprintf(w, `{"count":2,"deployments":[{"uuid":"depnew","status":"finished","git_commit_sha":"%s","created_at":"2026-08-01T20:01:00Z","updated_at":"2026-08-01T20:02:00Z","logs":%q},{"uuid":"depold","status":"finished","git_commit_sha":"%s","created_at":"2026-08-01T20:00:00Z","updated_at":"2026-08-01T20:01:00Z"}]}`, frontDoorTestSHA, largeLog, frontDoorTestSHA)
	}))
	defer server.Close()

	svc := configuredPlatformService(t, config.ModeReadOnly, server.URL)
	deployment, err := svc.latestManagedApplicationDeployment("app1")
	if err != nil {
		t.Fatal(err)
	}
	if deployment.DeploymentUUID != "depnew" || deployment.Status != "finished" || deployment.Commit != frontDoorTestSHA {
		t.Fatalf("deployment=%+v", deployment)
	}
}

func TestCoolifyRequestBoundedRejectsOversizedBodyWithoutTruncatingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1025)))
	}))
	defer server.Close()

	client := NewCoolifyClient(server.URL, "fixture-token", nil)
	status, body, err := client.requestBounded(context.Background(), http.MethodGet, "/large", nil, 1024)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1024 bytes") {
		t.Fatalf("status=%d body_len=%d error=%v", status, len(body), err)
	}
	if status != http.StatusOK || body != "" {
		t.Fatalf("status=%d body_len=%d", status, len(body))
	}
}
