package workqueue

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestDevelopmentRequestInitialBindingAndIdempotentReplay(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request"})
	initial := developmentRequestFixture()
	stored, created, err := store.SaveDevelopmentRequest(initial)
	if err != nil || !created || stored.Revision != 1 || stored.State != DevelopmentRequestPreparing {
		t.Fatalf("stored=%+v created=%t err=%v", stored, created, err)
	}
	byID, found, err := store.DevelopmentRequest(initial.ID)
	if err != nil || !found || byID != initial {
		t.Fatalf("by id=%+v found=%t err=%v", byID, found, err)
	}
	byKey, found, err := store.DevelopmentRequestByKey(initial.KeyDigest)
	if err != nil || !found || byKey != initial {
		t.Fatalf("by key=%+v found=%t err=%v", byKey, found, err)
	}

	replay := initial
	replay.ID = "dr_11111111111111111111111111111111"
	replayed, replayCreated, err := store.SaveDevelopmentRequest(replay)
	if err != nil || replayCreated || replayed.ID != initial.ID || replayed.Revision != 1 {
		t.Fatalf("replayed=%+v created=%t err=%v", replayed, replayCreated, err)
	}

	conflict := replay
	conflict.BodyDigest = "sha256:" + repeatRequestDigest("b")
	if _, _, err := store.SaveDevelopmentRequest(conflict); err == nil {
		t.Fatal("same idempotency key accepted a changed body binding")
	}

	future := developmentRequestFixture()
	future.ID = "dr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	future.KeyDigest = modelturn.IdempotencyDigest("another-development-request")
	future.Revision = 2
	future.State = DevelopmentRequestActive
	if _, _, err := store.SaveDevelopmentRequest(future); err == nil {
		t.Fatal("new request accepted a future revision or state")
	}
}

func TestProjectDevelopmentRequestsFilterBeforeLimitAndIncludeRecoveryStates(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "development-recovery-list"})
	var requests []DevelopmentRequest
	for i := 0; i < 6; i++ {
		request := developmentRequestFixture()
		request.ID = fmt.Sprintf("dr_%032x", i+1)
		request.KeyDigest = modelturn.IdempotencyDigest(fmt.Sprintf("recovery-list-%d", i))
		if i < 3 {
			request.Alias = "other"
		}
		if _, _, err := store.SaveDevelopmentRequest(request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	awaiting := requests[3]
	awaiting.Revision++
	awaiting.State = DevelopmentRequestActive
	if _, _, err := store.SaveDevelopmentRequest(awaiting); err != nil {
		t.Fatal(err)
	}
	awaiting.Revision++
	awaiting.State = DevelopmentRequestAwaitingReasoning
	awaiting.Reason = DevelopmentRequestReasonCodeFailure
	if _, _, err := store.SaveDevelopmentRequest(awaiting); err != nil {
		t.Fatal(err)
	}
	terminal := requests[4]
	terminal.Revision++
	terminal.State = DevelopmentRequestFailed
	terminal.Reason = DevelopmentRequestReasonOperationFailed
	if _, _, err := store.SaveDevelopmentRequest(terminal); err != nil {
		t.Fatal(err)
	}
	listed, complete, err := store.ProjectDevelopmentRequests("aeontra", "parrot", requests[0].DeviceID, 2)
	if err != nil || complete || len(listed) != 2 || listed[0].ID != terminal.ID || listed[1].ID != awaiting.ID {
		t.Fatalf("bounded recovery=%+v complete=%v err=%v", listed, complete, err)
	}
	listed, complete, err = store.ProjectDevelopmentRequests("aeontra", "parrot", requests[0].DeviceID, 100)
	if err != nil || !complete || len(listed) != 3 {
		t.Fatalf("complete=%v list=%+v err=%v", complete, listed, err)
	}
	for _, request := range listed {
		if request.Alias != "aeontra" {
			t.Fatal("scope crossed")
		}
	}
	listed, complete, err = store.ProjectDevelopmentRequests("aeontra", "parrot", "ed_"+strings.Repeat("f", 32), 2)
	if err != nil || !complete || len(listed) != 0 {
		t.Fatal("device generation crossed", listed, err)
	}
	for _, limit := range []int{0, -1, MaxListResults + 1} {
		if _, _, err := store.ProjectDevelopmentRequests("aeontra", "parrot", requests[0].DeviceID, limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, err := store.db.Exec(`UPDATE development_requests SET record_digest=? WHERE request_id=?`, "sha256:"+strings.Repeat("f", 64), awaiting.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ProjectDevelopmentRequests("aeontra", "parrot", requests[0].DeviceID, 100); err == nil {
		t.Fatal("tampered record accepted")
	}
}

func TestDevelopmentRequestCASSetOnceFieldsAndNonterminalOwners(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-cas"})
	initial := developmentRequestFixture()
	if _, created, err := store.SaveDevelopmentRequest(initial); err != nil || !created {
		t.Fatalf("created=%t err=%v", created, err)
	}

	active := initial
	active.Revision = 2
	active.State = DevelopmentRequestActive
	active.InspectionOperationID = "eo_22222222222222222222222222222222"
	active.ObjectiveID = "objective-1"
	active.ActiveOperationID = "eo_33333333333333333333333333333333"
	active.ProcessID = "pr_44444444444444444444444444444444"
	if _, created, err := store.SaveDevelopmentRequest(active); err != nil || created {
		t.Fatalf("active transition created=%t err=%v", created, err)
	}

	owners, err := store.DevelopmentGoalOwners()
	if err != nil || len(owners) != 1 || owners[0].OwnerDigest != initial.KeyDigest || len(owners[0].References) != 1 ||
		owners[0].References[0].BodyRef != initial.BodyRef || owners[0].References[0].ContentDigest != initial.BodyDigest {
		t.Fatalf("owners=%+v err=%v", owners, err)
	}
	listed, err := store.DevelopmentRequests(10)
	if err != nil || len(listed) != 1 || listed[0].ID != initial.ID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}

	updated := active
	updated.Revision = 3
	updated.ActiveOperationID = "eo_55555555555555555555555555555555"
	if _, _, err := store.SaveDevelopmentRequest(updated); err != nil {
		t.Fatalf("mutable active operation update failed: %v", err)
	}

	stale := active
	stale.Revision = 3
	stale.State = DevelopmentRequestFailed
	stale.Reason = DevelopmentRequestReasonOperationFailed
	if _, _, err := store.SaveDevelopmentRequest(stale); err == nil {
		t.Fatal("stale compare-and-swap overwrote a newer revision")
	}

	spoof := updated
	spoof.Revision = 4
	spoof.InspectionOperationID = "eo_66666666666666666666666666666666"
	if _, _, err := store.SaveDevelopmentRequest(spoof); err == nil {
		t.Fatal("set-once inspection operation was rewritten")
	}

	changed := updated
	changed.Revision = 4
	changed.Target = "other-target"
	if _, _, err := store.SaveDevelopmentRequest(changed); err == nil {
		t.Fatal("immutable initial target binding was rewritten")
	}

	completed := updated
	completed.Revision = 4
	completed.State = DevelopmentRequestCompleted
	completed.ActiveOperationID = ""
	if _, _, err := store.SaveDevelopmentRequest(completed); err != nil {
		t.Fatalf("terminal transition failed: %v", err)
	}
	owners, err = store.DevelopmentGoalOwners()
	if err != nil || len(owners) != 0 {
		t.Fatalf("terminal goal still owned: owners=%+v err=%v", owners, err)
	}
	listed, err = store.DevelopmentRequests(10)
	if err != nil || len(listed) != 0 {
		t.Fatalf("terminal request was listed as active: listed=%+v err=%v", listed, err)
	}
}

func TestDevelopmentRequestRejectsUnknownStateReasonAndMalformedOpaqueIDs(t *testing.T) {
	request := developmentRequestFixture()
	request.State = DevelopmentRequestState("waiting_for_magic")
	if request.Valid() {
		t.Fatal("unknown future state was accepted")
	}
	request = developmentRequestFixture()
	request.State = DevelopmentRequestAwaitingReasoning
	request.Reason = DevelopmentRequestReasonOperationFailed
	if request.Valid() {
		t.Fatal("awaiting reasoning accepted the wrong reason")
	}
	for _, reason := range []DevelopmentRequestReason{
		DevelopmentRequestReasonSemanticAcceptancePending,
		DevelopmentRequestReasonCodeFailure,
		DevelopmentRequestReasonNewRequirement,
		DevelopmentRequestReasonSourceChanged,
	} {
		request.Reason = reason
		if !request.Valid() {
			t.Errorf("awaiting reasoning rejected reason %q", reason)
		}
	}
	request = developmentRequestFixture()
	request.BodyRef = "https://example.invalid/private-goal"
	if request.Valid() {
		t.Fatal("body URL was accepted as an opaque reference")
	}
}

func TestDevelopmentRequestCanCancelDuringPreparing(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-preflight-cancel"})
	initial := developmentRequestFixture()
	if _, _, err := store.SaveDevelopmentRequest(initial); err != nil {
		t.Fatal(err)
	}
	cancelling := initial
	cancelling.Revision = 2
	cancelling.State = DevelopmentRequestCancelling
	cancelling.Reason = DevelopmentRequestReasonCancellationRequested
	if _, _, err := store.SaveDevelopmentRequest(cancelling); err != nil {
		t.Fatalf("preflight cancellation transition failed: %v", err)
	}
}

func TestDevelopmentRequestCancellationReconciliationRequiresCapturedProcess(t *testing.T) {
	for _, processID := range []string{"", "unknown-process", "pr_44444444444444444444444444444444"} {
		t.Run(processID, func(t *testing.T) {
			request := developmentRequestFixture()
			request.State = DevelopmentRequestCancelling
			request.Reason = DevelopmentRequestReasonReconciliationRequired
			request.ProcessID = processID
			want := processID == "pr_44444444444444444444444444444444"
			if request.Valid() != want {
				t.Fatalf("captured process %q valid=%t want=%t", processID, request.Valid(), want)
			}
		})
	}
}

func TestDevelopmentRequestCancellationReconciliationPersistsAndIsMonotonic(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-cancel-observation"})
	config := store.config
	initial := developmentRequestFixture()
	if _, _, err := store.SaveDevelopmentRequest(initial); err != nil {
		t.Fatal(err)
	}
	legacy := initial
	legacy.Revision++
	legacy.State, legacy.Reason = DevelopmentRequestCancelling, DevelopmentRequestReasonCancellationRequested
	legacy.ProcessID = "pr_44444444444444444444444444444444"
	legacy.ActiveOperationID = "eo_55555555555555555555555555555555"
	if _, _, err := store.SaveDevelopmentRequest(legacy); err != nil {
		t.Fatalf("legacy cancellation is invalid: %v", err)
	}
	observation := legacy
	observation.Revision++
	observation.Reason = DevelopmentRequestReasonReconciliationRequired
	observation.ActiveOperationID = ""
	if _, _, err := store.SaveDevelopmentRequest(observation); err != nil {
		t.Fatalf("captured cancellation cannot enter observation: %v", err)
	}
	stale := legacy
	stale.Revision++
	stale.ActiveOperationID = "eo_66666666666666666666666666666666"
	if _, _, err := store.SaveDevelopmentRequest(stale); err == nil {
		t.Fatal("stale cancellation CAS erased the observation phase")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(config)
	if err != nil {
		t.Fatalf("observation phase prevented reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recovered, found, err := reopened.DevelopmentRequest(initial.ID)
	if err != nil || !found || recovered != observation {
		t.Fatalf("observation changed across reopen: %+v found=%t err=%v", recovered, found, err)
	}
	if err := reopened.Integrity(); err != nil {
		t.Fatalf("observation record failed integrity: %v", err)
	}
	touched, err := reopened.TouchDevelopmentRequest(recovered.ID, recovered.Revision)
	if err != nil || touched.Reason != DevelopmentRequestReasonReconciliationRequired || touched.ProcessID != recovered.ProcessID {
		t.Fatalf("polling lost captured observation phase: %+v err=%v", touched, err)
	}
	reverse := touched
	reverse.Revision++
	reverse.Reason = DevelopmentRequestReasonCancellationRequested
	if !reverse.Valid() {
		t.Fatal("legacy cancellation reason ceased to be a valid record")
	}
	if _, _, err := reopened.SaveDevelopmentRequest(reverse); err == nil {
		t.Fatal("cancellation observation regressed to another stop phase")
	}
	advanced := touched
	advanced.Revision++
	advanced.ActiveOperationID = "eo_77777777777777777777777777777777"
	if _, _, err := reopened.SaveDevelopmentRequest(advanced); err != nil {
		t.Fatalf("next read could not preserve observation phase: %v", err)
	}
	terminal := advanced
	terminal.Revision++
	terminal.State, terminal.Reason = DevelopmentRequestCancelled, DevelopmentRequestReasonCancellationRequested
	if _, _, err := reopened.SaveDevelopmentRequest(terminal); err != nil {
		t.Fatalf("observed terminal cancellation is invalid: %v", err)
	}
}

func TestDevelopmentRequestLegacyCancellationWithoutProcessSurvivesReopen(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-legacy-cancel"})
	config := store.config
	initial := developmentRequestFixture()
	if _, _, err := store.SaveDevelopmentRequest(initial); err != nil {
		t.Fatal(err)
	}
	legacy := initial
	legacy.Revision++
	legacy.State, legacy.Reason = DevelopmentRequestCancelling, DevelopmentRequestReasonCancellationRequested
	if _, _, err := store.SaveDevelopmentRequest(legacy); err != nil {
		t.Fatal(err)
	}
	unbound := legacy
	unbound.Revision++
	unbound.Reason = DevelopmentRequestReasonReconciliationRequired
	if _, _, err := store.SaveDevelopmentRequest(unbound); err == nil {
		t.Fatal("uncaptured process entered cancellation observation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recovered, found, err := reopened.DevelopmentRequest(initial.ID)
	if err != nil || !found || recovered != legacy {
		t.Fatalf("legacy cancellation was migrated or lost: %+v found=%t err=%v", recovered, found, err)
	}
}

func TestDevelopmentRequestExpiredTerminalRowsDoNotBlockNewRequests(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-retention"})
	active := developmentRequestFixture()
	if _, _, err := store.SaveDevelopmentRequest(active); err != nil {
		t.Fatal(err)
	}
	active.Revision = 2
	active.State = DevelopmentRequestActive
	if _, _, err := store.SaveDevelopmentRequest(active); err != nil {
		t.Fatal(err)
	}

	// Fill the retained terminal idempotency window with valid content-free records.
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= MaxDevelopmentRequests; index++ {
		request := developmentRequestFixture()
		request.ID = fmt.Sprintf("dr_%032x", index)
		request.KeyDigest = modelturn.IdempotencyDigest(fmt.Sprintf("old-terminal-%d", index))
		request.BodyRef = fmt.Sprintf("mb_%032x", index)
		request.Revision = 2
		request.State = DevelopmentRequestFailed
		request.Reason = DevelopmentRequestReasonOperationFailed
		body, digest, err := marshalDevelopmentRequest(request)
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO development_requests(request_id,key_digest,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			request.ID, request.KeyDigest, request.Revision, request.State, digest, body, 1, 1); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.developmentRequestIntegrity(); err != nil {
		t.Fatalf("valid terminal history exceeded active-record bound: %v", err)
	}
	if _, found, err := store.DevelopmentRequestByKey(modelturn.IdempotencyDigest("old-terminal-1")); err != nil || found {
		t.Fatalf("expired terminal key lookup returned a request: found=%t err=%v", found, err)
	}

	newRequest := developmentRequestFixture()
	newRequest.ID = "dr_ffffffffffffffffffffffffffffffff"
	newRequest.KeyDigest = modelturn.IdempotencyDigest("new-after-terminal-retention")
	newRequest.BodyRef = "mb_ffffffffffffffffffffffffffffffff"
	if _, created, err := store.SaveDevelopmentRequest(newRequest); err != nil || !created {
		t.Fatalf("new request after expired terminal rows created=%t err=%v", created, err)
	}
	if _, found, err := store.DevelopmentRequestByKey(modelturn.IdempotencyDigest("old-terminal-1")); err != nil || found {
		t.Fatalf("expired terminal idempotency binding remained: found=%t err=%v", found, err)
	}
	if _, found, err := store.DevelopmentRequest(active.ID); err != nil || !found {
		t.Fatalf("active request was pruned: found=%t err=%v", found, err)
	}
	owners, err := store.DevelopmentGoalOwners()
	if err != nil || len(owners) != 2 {
		t.Fatalf("active goal owners=%+v err=%v", owners, err)
	}
}

func TestDevelopmentRequestDispatchListRotatesByCASAndOmitsAwaitingReasoning(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-fairness"})
	fixedNow := time.Unix(1_800_000_000, 0).UTC()
	store.now = func() time.Time { return fixedNow }

	first := developmentRequestFixture()
	second := developmentRequestFixture()
	second.ID = "dr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	second.KeyDigest = modelturn.IdempotencyDigest("fairness-second")
	second.BodyRef = "mb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	awaiting := developmentRequestFixture()
	awaiting.ID = "dr_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	awaiting.KeyDigest = modelturn.IdempotencyDigest("fairness-awaiting")
	awaiting.BodyRef = "mb_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	for _, request := range []DevelopmentRequest{first, second, awaiting} {
		if _, _, err := store.SaveDevelopmentRequest(request); err != nil {
			t.Fatal(err)
		}
	}
	awaiting.Revision = 2
	awaiting.State = DevelopmentRequestActive
	if _, _, err := store.SaveDevelopmentRequest(awaiting); err != nil {
		t.Fatal(err)
	}
	awaiting.Revision = 3
	awaiting.State = DevelopmentRequestAwaitingReasoning
	awaiting.Reason = DevelopmentRequestReasonCodeFailure
	if _, _, err := store.SaveDevelopmentRequest(awaiting); err != nil {
		t.Fatal(err)
	}

	listed, err := store.DevelopmentRequests(10)
	if err != nil || len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("initial dispatch order=%+v err=%v", listed, err)
	}

	touched, err := store.TouchDevelopmentRequest(first.ID, first.Revision)
	if err != nil || touched.Revision != first.Revision+1 {
		t.Fatalf("touch=%+v err=%v", touched, err)
	}
	if _, err := store.TouchDevelopmentRequest(first.ID, first.Revision); err == nil {
		t.Fatal("stale touch revision was accepted")
	}
	listed, err = store.DevelopmentRequests(10)
	if err != nil || len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID {
		t.Fatalf("dispatch order did not rotate after touch: %+v err=%v", listed, err)
	}
	owners, err := store.DevelopmentGoalOwners()
	if err != nil || len(owners) != 3 {
		t.Fatalf("awaiting reasoning lost its body pin owner: owners=%+v err=%v", owners, err)
	}
	if _, err := store.TouchDevelopmentRequest(awaiting.ID, awaiting.Revision); err == nil {
		t.Fatal("awaiting reasoning request was eligible for dispatch polling")
	}
}

func TestDevelopmentRequestIntegrityFailsClosedOnTamperedReceiptMetadata(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-development-request-integrity"})
	request := developmentRequestFixture()
	if _, _, err := store.SaveDevelopmentRequest(request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE development_requests SET record_json=? WHERE request_id=?`, []byte(`{"state":"completed"}`), request.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.developmentRequestIntegrity(); err == nil {
		t.Fatal("integrity check accepted tampered request metadata")
	}
	if _, found, err := store.DevelopmentRequest(request.ID); err == nil || found {
		t.Fatalf("read accepted tampered request: found=%t err=%v", found, err)
	}
}

func developmentRequestFixture() DevelopmentRequest {
	return DevelopmentRequest{
		ID:         "dr_0123456789abcdef0123456789abcdef",
		Revision:   1,
		KeyDigest:  modelturn.IdempotencyDigest("development-request-1"),
		Alias:      "aeontra",
		Target:     "parrot",
		DeviceID:   "ed_0123456789abcdef0123456789abcdef",
		BodyRef:    "mb_abcdefabcdefabcdefabcdefabcdefab",
		BodyDigest: "sha256:" + repeatRequestDigest("a"),
		State:      DevelopmentRequestPreparing,
	}
}

func repeatRequestDigest(character string) string {
	return strings.Repeat(character, 64)
}
