package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestVerifyManagedFrontDoorCoordinatorRuntimeRejectsStaleCommitAndUnexpectedEnvironment(t *testing.T) {
	environment := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(environment))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	environment = coordinatorRuntimeEnvironment(server.URL, "idle", "")

	svc := configuredCoordinatorService(t, config.ModeReadOnly, server.URL)
	front, backend, coordinator := coordinatorRuntimeApplications()
	coordinator.GitCommitSHA = strings.Repeat("b", 40)
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "commits") {
		t.Fatalf("stale coordinator commit accepted: %v", err)
	}

	coordinator.GitCommitSHA = frontDoorTestSHA
	unexpected, _ := json.Marshal(coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_UNEXPECTED", "bad"))
	environment = strings.TrimSuffix(coordinatorRuntimeEnvironment(server.URL, "idle", ""), "]") + "," + string(unexpected) + "]"
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "unexpected environment key") {
		t.Fatalf("unexpected managed key accepted: %v", err)
	}
}

func TestVerifyManagedFrontDoorCoordinatorRuntimeUsesAuthenticatedCommentsWithoutValues(t *testing.T) {
	const requestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	environment := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(environment))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	environment = coordinatorRuntimeEnvironment(server.URL, "cutover", requestID)

	svc := configuredCoordinatorService(t, config.ModeReadOnly, server.URL)
	front, backend, coordinator := coordinatorRuntimeApplications()
	coordinator.Description = `mcp-front-door-coordinator:v1 {"schema_version":1,"revision":1,"request_id":"` + requestID + `","target":"cutover","state":"running","phase":"add-backend-origin","updated_at":"2026-07-31T15:00:00Z"}`
	identity, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Protocol != "2024-11-05" || identity.CatalogHash != frontDoorTestCatalog {
		t.Fatalf("identity=%+v", identity)
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(environment), &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry["key"] == "MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID" {
			comment := entry["comment"].(string)
			replacement := byte('0')
			if comment[len(comment)-1] == replacement {
				replacement = '1'
			}
			entry["comment"] = comment[:len(comment)-1] + string(replacement)
		}
	}
	tampered, _ := json.Marshal(entries)
	environment = string(tampered)
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "REQUEST_ID") {
		t.Fatalf("tampered request id comment accepted: %v", err)
	}
}

func coordinatorRuntimeApplications() (platformApplication, platformApplication, platformApplication) {
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
		GitRepository: "acme/mcp-devbox", GitBranch: managedFrontDoorCoordinatorBranch, GitCommitSHA: frontDoorTestSHA,
		BuildPack: "dockerfile", Dockerfile: managedFrontDoorCoordinatorDockerfile,
		PortsExposes: managedFrontDoorCoordinatorPort, HealthcheckPath: managedFrontDoorCoordinatorHealthPath,
	}
	return front, backend, coordinator
}
