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
