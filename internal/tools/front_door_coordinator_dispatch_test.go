package tools

import (
	"testing"

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
