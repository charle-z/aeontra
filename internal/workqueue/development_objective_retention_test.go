package workqueue

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestDevelopmentObjectiveRetentionPreservesActiveRecentAndReplay(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "objective-retention-preserve"})
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	active := developmentObjectiveFixture(t, "objective-active-old")
	recent := retentionCancelledObjective(t, "objective-recent")
	boundary := retentionCancelledObjective(t, "objective-boundary")
	expired := retentionCancelledObjective(t, "objective-expired")
	retentionInsertObjective(t, store, active, now.Add(-40*24*time.Hour))
	retentionInsertObjective(t, store, recent, now.Add(-29*24*time.Hour))
	retentionInsertObjective(t, store, boundary, now.Add(-30*24*time.Hour))
	retentionInsertObjective(t, store, expired, now.Add(-30*24*time.Hour-time.Nanosecond))
	if _, _, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err != nil {
		t.Fatal(err)
	}
	for _, objective := range []development.Objective{active, recent, boundary} {
		if got, found, err := store.DevelopmentObjective(objective.ObjectiveID); err != nil || !found || got.Revision != objective.Revision {
			t.Fatalf("retained %s found=%t err=%v", objective.ObjectiveID, found, err)
		}
		if _, created, err := store.SaveDevelopmentObjective(objective); err != nil || created {
			t.Fatalf("replay %s created=%t err=%v", objective.ObjectiveID, created, err)
		}
	}
	if _, found, err := store.DevelopmentObjective(expired.ObjectiveID); err != nil || found {
		t.Fatalf("expired found=%t err=%v", found, err)
	}
}

func TestDevelopmentObjectiveRetentionRetainedRequestPinsEveryState(t *testing.T) {
	for _, state := range []DevelopmentRequestState{DevelopmentRequestActive, DevelopmentRequestCompleted, DevelopmentRequestFailed, DevelopmentRequestCancelled} {
		t.Run(string(state), func(t *testing.T) {
			store := openTestStore(t, Config{ControllerID: "objective-retention-request"})
			now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			objective := retentionCancelledObjective(t, "objective-pinned-request")
			retentionInsertObjective(t, store, objective, now.Add(-40*24*time.Hour))
			request := developmentRequestFixture()
			request.State, request.ObjectiveID = state, objective.ObjectiveID
			if state == DevelopmentRequestFailed {
				request.Reason = DevelopmentRequestReasonOperationFailed
			}
			body, digest, err := marshalDevelopmentRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			stamp := now.Add(-40 * 24 * time.Hour).UnixNano()
			if _, err := store.db.Exec(`INSERT INTO development_requests(request_id,revision,state,key_digest,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, request.ID, request.Revision, request.State, request.KeyDigest, digest, body, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.DevelopmentObjective(objective.ObjectiveID); err != nil || !found {
				t.Fatalf("request protector lost: found=%t err=%v", found, err)
			}
			if _, found, err := store.DevelopmentRequest(request.ID); err != nil || !found {
				t.Fatalf("retained request lost: found=%t err=%v", found, err)
			}
		})
	}
}

func TestDevelopmentObjectiveRetentionProvisionJobPinsBoundAndLostBinding(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			store := openTestStore(t, Config{ControllerID: "objective-retention-job"})
			now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			objective := developmentObjectiveFixture(t, "objective-pinned-job")
			var err error
			objective.Scope, err = development.NewObjectiveScope("project", "parrot")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := development.NewProvisionPlan("toolchain", "development.toolchain", "go", development.ClassWorkcell, "", []development.CapabilityID{"toolchain.go"})
			if err != nil {
				t.Fatal(err)
			}
			objective, err = objective.PlanProvisioning("validate", "provision-retention", objectiveSourceDigest("a"), plan)
			if err != nil {
				t.Fatal(err)
			}
			job, _, err := store.Enqueue(Spec{IdempotencyKey: "development:provision-retention", Workspace: "project", Pool: plan.Pool, Profile: plan.Profile, PayloadHash: objectiveSourceDigest("a")})
			if err != nil {
				t.Fatal(err)
			}
			if bound {
				objective, err = objective.BindProvisionJob("validate", "provision-retention", job.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			objective, err = objective.Cancel()
			if err != nil {
				t.Fatal(err)
			}
			retentionInsertObjective(t, store, objective, now.Add(-40*24*time.Hour))
			if _, _, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.DevelopmentObjective(objective.ObjectiveID); err != nil || !found {
				t.Fatalf("active job lost owner: found=%t err=%v", found, err)
			}
			if _, err := store.Cancel(job.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-after-job")); err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.DevelopmentObjective(objective.ObjectiveID); err != nil || found {
				t.Fatalf("terminal job still pins: found=%t err=%v", found, err)
			}
			if _, found, err := store.Get(job.ID); err != nil || !found {
				t.Fatalf("job receipt lost: found=%t err=%v", found, err)
			}
		})
	}
}

func TestDevelopmentObjectiveRetentionCorruptionRollsBack(t *testing.T) {
	for _, corrupt := range []string{"objective", "timestamp", "request"} {
		t.Run(corrupt, func(t *testing.T) {
			store := openTestStore(t, Config{ControllerID: "objective-retention-corrupt"})
			now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			for _, id := range []string{"objective-old-a", "objective-old-b"} {
				retentionInsertObjective(t, store, retentionCancelledObjective(t, id), now.Add(-40*24*time.Hour))
			}
			if corrupt == "objective" {
				if _, err := store.db.Exec(`UPDATE development_objectives SET record_json=? WHERE objective_id=?`, []byte("{}"), "objective-old-b"); err != nil {
					t.Fatal(err)
				}
			} else if corrupt == "timestamp" {
				if _, err := store.db.Exec(`UPDATE development_objectives SET created_at=updated_at+1 WHERE objective_id=?`, "objective-old-b"); err != nil {
					t.Fatal(err)
				}
			} else {
				request := developmentRequestFixture()
				if _, _, err := store.SaveDevelopmentRequest(request); err != nil {
					t.Fatal(err)
				}
				if _, err := store.db.Exec(`UPDATE development_requests SET record_json=? WHERE request_id=?`, []byte("{}"), request.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, created, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err == nil || created || !strings.Contains(err.Error(), "corrupt") {
				t.Fatalf("corruption admitted/pruned: created=%t err=%v", created, err)
			}
			var count int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM development_objectives`).Scan(&count); err != nil || count != 2 {
				t.Fatalf("rollback count=%d err=%v", count, err)
			}
		})
	}
}

func TestDevelopmentObjectiveRetentionIncludesAcceptedAndFailed(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "objective-retention-terminal"})
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for _, state := range []development.ObjectiveState{development.ObjectiveAccepted, development.ObjectiveFailed} {
		objective := developmentObjectiveFixture(t, "objective-old-"+string(state))
		environment := developmentEnvironmentFixture(t, "retention-workcell", development.ClassWorkcell, 1, "toolchain.go")
		var err error
		objective, _, err = objective.PlanAttempt("validate", "attempt-retention", objectiveSourceDigest("a"), []development.EnvironmentAttestation{environment})
		if err != nil {
			t.Fatal(err)
		}
		objective, err = objective.StartAttempt("validate")
		if err != nil {
			t.Fatal(err)
		}
		if state == development.ObjectiveAccepted {
			objective, err = objective.CompleteAttempt("validate")
			if err == nil {
				objective, err = objective.Accept()
			}
		} else {
			objective, _, err = objective.FailAttempt("validate", development.FailurePolicyDenied)
		}
		if err != nil || objective.State != state {
			t.Fatalf("terminal fixture state=%s err=%v", objective.State, err)
		}
		retentionInsertObjective(t, store, objective, now.Add(-40*24*time.Hour))
		// Existing terminal identities replay without initiating maintenance.
		if _, created, err := store.SaveDevelopmentObjective(objective); err != nil || created {
			t.Fatalf("old replay created=%t err=%v", created, err)
		}
	}
	if _, _, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err != nil {
		t.Fatal(err)
	}
	for _, state := range []development.ObjectiveState{development.ObjectiveAccepted, development.ObjectiveFailed} {
		if _, found, err := store.DevelopmentObjective("objective-old-" + string(state)); err != nil || found {
			t.Fatalf("expired %s found=%t err=%v", state, found, err)
		}
	}
}

func TestDevelopmentObjectiveRetentionCorruptProvisionJobPreservesEvidence(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "objective-retention-corrupt-job"})
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	objective := developmentObjectiveFixture(t, "objective-corrupt-job")
	var err error
	objective.Scope, err = development.NewObjectiveScope("project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := development.NewProvisionPlan("toolchain", "development.toolchain", "go", development.ClassWorkcell, "", []development.CapabilityID{"toolchain.go"})
	if err != nil {
		t.Fatal(err)
	}
	objective, err = objective.PlanProvisioning("validate", "provision-corrupt", objectiveSourceDigest("a"), plan)
	if err != nil {
		t.Fatal(err)
	}
	objective, err = objective.Cancel()
	if err != nil {
		t.Fatal(err)
	}
	retentionInsertObjective(t, store, objective, now.Add(-40*24*time.Hour))
	job, _, err := store.Enqueue(Spec{IdempotencyKey: "development:provision-corrupt", Workspace: "project", Pool: plan.Pool, Profile: plan.Profile, PayloadHash: objectiveSourceDigest("a")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE jobs SET state=? WHERE job_id=?`, "corrupt", job.ID); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.SaveDevelopmentObjective(developmentObjectiveFixture(t, "objective-next")); err == nil || created || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt job admitted/pruned created=%t err=%v", created, err)
	}
	if _, found, err := store.DevelopmentObjective(objective.ObjectiveID); err != nil || !found {
		t.Fatalf("job evidence owner lost found=%t err=%v", found, err)
	}
}

func TestDevelopmentObjectiveRetentionReclaimsExpiredTerminalCapacity(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "objective-retention-capacity"})
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < MaxDevelopmentObjectives; i++ {
		objective, err := developmentObjectiveFixture(t, fmt.Sprintf("expired-objective-%04d", i)).Cancel()
		if err != nil {
			t.Fatal(err)
		}
		body, digest, err := objective.MarshalRecord()
		if err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-31 * 24 * time.Hour).UnixNano()
		if _, err := tx.Exec(`INSERT INTO development_objectives(objective_id,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, objective.ObjectiveID, objective.Revision, objective.State, digest, body, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	next := developmentObjectiveFixture(t, "objective-retention-next")
	if _, created, err := store.SaveDevelopmentObjective(next); err != nil || !created {
		t.Fatalf("expired terminal history blocked admission: created=%t err=%v", created, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM development_objectives`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func retentionInsertObjective(t *testing.T, store *Store, objective development.Objective, at time.Time) {
	t.Helper()
	body, digest, err := objective.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO development_objectives(objective_id,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, objective.ObjectiveID, objective.Revision, objective.State, digest, body, at.UnixNano(), at.UnixNano()); err != nil {
		t.Fatal(err)
	}
}

func retentionCancelledObjective(t *testing.T, id string) development.Objective {
	t.Helper()
	objective, err := developmentObjectiveFixture(t, id).Cancel()
	if err != nil {
		t.Fatal(err)
	}
	return objective
}
