package frontdoorcoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompactPublishedStatusRoundTripFitsDescriptionLimit(t *testing.T) {
	t.Parallel()
	requestID := strings.Repeat("a", 43)
	now := time.Date(2026, 8, 2, 0, 45, 12, 987654321, time.UTC)
	topology := Topology{
		FrontDomain: FrontTemporaryOrigin, FrontBackendURL: BackendOrigin,
		BackendDomains: FrontPublicOrigin + "," + BackendOrigin,
	}
	cases := []Status{
		{SchemaVersion: 1, Target: TargetIdle, State: StateIdle},
		{SchemaVersion: 1, Revision: 1, RequestID: requestID, Target: TargetCutover, State: StateQueued, UpdatedAt: now},
		{SchemaVersion: 1, Revision: 2, RequestID: requestID, Target: TargetCutover, State: StateRunning, Phase: PhaseReleasePublicBackend, Topology: topology, UpdatedAt: now},
		{SchemaVersion: 1, Revision: 3, RequestID: requestID, Target: TargetCutover, State: StateRunning, Phase: PhaseAssignPublicFront, Topology: topology, DeploymentID: strings.Repeat("d", maxPublishedDeploymentIDLen), UpdatedAt: now},
		{SchemaVersion: 1, Revision: 4, RequestID: requestID, Target: TargetCutover, RecoveryTarget: TargetRollback, State: StateCompensating, Phase: PhaseAssignPublicFront, Topology: topology, Reason: "assign-public-front_failed", UpdatedAt: now},
		{SchemaVersion: 1, Revision: 5, RequestID: requestID, Target: TargetCutover, State: StateSucceeded, Phase: PhaseComplete, Topology: topology, UpdatedAt: now},
		{SchemaVersion: 1, Revision: ^uint64(0), RequestID: requestID, Target: TargetRollback, RecoveryTarget: TargetCutover, State: StateFailed, Phase: PhaseSwitchFrontPublicBackend, Topology: topology, DeploymentID: strings.Repeat("z", maxPublishedDeploymentIDLen), Reason: "switch-front-public-backend_failed_compensation_budget_exhausted", UpdatedAt: now},
	}
	for _, original := range cases {
		description, err := encodePublishedStatus(original)
		if err != nil {
			t.Fatalf("encode %+v: %v", original, err)
		}
		if len(description) > maxPublishedStatusDescriptionLen {
			t.Fatalf("description length=%d max=%d: %s", len(description), maxPublishedStatusDescriptionLen, description)
		}
		if !strings.HasPrefix(description, compactStatusDescriptionPrefix) {
			t.Fatalf("unexpected prefix: %q", description)
		}
		decoded, present, err := DecodePublishedStatus(description)
		if err != nil || !present {
			t.Fatalf("decode present=%t err=%v description=%q", present, err, description)
		}
		if decoded.SchemaVersion != 1 || decoded.Revision != original.Revision || decoded.RequestID != original.RequestID || decoded.Target != original.Target || decoded.RecoveryTarget != original.RecoveryTarget || decoded.State != original.State || decoded.Phase != original.Phase || decoded.DeploymentID != original.DeploymentID || decoded.Reason != original.Reason {
			t.Fatalf("round trip mismatch\noriginal=%+v\ndecoded=%+v", original, decoded)
		}
		if original.UpdatedAt.IsZero() {
			if !decoded.UpdatedAt.IsZero() {
				t.Fatalf("zero update time changed: %s", decoded.UpdatedAt)
			}
		} else if !decoded.UpdatedAt.Equal(original.UpdatedAt.Truncate(time.Second)) {
			t.Fatalf("update time=%s want=%s", decoded.UpdatedAt, original.UpdatedAt.Truncate(time.Second))
		}
		if decoded.Topology != (Topology{}) {
			t.Fatalf("published envelope duplicated topology: %+v", decoded.Topology)
		}
	}
}

func TestCompactPublishedReasonsRoundTripFixedContract(t *testing.T) {
	t.Parallel()
	bases := []string{
		"topology_invalid",
		"topology_read_failed",
		"topology_read_budget_exhausted",
		"transition_budget_exhausted",
	}
	for _, phase := range []Phase{
		PhaseAddBackendOrigin,
		PhaseSwitchFrontBackend,
		PhaseReleasePublicBackend,
		PhaseAssignPublicFront,
		PhaseMoveFrontTemporary,
		PhaseRestorePublicBackend,
		PhaseSwitchFrontPublicBackend,
		PhaseRemoveAlternateBackend,
	} {
		bases = append(bases, string(phase)+"_failed")
	}
	suffixes := []string{"", "_compensation_unavailable", "_compensated", "_compensation_topology_invalid", "_compensation_budget_exhausted"}
	for _, base := range bases {
		for _, suffix := range suffixes {
			reason := base + suffix
			code, err := encodePublishedReason(reason)
			if err != nil {
				t.Fatalf("encode reason %q: %v", reason, err)
			}
			decoded, err := decodePublishedReason(code)
			if err != nil || decoded != reason {
				t.Fatalf("reason=%q code=%q decoded=%q err=%v", reason, code, decoded, err)
			}
		}
	}
	for _, reason := range []string{"unknown", "complete_failed", "topology_invalid_unknown"} {
		if _, err := encodePublishedReason(reason); err == nil {
			t.Fatalf("unknown reason accepted: %q", reason)
		}
	}
}

func TestPublishStatusUsesCompactDescriptionWithinCoolifyLimit(t *testing.T) {
	t.Parallel()
	requestID := strings.Repeat("p", 43)
	status := Status{
		SchemaVersion: 1, Revision: 42, RequestID: requestID,
		Target: TargetCutover, RecoveryTarget: TargetRollback,
		State: StateCompensating, Phase: PhaseAssignPublicFront,
		Topology:     Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: BackendOrigin, BackendDomains: FrontPublicOrigin + "," + BackendOrigin},
		DeploymentID: strings.Repeat("e", maxPublishedDeploymentIDLen),
		Reason:       "assign-public-front_failed_compensation_budget_exhausted",
		UpdatedAt:    time.Date(2026, 8, 2, 0, 50, 0, 0, time.UTC),
	}
	legacy, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if len(statusDescriptionPrefix)+len(legacy) <= maxPublishedStatusDescriptionLen {
		t.Fatalf("legacy fixture did not exceed practical description limit: %d", len(statusDescriptionPrefix)+len(legacy))
	}

	var mu sync.Mutex
	published := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/applications/coord1" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload.Description) > maxPublishedStatusDescriptionLen {
			http.Error(w, "description too long", http.StatusUnprocessableEntity)
			return
		}
		mu.Lock()
		published = payload.Description
		mu.Unlock()
		_, _ = w.Write([]byte(`{"uuid":"coord1"}`))
	}))
	defer server.Close()

	client, err := NewClient(validClientConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	if err := client.PublishStatus(context.Background(), status); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	description := published
	mu.Unlock()
	if description == "" || len(description) > maxPublishedStatusDescriptionLen {
		t.Fatalf("published description length=%d", len(description))
	}
	decoded, present, err := DecodePublishedStatus(description)
	if err != nil || !present || decoded.Revision != status.Revision || decoded.State != status.State || decoded.Reason != status.Reason {
		t.Fatalf("decoded=%+v present=%t err=%v", decoded, present, err)
	}
}

func TestDecodePublishedStatusRetainsLegacyV1Compatibility(t *testing.T) {
	t.Parallel()
	status := Status{
		SchemaVersion: 1, Revision: 7, RequestID: strings.Repeat("l", 43),
		Target: TargetRollback, State: StateRunning, Phase: PhaseMoveFrontTemporary,
		Topology:  Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin},
		UpdatedAt: time.Date(2026, 8, 2, 0, 55, 0, 0, time.UTC),
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	decoded, present, err := DecodePublishedStatus(statusDescriptionPrefix + string(data))
	if err != nil || !present || decoded.Revision != status.Revision || decoded.Topology != status.Topology {
		t.Fatalf("legacy decode=%+v present=%t err=%v", decoded, present, err)
	}
}

func TestDecodeCompactPublishedStatusRejectsUnknownOrOversizedData(t *testing.T) {
	t.Parallel()
	cases := []string{
		compactStatusDescriptionPrefix + `{"r":1,"q":"` + strings.Repeat("a", 43) + `","t":"c","s":"q","u":1785630000,"unknown":true}`,
		compactStatusDescriptionPrefix + `{"r":1,"q":"` + strings.Repeat("a", 43) + `","t":"x","s":"q","u":1785630000}`,
		compactStatusDescriptionPrefix + `{"r":1,"q":"` + strings.Repeat("a", 43) + `","t":"c","s":"q","x":"unknown","u":1785630000}`,
		compactStatusDescriptionPrefix + `{"r":1,"q":"` + strings.Repeat("a", 43) + `","t":"c","s":"q","u":1785630000}{}`,
		compactStatusDescriptionPrefix + strings.Repeat("x", maxPublishedStatusDescriptionLen),
	}
	for _, description := range cases {
		if _, present, err := DecodePublishedStatus(description); !present || err == nil {
			t.Fatalf("invalid compact description accepted present=%t err=%v description=%s", present, err, fmt.Sprintf("%.80s", description))
		}
	}
}
