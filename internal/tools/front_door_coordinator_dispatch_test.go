package tools

import (
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func TestManagedFrontDoorCoordinatorDispatchPreservesPublishedRequest(t *testing.T) {
	const requestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	description := `mcp-fdc:v2 {"r":7,"q":"` + requestID + `","t":"c","s":"s","p":"z","u":1785633755}`

	target, gotRequestID, err := managedFrontDoorCoordinatorDispatch(description, true)
	if err != nil {
		t.Fatal(err)
	}
	if target != frontdoorcoordinator.TargetCutover || gotRequestID != requestID {
		t.Fatalf("target=%q request_id=%q", target, gotRequestID)
	}
}

func TestManagedFrontDoorCoordinatorDispatchDefaultsNewWorkerToIdle(t *testing.T) {
	target, requestID, err := managedFrontDoorCoordinatorDispatch("", false)
	if err != nil {
		t.Fatal(err)
	}
	if target != frontdoorcoordinator.TargetIdle || requestID != "" {
		t.Fatalf("target=%q request_id=%q", target, requestID)
	}
}

func TestManagedFrontDoorCoordinatorDispatchRejectsActiveCatalogRollout(t *testing.T) {
	description := catalogrollout.PublishedStatusPrefix + `{"r":7,"q":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":"running","p":"deploy-backend","u":1785633755}`
	if _, _, err := managedFrontDoorCoordinatorDispatch(description, true); err == nil || !strings.Contains(err.Error(), "catalog-aware") {
		t.Fatalf("active catalog rollout was not rejected: %v", err)
	}
}

func TestManagedFrontDoorCoordinatorDispatchIgnoresTerminalCatalogRollout(t *testing.T) {
	description := catalogrollout.PublishedStatusPrefix + `{"r":7,"q":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","s":"succeeded","p":"complete","u":1785633755}`
	target, requestID, err := managedFrontDoorCoordinatorDispatch(description, true)
	if err != nil || target != frontdoorcoordinator.TargetIdle || requestID != "" {
		t.Fatalf("target=%q request_id=%q err=%v", target, requestID, err)
	}
}
