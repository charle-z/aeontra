package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestVerifyManagedFrontDoorCoordinatorRuntimeRejectsWrongBackendCommitAndUnexpectedEnvironment(t *testing.T) {
	environment := ""
	backendCommit := frontDoorTestSHA
	compareStatus := "identical"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/repos/acme/mcp-devbox/compare/" + frontDoorTestSHA + "..." + frontDoorTestSHA:
			_, _ = w.Write([]byte(`{"status":"` + compareStatus + `"}`))
		case "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(environment))
		case "/api/v1/deployments/applications/coord1":
			writeManagedDeploymentList(w, "coord1", "finished", frontDoorTestSHA)
		case "/api/v1/deployments/applications/front1":
			writeManagedDeploymentList(w, "front1", "finished", frontDoorTestSHA)
		case "/api/v1/deployments/applications/" + managedBackendAppUUID:
			writeManagedDeploymentList(w, managedBackendAppUUID, "finished", backendCommit)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	environment = coordinatorRuntimeEnvironment(server.URL, "idle", "")

	svc := configuredCoordinatorService(t, config.ModeReadOnly, server.URL)
	front, backend, coordinator := coordinatorRuntimeApplications()
	backendCommit = strings.Repeat("b", 40)
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "approved branch") {
		t.Fatalf("wrong backend deployment commit accepted: %v", err)
	}

	backendCommit = frontDoorTestSHA
	compareStatus = "diverged"
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "main history") {
		t.Fatalf("diverged coordinator deployment accepted: %v", err)
	}
	compareStatus = "identical"
	unexpected, _ := json.Marshal(coordinatorRuntimeEnvironmentEntry("MCP_FRONT_DOOR_UNEXPECTED", "bad"))
	environment = strings.TrimSuffix(coordinatorRuntimeEnvironment(server.URL, "idle", ""), "]") + "," + string(unexpected) + "]"
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "unexpected environment key") {
		t.Fatalf("unexpected managed key accepted: %v", err)
	}

	base := strings.TrimSuffix(coordinatorRuntimeEnvironment(server.URL, "idle", ""), "]")
	catalogRequest, _ := json.Marshal(coordinatorRuntimeEnvironmentEntry(managedCatalogRequestEnv, "opaque-request"))
	catalogToken, _ := json.Marshal(coordinatorRuntimeEnvironmentEntry(managedCatalogMCPTokenEnv, "opaque-token"))
	environment = base + "," + string(catalogRequest) + "," + string(catalogToken) + "]"
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err != nil {
		t.Fatalf("managed catalog rollout environment rejected: %v", err)
	}
	environment = base + "," + string(catalogRequest) + "]"
	if _, err := svc.verifyManagedFrontDoorCoordinatorRuntime(coordinator, front, backend); err == nil || !strings.Contains(err.Error(), "catalog rollout environment is incomplete") {
		t.Fatalf("partial catalog rollout environment accepted: %v", err)
	}
}

func TestVerifyManagedFrontDoorCoordinatorRuntimeUsesAuthenticatedCommentsWithoutValues(t *testing.T) {
	const requestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coordinatorCommit := strings.Repeat("a", 40)
	environment := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main", "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case "/repos/acme/mcp-devbox/compare/" + coordinatorCommit + "..." + frontDoorTestSHA:
			_, _ = w.Write([]byte(`{"status":"ahead"}`))
		case "/api/v1/applications/coord1/envs":
			_, _ = w.Write([]byte(environment))
		case "/api/v1/deployments/applications/coord1":
			writeManagedDeploymentList(w, "coord1", "finished", coordinatorCommit)
		case "/api/v1/deployments/applications/front1":
			writeManagedDeploymentList(w, "front1", "finished", frontDoorTestSHA)
		case "/api/v1/deployments/applications/" + managedBackendAppUUID:
			writeManagedDeploymentList(w, managedBackendAppUUID, "finished", frontDoorTestSHA)
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
	if identity.CoordinatorCommit != coordinatorCommit || identity.MainCommit != frontDoorTestSHA || identity.Protocol != "2024-11-05" || identity.CatalogHash != frontDoorTestCatalog {
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
		UUID: "front1", Name: managedFrontDoorName, Status: "running:healthy",
		GitRepository: "acme/mcp-devbox", GitBranch: managedFrontDoorBranch, GitCommitSHA: "HEAD",
		BuildPack: "dockerfile", Dockerfile: managedFrontDoorDockerfile, PortsExposes: managedFrontDoorPort,
		HealthcheckPath: managedFrontDoorHealthPath,
	}
	backend := platformApplication{
		UUID: managedBackendAppUUID, Name: "mcp-devbox", Status: "running:healthy",
		GitRepository: "acme/mcp-devbox", GitBranch: "main", GitCommitSHA: "HEAD",
	}
	coordinator := platformApplication{
		UUID: "coord1", Name: managedFrontDoorCoordinatorName, Status: "running:healthy",
		GitRepository: "acme/mcp-devbox", GitBranch: managedFrontDoorCoordinatorBranch, GitCommitSHA: "HEAD",
		BuildPack: "dockerfile", Dockerfile: managedFrontDoorCoordinatorDockerfile,
		PortsExposes: managedFrontDoorCoordinatorPort, HealthcheckPath: managedFrontDoorCoordinatorHealthPath,
		DockerRunOptions: managedFrontDoorCoordinatorDockerOptions,
	}
	return front, backend, coordinator
}
