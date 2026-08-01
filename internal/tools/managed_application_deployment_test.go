package tools

import (
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
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/deployments/applications/app1" || r.URL.Query().Get("skip") != "0" || r.URL.Query().Get("take") != "20" {
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
