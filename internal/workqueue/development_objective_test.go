package workqueue

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestDevelopmentObjectivePersistsAcrossRestartAndAdvancesByRevision(t *testing.T) {
	root := openObjectiveStoreRoot(t)
	store, err := Open(Config{Root: root, ControllerID: "controller-objective"})
	if err != nil {
		t.Fatal(err)
	}

	objective := developmentObjectiveFixture(t, "objective-persist-1")
	stored, created, err := store.SaveDevelopmentObjective(objective)
	if err != nil || !created || stored.Revision != 1 {
		t.Fatalf("initial stored=%+v created=%t err=%v", stored, created, err)
	}

	environment := developmentEnvironmentFixture(t, "l3", development.ClassWorkcell, 1, "toolchain.go")
	objective, _, err = objective.PlanAttempt("validate", "attempt-1", objectiveSourceDigest("a"), []development.EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err = store.SaveDevelopmentObjective(objective)
	if err != nil || created || stored.Revision != 2 {
		t.Fatalf("updated stored=%+v created=%t err=%v", stored, created, err)
	}

	replayed, created, err := store.SaveDevelopmentObjective(objective)
	if err != nil || created || replayed.Revision != objective.Revision {
		t.Fatalf("idempotent replay=%+v created=%t err=%v", replayed, created, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, ControllerID: "controller-objective"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	recovered, found, err := store.DevelopmentObjective(objective.ObjectiveID)
	if err != nil || !found || recovered.Revision != objective.Revision || !recovered.Valid() {
		t.Fatalf("recovered=%+v found=%t err=%v", recovered, found, err)
	}
	summaries, err := store.DevelopmentObjectiveSummaries(10)
	if err != nil || len(summaries) != 1 || summaries[0].ObjectiveID != objective.ObjectiveID ||
		summaries[0].Revision != objective.Revision || summaries[0].UpdatedAt.IsZero() {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}

	started, err := recovered.StartAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveDevelopmentObjective(started); err != nil {
		t.Fatalf("restart continuation was not persisted: %v", err)
	}
}

func TestDevelopmentObjectiveRejectsStaleRevisionAndPolicyRewrite(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-objective-cas"})
	objective := developmentObjectiveFixture(t, "objective-cas-1")
	broadPolicy, err := development.NewResolutionPolicy(
		development.TierIsolatedRunner,
		development.ClassWorkcell,
		development.ClassIsolatedRunner,
	)
	if err != nil {
		t.Fatal(err)
	}
	objective.Policy = broadPolicy
	if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
		t.Fatal(err)
	}

	environment := developmentEnvironmentFixture(t, "l3", development.ClassWorkcell, 1, "toolchain.go")
	next, _, err := objective.PlanAttempt("validate", "attempt-1", objectiveSourceDigest("a"), []development.EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveDevelopmentObjective(next); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.SaveDevelopmentObjective(objective); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale objective was accepted: %v", err)
	}

	forged := next
	forged.Revision++
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	forged.Policy = policy
	if !forged.Valid() {
		t.Fatal("policy-rewrite fixture must be internally valid before transition validation")
	}
	if _, _, err := store.SaveDevelopmentObjective(forged); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("policy rewrite was accepted: %v", err)
	}
}

func TestDevelopmentObjectiveCorruptionFailsClosed(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-objective-corrupt"})
	objective := developmentObjectiveFixture(t, "objective-corrupt-1")
	if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE development_objectives SET record_digest=? WHERE objective_id=?`,
		"sha256:"+strings.Repeat("f", 64), objective.ObjectiveID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.DevelopmentObjective(objective.ObjectiveID); err == nil || found {
		t.Fatalf("corrupt objective was returned: found=%t err=%v", found, err)
	}
	if summaries, err := store.DevelopmentObjectiveSummaries(10); err == nil || summaries != nil {
		t.Fatalf("corrupt objective was returned by summary list: summaries=%+v err=%v", summaries, err)
	}
	if err := store.Integrity(); err == nil || !strings.Contains(err.Error(), "development objective semantic integrity") {
		t.Fatalf("corrupt objective passed integrity: %v", err)
	}
}

func TestDevelopmentObjectiveFirstWriteMustBeRevisionOne(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-objective-first"})
	objective := developmentObjectiveFixture(t, "objective-first-1")
	objective.Revision = 2
	if !objective.Valid() {
		t.Fatal("revision-two fixture unexpectedly invalid")
	}
	if _, _, err := store.SaveDevelopmentObjective(objective); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("non-durable history was accepted as a first write: %v", err)
	}
}

func TestDevelopmentObjectiveFirstWriteCannotSkipPlanning(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-objective-first-state"})
	objective := developmentObjectiveFixture(t, "objective-first-state")
	environment := developmentEnvironmentFixture(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	objective, _, err := objective.PlanAttempt("validate", "attempt-forged", objectiveSourceDigest("a"), []development.EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	objective.Revision = 1
	if !objective.Valid() {
		t.Fatal("forged first revision must be otherwise valid")
	}
	if _, _, err := store.SaveDevelopmentObjective(objective); err == nil {
		t.Fatal("initial persistence accepted an execution history without its planning revision")
	}
}

func developmentObjectiveFixture(t *testing.T, id string) development.Objective {
	t.Helper()
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := development.Requirements("toolchain.go")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewObjective(id, policy, []development.StepSpec{{
		StepID: "validate", Requirements: requirements,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return objective
}

func developmentEnvironmentFixture(t *testing.T, id string, class development.ExecutionClass, generation uint64, raw ...string) development.EnvironmentAttestation {
	t.Helper()
	capabilities, err := development.NewCapabilitySet(raw...)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := development.NewEnvironmentAttestation(id, class, generation, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func objectiveSourceDigest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func openObjectiveStoreRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/queue"
}

func TestDevelopmentObjectiveCapacityFailsBeforeInsert(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-objective-capacity"})
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixNano()
	for index := 0; index < MaxDevelopmentObjectives; index++ {
		id := fmt.Sprintf("capacity-objective-%04d", index)
		if _, err := tx.Exec(`INSERT INTO development_objectives(objective_id,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			id, 1, development.ObjectivePlanned, "sha256:"+strings.Repeat("0", 64), []byte("{}"), now, now); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	objective := developmentObjectiveFixture(t, "objective-capacity-next")
	if _, _, err := store.SaveDevelopmentObjective(objective); err == nil || !strings.Contains(err.Error(), "row bound exceeded") {
		t.Fatalf("objective capacity did not fail closed: %v", err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM development_objectives`).Scan(&count); err != nil || count != MaxDevelopmentObjectives {
		t.Fatalf("objective count=%d err=%v", count, err)
	}
}
