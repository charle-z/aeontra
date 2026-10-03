package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func runnerJournalFixture(t *testing.T) (*Store, Lease, DevelopmentRunnerEffect) {
	t.Helper()
	store := openTestStore(t, Config{})
	_, _, err := store.Enqueue(testSpec("runner-journal-001", "runner"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "runner-holder-001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := DevelopmentRunnerEffect{EffectID: strings.Repeat("a", 64), Revision: 1, BindingDigest: "sha256:" + strings.Repeat("b", 64), TemplateDigest: "sha256:" + strings.Repeat("c", 64), WorkflowID: 31, CommandProfile: "probe-only", JobID: lease.Job.ID, Fence: lease.Fence, State: "dispatch_intent", StartedAt: time.Now().UTC()}
	return store, lease, r
}

func TestDevelopmentRunnerJournalRetentionPreservesCurrentCalibrationAndActiveEffects(t *testing.T) {
	store, lease, record := runnerJournalFixture(t)
	keep := strings.Repeat("a", 64)
	expired := strings.Repeat("b", 64)
	active := strings.Repeat("c", 64)
	for _, id := range []string{keep, expired, active} {
		record.EffectID = id
		record.Revision = 1
		record.State = "dispatch_intent"
		record.RunID = 0
		record.ReceiptDigest = ""
		record.CompletedAt = time.Time{}
		record.StartedAt = time.Now().UTC().Add(-32 * 24 * time.Hour)
		if _, err := store.SaveDevelopmentRunnerEffect(record, lease); err != nil {
			t.Fatal(err)
		}
		if id != active {
			record.Revision++
			record.State = "succeeded"
			record.RunID = 91
			record.ReceiptDigest = "sha256:" + strings.Repeat("d", 64)
			record.CompletedAt = record.StartedAt.Add(time.Hour)
			if _, err := store.SaveDevelopmentRunnerEffect(record, lease); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := store.PruneDevelopmentRunnerEffects([]string{keep}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.DevelopmentRunnerEffect(expired); !found {
		t.Fatal("leased job lost evidence")
	}
	if _, err := store.Complete(lease.Job.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "settled"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneDevelopmentRunnerEffects([]string{keep}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.DevelopmentRunnerEffect(expired); found {
		t.Fatal("expired terminal effect retained forever")
	}
	for _, id := range []string{keep, active} {
		if _, found, err := store.DevelopmentRunnerEffect(id); err != nil || !found {
			t.Fatal("current calibration or active intent lost", err)
		}
	}
	if err := store.Integrity(); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentRunnerJournalRejectsUnknownFields(t *testing.T) {
	store, lease, record := runnerJournalFixture(t)
	if _, err := store.SaveDevelopmentRunnerEffect(record, lease); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(record)
	body = append(body[:len(body)-1], []byte(`,"unexpected":"secret"}`)...)
	if _, err := store.db.Exec(`UPDATE development_runner_effects SET record_json=? WHERE effect_id=?`, body, record.EffectID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DevelopmentRunnerEffect(record.EffectID); err == nil {
		t.Fatal("unknown runner fields accepted")
	}
}

func TestDevelopmentRunnerJournalIntentCASAndImmutableIdentity(t *testing.T) {
	store, lease, r := runnerJournalFixture(t)
	if created, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err == nil {
		t.Fatal("same intent acquired second dispatcher")
	}
	r.Revision++
	r.RunID = 91
	r.State = "pending"
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*DevelopmentRunnerEffect){func(r *DevelopmentRunnerEffect) { r.BindingDigest = "sha256:" + strings.Repeat("d", 64) }, func(r *DevelopmentRunnerEffect) { r.RunID++ }, func(r *DevelopmentRunnerEffect) { r.CommandProfile = "go-test-all" }, func(r *DevelopmentRunnerEffect) { r.TemplateDigest = "sha256:" + strings.Repeat("e", 64) }, func(r *DevelopmentRunnerEffect) { r.WorkflowID++ }, func(r *DevelopmentRunnerEffect) { r.Fence++ }} {
		forged := r
		forged.Revision++
		mutate(&forged)
		if _, err := store.SaveDevelopmentRunnerEffect(forged, lease); err == nil {
			t.Fatal("journal identity/fence rewritten")
		}
	}
	if err := store.Integrity(); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentRunnerJournalTerminalAndCancellationFailClosed(t *testing.T) {
	store, lease, r := runnerJournalFixture(t)
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil {
		t.Fatal(err)
	}
	r.Revision++
	r.State = "cancel_intent"
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil {
		t.Fatal(err)
	}
	forged := r
	forged.Revision++
	forged.State = "succeeded"
	forged.RunID = 91
	forged.ReceiptDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := store.SaveDevelopmentRunnerEffect(forged, lease); err == nil {
		t.Fatal("cancellation became success")
	}
	r.Revision++
	r.State = "cancelled"
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil {
		t.Fatal(err)
	}
	r.Revision++
	r.State = "failed"
	r.Failure = development.FailureExternalTransient
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err == nil {
		t.Fatal("terminal outcome rewritten")
	}
}

func TestDevelopmentRunnerJournalCorruptionAndCapacityFailClosed(t *testing.T) {
	store, lease, r := runnerJournalFixture(t)
	if _, err := store.SaveDevelopmentRunnerEffect(r, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE development_runner_effects SET record_json='{}' WHERE effect_id=?`, r.EffectID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DevelopmentRunnerEffect(r.EffectID); err == nil {
		t.Fatal("corrupt journal readable")
	}
	if store.Integrity() == nil {
		t.Fatal("corrupt journal passed startup integrity")
	}
}
