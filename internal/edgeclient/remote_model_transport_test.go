package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestRemoteEdgeTransportRecoversLostCreateAndWaitResponses(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{"/turns": 1, "/wait": 1})
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{
		StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	started, err := remote.Started(t.Context())
	if err != nil || started.State != modelturn.RuntimeStateAwaitingModel {
		t.Fatalf("started=%+v err=%v", started, err)
	}

	payload := json.RawMessage(`{"messages":[{"content":"read calc.go","role":"user"}],"tools":[{"name":"read"}]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := modelturn.ModelRequest{
		RuntimeID: fixture.lease.RuntimeID, Sequence: 1, Payload: payload, CanonicalPayload: true,
		RequestDigest: digest, TTL: 2 * time.Minute,
		OfferedTools: []modelturn.ToolDefinition{{ID: "tool-read", Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	turn, err := remote.CreateTurn(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if turn.RuntimeID != request.RuntimeID || turn.Sequence != 1 || turn.RequestDigest != digest || !remoteBodyRefPattern.MatchString(turn.RequestRef) {
		t.Fatalf("turn=%+v", turn)
	}
	if fixture.dropper.callsEnding("/turns") != 2 {
		t.Fatalf("create calls=%d, want 2", fixture.dropper.callsEnding("/turns"))
	}
	stats, err := fixture.turns.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TurnCount != 1 {
		t.Fatalf("authoritative turn count=%d", stats.TurnCount)
	}

	responsePayload := json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[{"arguments":{"filePath":"calc.go"},"id":"call-read","name":"read"}]}`)
	if _, err := fixture.turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: request.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: digest,
		Payload: responsePayload, UsedToolIDs: []string{"tool-read"},
	}); err != nil {
		t.Fatal(err)
	}
	response, err := remote.WaitResponse(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response.RuntimeID != request.RuntimeID || response.TurnID != turn.ID || response.Sequence != 1 || response.RequestDigest != digest || !bytes.Equal(response.Payload, responsePayload) {
		t.Fatalf("response=%+v", response)
	}
	if fixture.dropper.callsEnding("/wait") != 2 {
		t.Fatalf("wait calls=%d, want 2", fixture.dropper.callsEnding("/wait"))
	}
	record, err := fixture.turns.Get(t.Context(), turn.ID)
	if err != nil || record.Status != modelturn.StatusConsumed {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	entry, err := remote.journal.turnByID(t.Context(), turn.ID)
	if err != nil || entry.State != "consumed" || !remoteCreateIDPattern.MatchString(entry.CreateID) || !remoteWaitIDPattern.MatchString(entry.WaitID) {
		t.Fatalf("journal=%+v err=%v", entry, err)
	}
	metadata := entry.RuntimeID + entry.RequestDigest + entry.CreateID + string(entry.TurnID) + entry.RequestRef + entry.WaitID + entry.State
	if strings.Contains(metadata, "read calc.go") || strings.Contains(metadata, "filePath") {
		t.Fatal("model relay journal leaked request or response content")
	}
}

func TestLeaseModelRuntimeRetriesWithSameLeaseID(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{"/lease": 1})
	if fixture.lease.RetryCounts[modelturn.RuntimeRetryTransportError] != 1 || len(fixture.lease.RetryCounts) != 1 {
		t.Fatalf("retry counts=%v", fixture.lease.RetryCounts)
	}
	if fixture.dropper.callsEnding("/lease") != 2 {
		t.Fatalf("lease calls=%d, want 2", fixture.dropper.callsEnding("/lease"))
	}
	bodies := fixture.dropper.bodiesEnding("/lease")
	if len(bodies) != 2 {
		t.Fatalf("lease request bodies=%d, want 2", len(bodies))
	}
	var first, second struct {
		LeaseID string `json:"lease_id"`
	}
	if err := json.Unmarshal(bodies[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bodies[1], &second); err != nil {
		t.Fatal(err)
	}
	if !remoteLeaseIDPattern.MatchString(first.LeaseID) || first.LeaseID != second.LeaseID {
		t.Fatalf("lease ids first=%q second=%q", first.LeaseID, second.LeaseID)
	}
}

func TestLeaseModelRuntimeLongPollOutlivesGenericClientTimeout(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	client := &http.Client{
		Transport: delayedNoContentRoundTripper{delay: 75 * time.Millisecond},
		Timeout:   10 * time.Millisecond,
	}
	transport, err := NewTransport(fixture.stateRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	lease, err := transport.LeaseModelRuntime(ctx, time.Second)
	if err != nil || lease != nil {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if client.Timeout != 10*time.Millisecond {
		t.Fatalf("generic client timeout mutated to %s", client.Timeout)
	}
}

func TestLeaseModelRuntimeContextDeadlineOverridesLongPollClient(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	transport, err := NewTransport(fixture.stateRoot, &http.Client{
		Transport: delayedNoContentRoundTripper{delay: time.Second},
		Timeout:   10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	lease, err := transport.LeaseModelRuntime(ctx, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) || lease != nil {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("context deadline was not authoritative: %s", elapsed)
	}
}

func TestModelRuntimeLeaseClientUsesBoundedMargin(t *testing.T) {
	base := &http.Client{Timeout: 10 * time.Millisecond}
	transport := &Transport{client: base}
	cases := []struct {
		wait time.Duration
		want time.Duration
	}{
		{wait: time.Second, want: 31 * time.Second},
		{wait: remoteModelDefaultWait, want: 150 * time.Second},
		{wait: remoteModelMaxWait, want: remoteModelClientTimeout},
	}
	for _, test := range cases {
		client := transport.modelRuntimeLeaseClient(test.wait)
		if client == base || client.Timeout != test.want {
			t.Fatalf("wait=%s client=%p timeout=%s want=%s", test.wait, client, client.Timeout, test.want)
		}
	}
	if base.Timeout != 10*time.Millisecond {
		t.Fatalf("base client timeout mutated to %s", base.Timeout)
	}
}

func TestRemoteEdgeTransportLifecycleIsIdempotentAfterLostResponses(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{"/started": 1, "/completed": 1})
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{
		StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	started, err := remote.Started(t.Context())
	if err != nil || started.State != modelturn.RuntimeStateAwaitingModel {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	if fixture.dropper.callsEnding("/started") != 2 {
		t.Fatalf("started calls=%d, want 2", fixture.dropper.callsEnding("/started"))
	}
	resultRef := "rs_11111111111111111111111111111111"
	completed, err := remote.Completed(t.Context(), resultRef)
	if err != nil || completed.State != modelturn.RuntimeStateCompleted || completed.ResultRef != resultRef {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if fixture.dropper.callsEnding("/completed") != 2 {
		t.Fatalf("completed calls=%d, want 2", fixture.dropper.callsEnding("/completed"))
	}
	repeated, err := remote.Completed(t.Context(), resultRef)
	if err != nil || repeated.State != modelturn.RuntimeStateCompleted || repeated.ResultRef != resultRef {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	if _, err := remote.Completed(t.Context(), "rs_22222222222222222222222222222222"); !errors.Is(err, modelturn.ErrTurnConflict) {
		t.Fatalf("changed terminal result err=%v", err)
	}
}

func TestRemoteEdgeTransportFailureIsIdempotentAfterLostResponse(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{"/failed": 1})
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	resultRef := "rs_33333333333333333333333333333333"
	failed, err := remote.Failed(t.Context(), resultRef)
	if err != nil || failed.State != modelturn.RuntimeStateFailed || failed.ResultRef != resultRef {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	if fixture.dropper.callsEnding("/failed") != 2 {
		t.Fatalf("failed calls=%d, want 2", fixture.dropper.callsEnding("/failed"))
	}
	repeated, err := remote.Failed(t.Context(), resultRef)
	if err != nil || repeated.ResultRef != resultRef {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	if _, err := remote.Failed(t.Context(), "rs_44444444444444444444444444444444"); !errors.Is(err, modelturn.ErrTurnConflict) {
		t.Fatalf("changed terminal result err=%v", err)
	}
}

func TestRemoteEdgeTransportCancelIsIdempotentAfterLostResponse(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{"/cancel": 1})
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"messages":[{"content":"cancel","role":"user"}],"tools":[]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := remote.CreateTurn(t.Context(), modelturn.ModelRequest{
		RuntimeID: fixture.lease.RuntimeID, Sequence: 1, Payload: payload, CanonicalPayload: true,
		RequestDigest: digest, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Cancel(t.Context(), turn.ID); err != nil {
		t.Fatal(err)
	}
	if fixture.dropper.callsEnding("/cancel") != 2 {
		t.Fatalf("cancel calls=%d, want 2", fixture.dropper.callsEnding("/cancel"))
	}
	if err := remote.Cancel(t.Context(), turn.ID); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.turns.Get(t.Context(), turn.ID)
	if err != nil || record.Status != modelturn.StatusCancelled {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	entry, err := remote.journal.turnByID(t.Context(), turn.ID)
	if err != nil || entry.State != "cancelled" {
		t.Fatalf("journal=%+v err=%v", entry, err)
	}
}

func TestRemoteEdgeTransportRejectsAlteredLeaseBindings(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	cases := map[string]func(*ModelRuntimeLease){
		"runtime":    func(lease *ModelRuntimeLease) { lease.RuntimeID = "mr_99999999999999999999999999999999" },
		"device":     func(lease *ModelRuntimeLease) { lease.DeviceID = "ed_99999999999999999999999999999999" },
		"workspace":  func(lease *ModelRuntimeLease) { lease.WorkspaceID = "ws_invalid" },
		"controller": func(lease *ModelRuntimeLease) { lease.Controller = modelturn.ControllerMCPSampling },
		"state":      func(lease *ModelRuntimeLease) { lease.State = modelturn.RuntimeStateAwaitingModel },
		"provider":   func(lease *ModelRuntimeLease) { lease.ProviderProfile = "arbitrary-provider" },
		"goal":       func(lease *ModelRuntimeLease) { lease.Goal = lease.Goal + " changed" },
		"digest": func(lease *ModelRuntimeLease) {
			lease.GoalDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		},
		"timeout": func(lease *ModelRuntimeLease) { lease.TimeoutSeconds = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			lease := fixture.lease
			mutate(&lease)
			remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{
				StateRoot: fixture.stateRoot, Lease: lease, HTTPClient: fixture.client, LongPoll: time.Second,
			})
			if err == nil {
				_ = remote.Close()
				t.Fatal("altered lease accepted")
			}
		})
	}
}

func TestRemoteEdgeTransportRecoversTemporaryVPSOutageWithoutDuplicateActions(t *testing.T) {
	fixture := newRemoteModelFixture(t, map[string]int{
		"/started":   0,
		"/turns":     0,
		"/wait":      0,
		"/completed": 0,
	})
	outage := &temporaryOutageRoundTripper{
		base: fixture.client.Transport,
		failures: map[string]int{
			"/started":   1,
			"/turns":     1,
			"/wait":      1,
			"/completed": 1,
		},
	}
	fixture.client.Transport = outage
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{
		StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"messages":[{"content":"temporary outage","role":"user"}],"tools":[]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := remote.CreateTurn(t.Context(), modelturn.ModelRequest{
		RuntimeID: fixture.lease.RuntimeID, Sequence: 1, Payload: payload, CanonicalPayload: true,
		RequestDigest: digest, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: fixture.lease.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1,
		RequestDigest: digest, Payload: json.RawMessage(`{"finish_reason":"stop","text":"recovered"}`),
	}); err != nil {
		t.Fatal(err)
	}
	response, err := remote.WaitResponse(t.Context(), turn.ID)
	if err != nil || response.TurnID != turn.ID || response.RequestDigest != digest {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if _, err := remote.Completed(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	stats, err := fixture.turns.Stats(t.Context())
	if err != nil || stats.RuntimeCount != 1 || stats.TurnCount != 1 || stats.ConsumedCount != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	for _, suffix := range []string{"/started", "/turns", "/wait", "/completed"} {
		if outage.failuresRemaining(suffix) != 0 {
			t.Fatalf("outage for %s was not exercised", suffix)
		}
		if fixture.dropper.callsEnding(suffix) != 1 {
			t.Fatalf("authoritative calls for %s=%d want=1", suffix, fixture.dropper.callsEnding(suffix))
		}
	}
}

func TestRemoteEdgeTransportStagesLargeBodyWithoutLocalAuthority(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	secret := "temporary-private-prompt"
	payloadBytes, err := json.Marshal(map[string]any{
		"messages": []any{map[string]any{"content": secret + strings.Repeat("x", int(modelturn.MaxInlineRequestBytes)+4096), "role": "user"}},
		"tools":    []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(payloadBytes)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	localRef, err := remote.StageRequestBody(t.Context(), payload, true, 2*time.Minute)
	if err != nil || !localRequestRefPattern.MatchString(localRef.RequestRef) || localRef.RequestDigest != digest {
		t.Fatalf("localRef=%+v err=%v", localRef, err)
	}
	turn, err := remote.CreateTurnFromReference(t.Context(), modelturn.ModelRequest{
		RuntimeID: fixture.lease.RuntimeID, Sequence: 1, RequestRef: localRef.RequestRef,
		RequestDigest: digest, TTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !remoteBodyRefPattern.MatchString(turn.RequestRef) || turn.RequestRef == localRef.RequestRef {
		t.Fatalf("turn=%+v localRef=%+v", turn, localRef)
	}
	var stagedCount int
	if err := remote.journal.db.QueryRow(`SELECT COUNT(*) FROM staged_model_bodies WHERE local_ref=?`, localRef.RequestRef).Scan(&stagedCount); err != nil {
		t.Fatal(err)
	}
	if stagedCount != 0 {
		t.Fatalf("local staged body remains after create: %d", stagedCount)
	}
	var forbiddenTables int
	if err := remote.journal.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('model_turns','turn_bodies','model_runtimes')`).Scan(&forbiddenTables); err != nil {
		t.Fatal(err)
	}
	if forbiddenTables != 0 {
		t.Fatalf("local journal created authoritative tables: %d", forbiddenTables)
	}
	offer, pending, err := fixture.turns.Poll(t.Context(), fixture.lease.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !bytes.Equal(offer.RequestPayload, payload) || offer.RequestRef != turn.RequestRef || offer.RequestDigest != digest {
		t.Fatalf("authoritative offer=%+v", offer.Record)
	}
}

func TestRemoteEdgeTransportPreservesCanonicalHTMLCharacters(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"messages":[{"content":"compare <tag> & value > zero","role":"user"}],"tools":[]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := remote.CreateTurn(t.Context(), modelturn.ModelRequest{
		RuntimeID: fixture.lease.RuntimeID, Sequence: 1, Payload: payload, CanonicalPayload: true,
		RequestDigest: digest, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	offer, pending, err := fixture.turns.Poll(t.Context(), fixture.lease.RuntimeID)
	if err != nil || !pending {
		t.Fatalf("pending=%t err=%v", pending, err)
	}
	if turn.RequestDigest != digest || offer.RequestDigest != digest || !bytes.Equal(offer.RequestPayload, payload) {
		t.Fatalf("turn_digest=%s offer_digest=%s payload_equal=%t", turn.RequestDigest, offer.RequestDigest, bytes.Equal(offer.RequestPayload, payload))
	}
}

func TestRemoteEdgeTransportJournalSurvivesLocalRestart(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"messages":[{"content":"restart","role":"user"}],"tools":[]}`)
	digest, _ := modelturn.ExactPayloadDigest(payload)
	request := modelturn.ModelRequest{RuntimeID: fixture.lease.RuntimeID, Sequence: 1, Payload: payload, CanonicalPayload: true, RequestDigest: digest, TTL: time.Minute}
	turn, err := remote.CreateTurn(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := remote.journal.turnByID(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, err := reopened.CreateTurn(t.Context(), request)
	if err != nil || replayed.ID != turn.ID || replayed.RequestRef != turn.RequestRef {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	after, err := reopened.journal.ensureWaitID(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.CreateID != after.CreateID || after.WaitID == "" || after.RuntimeID != before.RuntimeID || after.Sequence != before.Sequence || after.RequestDigest != before.RequestDigest || after.TurnID != before.TurnID || after.RequestRef != before.RequestRef {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestDriverUsesRemoteEdgeTransportAndPrivateLocalReference(t *testing.T) {
	fixture := newRemoteModelFixture(t, nil)
	remote, err := NewRemoteEdgeTransport(RemoteEdgeTransportOptions{StateRoot: fixture.stateRoot, Lease: fixture.lease, HTTPClient: fixture.client, LongPoll: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	driver, err := modelturn.NewDriverTransport(remote)
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"messages": []any{map[string]any{"content": strings.Repeat("z", int(modelturn.MaxInlineRequestBytes)+4096), "role": "user"}},
		"tools":    []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(payloadBytes)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	stageRequest := httptest.NewRequest(http.MethodPost, "/v1/request-bodies", bytes.NewReader(payload))
	stageRequest.Header.Set("Content-Type", "application/json")
	stageRequest.Header.Set("X-MCP-Request-Digest", digest)
	stageRequest.Header.Set("X-MCP-TTL-Ms", "120000")
	stageResponse := httptest.NewRecorder()
	driver.Handler().ServeHTTP(stageResponse, stageRequest)
	if stageResponse.Code != http.StatusCreated {
		t.Fatalf("stage status=%d body=%s", stageResponse.Code, stageResponse.Body.String())
	}
	var localReference modelturn.RequestBodyReference
	if err := json.Unmarshal(stageResponse.Body.Bytes(), &localReference); err != nil || !localRequestRefPattern.MatchString(localReference.RequestRef) {
		t.Fatalf("localReference=%+v err=%v", localReference, err)
	}
	createBody, _ := json.Marshal(map[string]any{
		"runtime_id": fixture.lease.RuntimeID, "sequence": 1, "request_digest": digest,
		"request_ref": localReference.RequestRef, "ttl_ms": 120000,
	})
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/turns", bytes.NewReader(createBody))
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	driver.Handler().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var turn modelturn.Turn
	if err := json.Unmarshal(createResponse.Body.Bytes(), &turn); err != nil || !remoteBodyRefPattern.MatchString(turn.RequestRef) {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	controllerPayload := json.RawMessage(`{"finish_reason":"stop","text":"driver remote verified"}`)
	if _, err := fixture.turns.Respond(t.Context(), modelturn.ResponseSubmission{RuntimeID: fixture.lease.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: digest, Payload: controllerPayload}); err != nil {
		t.Fatal(err)
	}
	waitRequest := httptest.NewRequest(http.MethodGet, "/v1/turns/"+string(turn.ID)+"/response", nil)
	waitRequest.SetPathValue("turnID", string(turn.ID))
	waitResponse := httptest.NewRecorder()
	driver.Handler().ServeHTTP(waitResponse, waitRequest)
	if waitResponse.Code != http.StatusOK {
		t.Fatalf("wait status=%d body=%s", waitResponse.Code, waitResponse.Body.String())
	}
	var response modelturn.ModelResponse
	if err := json.Unmarshal(waitResponse.Body.Bytes(), &response); err != nil || !bytes.Equal(response.Payload, controllerPayload) {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

type remoteModelFixture struct {
	devices   *edge.Store
	turns     *modelturn.Store
	stateRoot string
	lease     ModelRuntimeLease
	client    *http.Client
	dropper   *dropOnceRoundTripper
}

func newRemoteModelFixture(t *testing.T, drops map[string]int) remoteModelFixture {
	t.Helper()
	devices, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devices.Close() })
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "server-turns")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	server := httptest.NewTLSServer(edge.NewHTTPHandler(devices, turns))
	t.Cleanup(server.Close)
	dropper := &dropOnceRoundTripper{base: server.Client().Transport, drops: make(map[string]int), calls: make(map[string]int), requestBodies: make(map[string][][]byte)}
	for suffix, count := range drops {
		dropper.drops[suffix] = count
	}
	client := &http.Client{Transport: dropper, Timeout: remoteModelClientTimeout}
	stateRoot := filepath.Join(t.TempDir(), "client")
	code, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := Pair(t.Context(), PairOptions{ServerURL: server.URL, Code: code, Name: "parrot-development", StateRoot: stateRoot, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	goal := []byte("run remote OpenCode")
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: identity.DeviceID, WorkspaceID: "ws_0123456789abcdef0123456789abcdef",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("remote-model-fixture"), TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := NewTransport(stateRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := signed.LeaseModelRuntime(t.Context(), time.Second)
	if err != nil || lease == nil || lease.RuntimeID != runtime.RuntimeID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	return remoteModelFixture{devices: devices, turns: turns, stateRoot: stateRoot, lease: *lease, client: client, dropper: dropper}
}

type delayedNoContentRoundTripper struct {
	delay time.Duration
}

func (d delayedNoContentRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	timer := time.NewTimer(d.delay)
	defer timer.Stop()
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-timer.C:
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    request,
		}, nil
	}
}

type temporaryOutageRoundTripper struct {
	base     http.RoundTripper
	mu       sync.Mutex
	failures map[string]int
}

func (t *temporaryOutageRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	for suffix, remaining := range t.failures {
		if strings.HasSuffix(request.URL.Path, suffix) && remaining > 0 {
			t.failures[suffix] = remaining - 1
			t.mu.Unlock()
			return nil, errors.New("temporary VPS unavailable")
		}
	}
	t.mu.Unlock()
	return t.base.RoundTrip(request)
}

func (t *temporaryOutageRoundTripper) failuresRemaining(suffix string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failures[suffix]
}

type dropOnceRoundTripper struct {
	base          http.RoundTripper
	mu            sync.Mutex
	drops         map[string]int
	calls         map[string]int
	requestBodies map[string][][]byte
}

func (t *dropOnceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var requestBody []byte
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err == nil {
			requestBody, _ = io.ReadAll(io.LimitReader(body, 1<<20))
			_ = body.Close()
		}
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	for suffix, remaining := range t.drops {
		if strings.HasSuffix(request.URL.Path, suffix) {
			t.calls[suffix]++
			t.requestBodies[suffix] = append(t.requestBodies[suffix], append([]byte(nil), requestBody...))
			if remaining > 0 {
				t.drops[suffix] = remaining - 1
				t.mu.Unlock()
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				return nil, errors.New("simulated lost HTTP response")
			}
			t.mu.Unlock()
			return response, nil
		}
	}
	t.mu.Unlock()
	return response, nil
}

func (t *dropOnceRoundTripper) callsEnding(suffix string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls[suffix]
}

func (t *dropOnceRoundTripper) bodiesEnding(suffix string) [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	bodies := make([][]byte, len(t.requestBodies[suffix]))
	for index := range t.requestBodies[suffix] {
		bodies[index] = append([]byte(nil), t.requestBodies[suffix][index]...)
	}
	return bodies
}
