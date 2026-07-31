package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestVerifyManagedFrontDoorCoordinatorRuntimeRejectsStaleCommitAndUnexpectedEnvironment(t *testing.T) {
	environment := coordinatorRuntimeEnvironment("", "idle", "")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/api/v1/applications/coord1/envs":
			body := strings.Replace(environment, `"value":""`, `"value":"`+server.URL+`"`, 1)
			_, _ = w.Write([]byte(body))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	svc := configuredCoordinatorService(t, config.ModeReadOnly, server.URL)
	front := platformApplication{
		UUID: "front1", Name: managedFrontDoorName, Status: "running:healthy", DeploymentStatus: "finished",
		GitRepository: "acme/mcp-devbox", GitBranch: managedFrontDoorBranch, GitCommitSHA: frontDoorTestSHA,
		BuildPack: "dockerfile", Dockerfile: managedFrontDoorDockerfile, PortsExposes: managedFrontDoorPort,
		HealthcheckPath: managedFrontDoorHealthPath,
	}
	backend := platformApplication{
		UUID: managedBackendAppUUID, Name: "mcp-devbox", Status: "running:healthy", DeploymentStatus: "finished",
		GitRepository: "acme/mcp-devbox", GitBranch: "main", GitCommitSHA: frontDoorTestSHA,
	}
	coordinator := platformApplication{
		UUID: "coord1", Name: managedFrontDoorCoordinatorName, Status: "running:healthy", DeploymentStatus: "finished",
		GitRepository: "acme/mcp-devbox", GitBranch: managedFrontDoorCoordinatorBranch,
		GitCommitSHA: strings.Repeat("b", 40), BuildPack: "dockerfile", Dockerfile: managedFrontDoorCoordinatorDockerfile,
		PortsExposes: managedFrontDoorCoordinatorPort, HealthcheckPath: managedFrontDoorCoordinatorHealthPath,
	}
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "commits") {
		t.Fatalf("stale coordinator commit accepted: %v", err)
	}

	coordinator.GitCommitSHA = frontDoorTestSHA
	environment = strings.TrimSuffix(coordinatorRuntimeEnvironment(server.URL, "idle", ""), "]") + `,{"key":"MCP_FRONT_DOOR_UNEXPECTED","value":"bad","is_preview":false}]`
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "unexpected environment key") {
		t.Fatalf("unexpected managed key accepted: %v", err)
	}
}

func TestManagedCoordinatorSecretMatchesOnlyExactOrMaskedValues(t *testing.T) {
	for _, tc := range []struct {
		got  string
		want bool
	}{
		{got: "coolify-token", want: true},
		{got: "********", want: true},
		{got: "", want: false},
		{got: "other-token", want: false},
		{got: "***x***", want: false},
	} {
		if got := managedCoordinatorSecretMatches(tc.got, "coolify-token"); got != tc.want {
			t.Fatalf("managedCoordinatorSecretMatches(%q)=%t want=%t", tc.got, got, tc.want)
		}
	}
}
