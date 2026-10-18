package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	remoteModelRuntimeLeasePath = "/edge/v1/model-runtimes/lease"
	remoteModelRuntimePrefix    = "/edge/v1/model-runtimes/"
	remoteProviderProfile       = modelturn.OpenCodeProviderProfile
	remoteModelResponseLimit    = int64(modelturn.MaxRequestBodyBytes + (512 << 10))
	remoteModelDefaultWait      = 120 * time.Second
	remoteModelMaxWait          = 180 * time.Second
	remoteModelLongPollMargin   = 30 * time.Second
	remoteModelClientTimeout    = remoteModelMaxWait + remoteModelLongPollMargin
)

type ModelRuntimeLease struct {
	RuntimeID       string                                    `json:"runtime_id"`
	DeviceID        string                                    `json:"device_id"`
	WorkspaceID     string                                    `json:"workspace_id"`
	Controller      modelturn.RuntimeController               `json:"controller"`
	State           modelturn.RuntimeState                    `json:"state"`
	Goal            string                                    `json:"goal"`
	GoalDigest      string                                    `json:"goal_digest"`
	TimeoutSeconds  int                                       `json:"timeout_seconds"`
	ProviderProfile string                                    `json:"provider_profile"`
	RetryCounts     map[modelturn.RuntimeRetryCategory]uint32 `json:"-"`
}

type RemoteEdgeTransportOptions struct {
	StateRoot  string
	Lease      ModelRuntimeLease
	HTTPClient *http.Client
	LongPoll   time.Duration
}

type RemoteEdgeTransport struct {
	signed   *Transport
	journal  *modelJournal
	lease    ModelRuntimeLease
	longPoll time.Duration
	mu       sync.RWMutex
	runtime  modelturn.Runtime
}

type remoteTurnCreateRequest struct {
	CreateID      string                     `json:"create_id"`
	Sequence      uint64                     `json:"sequence"`
	RequestDigest string                     `json:"request_digest"`
	Payload       json.RawMessage            `json:"payload"`
	OfferedTools  []modelturn.ToolDefinition `json:"offered_tools,omitempty"`
	TTLMillis     int64                      `json:"ttl_ms"`
}

type remoteTurnWaitRequest struct {
	WaitID         string `json:"wait_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type remoteRuntimeLifecycleRequest struct {
	ResultRef     string                         `json:"result_ref,omitempty"`
	Phase         modelturn.RuntimePhase         `json:"phase,omitempty"`
	RetryCategory modelturn.RuntimeRetryCategory `json:"retry_category,omitempty"`
	Count         uint32                         `json:"count,omitempty"`
}

func (t *Transport) LeaseModelRuntime(ctx context.Context, wait time.Duration) (*ModelRuntimeLease, error) {
	if t == nil {
		return nil, errors.New("edge transport is unavailable")
	}
	if wait == 0 {
		wait = remoteModelDefaultWait
	}
	if wait < time.Second || wait > remoteModelMaxWait {
		return nil, errors.New("model runtime lease wait is invalid")
	}
	leaseClient := t.modelRuntimeLeaseClient(wait)
	leaseID, err := randomModelJournalID("el_")
	if err != nil || !remoteLeaseIDPattern.MatchString(leaseID) {
		return nil, errors.New("model runtime lease id generation failed")
	}
	input := map[string]any{"lease_id": leaseID, "wait_seconds": int(wait / time.Second)}
	retryCounts := make(map[modelturn.RuntimeRetryCategory]uint32)
	recordRetry := func(category modelturn.RuntimeRetryCategory) {
		if retryCounts[category] < 100 {
			retryCounts[category]++
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var lease ModelRuntimeLease
		status, callErr := t.doLimitedWithClient(ctx, leaseClient, http.MethodPost, remoteModelRuntimeLeasePath, input, &lease, remoteModelResponseLimit)
		if callErr == nil {
			switch status {
			case http.StatusOK:
				if err := validateModelRuntimeLease(t.identity, lease); err != nil {
					return nil, err
				}
				journal, err := openModelJournal(t.stateRoot)
				if err != nil {
					return nil, err
				}
				recordErr := journal.recordLease(ctx, leaseID, lease)
				closeErr := journal.close()
				if recordErr != nil {
					return nil, recordErr
				}
				if closeErr != nil {
					return nil, errors.New("model runtime lease receipt close failed")
				}
				if len(retryCounts) != 0 {
					lease.RetryCounts = retryCounts
				}
				return &lease, nil
			case http.StatusNoContent:
				return nil, nil
			case http.StatusTooManyRequests:
				recordRetry(modelturn.RuntimeRetryServerBusy)
			case http.StatusBadGateway, http.StatusServiceUnavailable:
				recordRetry(modelturn.RuntimeRetryUpstreamUnavailable)
			case http.StatusGatewayTimeout:
				recordRetry(modelturn.RuntimeRetryGatewayTimeout)
			case http.StatusUnauthorized, http.StatusForbidden:
				return nil, errors.New("model runtime device was rejected")
			default:
				return nil, fmt.Errorf("model runtime lease rejected with HTTP %d", status)
			}
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		} else if isHTTPClientTimeout(callErr) {
			recordRetry(modelturn.RuntimeRetryClientTimeout)
		} else {
			recordRetry(modelturn.RuntimeRetryTransportError)
		}
		if err := waitRetry(ctx, time.Second); err != nil {
			return nil, err
		}
	}
}

func (t *Transport) modelRuntimeLeaseClient(wait time.Duration) *http.Client {
	client := *t.client
	timeout := wait + remoteModelLongPollMargin
	if timeout > remoteModelClientTimeout {
		timeout = remoteModelClientTimeout
	}
	client.Timeout = timeout
	return &client
}

func isHTTPClientTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func NewRemoteEdgeTransport(opts RemoteEdgeTransportOptions) (*RemoteEdgeTransport, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: remoteModelClientTimeout}
	}
	signed, err := NewTransport(opts.StateRoot, client)
	if err != nil {
		return nil, err
	}
	if err := validateModelRuntimeLease(signed.identity, opts.Lease); err != nil {
		return nil, err
	}
	journal, err := openModelJournal(opts.StateRoot)
	if err != nil {
		return nil, err
	}
	if err := journal.validateLease(context.Background(), opts.Lease); err != nil {
		_ = journal.close()
		return nil, err
	}
	wait := opts.LongPoll
	if wait == 0 {
		wait = remoteModelDefaultWait
	}
	if wait < time.Second || wait > remoteModelMaxWait {
		return nil, errors.New("remote model wait is invalid")
	}
	now := time.Now().UTC()
	runtime := modelturn.Runtime{
		RuntimeID: opts.Lease.RuntimeID, DeviceID: opts.Lease.DeviceID, WorkspaceID: opts.Lease.WorkspaceID,
		Controller: opts.Lease.Controller, State: opts.Lease.State, Status: modelturn.RuntimeRunning,
		GoalSummary: modelturn.GoalSummary([]byte(opts.Lease.Goal)), CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(opts.Lease.TimeoutSeconds) * time.Second), UpdatedAt: now,
	}
	return &RemoteEdgeTransport{signed: signed, journal: journal, lease: opts.Lease, longPoll: wait, runtime: runtime}, nil
}

func (r *RemoteEdgeTransport) StageRequestBody(ctx context.Context, payload json.RawMessage, canonical bool, ttl time.Duration) (modelturn.RequestBodyReference, error) {
	if r == nil || !canonical {
		return modelturn.RequestBodyReference{}, modelturn.ErrInvalidRequest
	}
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		return modelturn.RequestBodyReference{}, modelturn.ErrInvalidRequest
	}
	return r.journal.stageBody(ctx, payload, digest, ttl)
}

func (r *RemoteEdgeTransport) CreateTurnFromReference(ctx context.Context, request modelturn.ModelRequest) (modelturn.Turn, error) {
	if r == nil || !localRequestRefPattern.MatchString(request.RequestRef) || len(request.Payload) != 0 {
		return modelturn.Turn{}, modelturn.ErrInvalidRequest
	}
	payload, err := r.journal.loadBody(ctx, request.RequestRef, request.RequestDigest)
	if err != nil {
		return modelturn.Turn{}, err
	}
	request.Payload = payload
	request.CanonicalPayload = true
	localRef := request.RequestRef
	request.RequestRef = ""
	turn, err := r.CreateTurn(ctx, request)
	if err != nil {
		return modelturn.Turn{}, err
	}
	r.journal.deleteBody(context.Background(), localRef)
	return turn, nil
}

func (r *RemoteEdgeTransport) CreateTurn(ctx context.Context, request modelturn.ModelRequest) (modelturn.Turn, error) {
	if r == nil || request.RuntimeID != r.lease.RuntimeID || request.Sequence == 0 || len(request.Payload) == 0 || request.RequestRef != "" || request.TTL <= 0 || request.TTL > modelturn.MaxTurnTTL {
		return modelturn.Turn{}, modelturn.ErrInvalidRequest
	}
	digest, err := modelturn.ExactPayloadDigest(request.Payload)
	if err != nil || digest != request.RequestDigest {
		return modelturn.Turn{}, modelturn.ErrSequenceMismatch
	}
	entry, _, err := r.journal.beginTurn(ctx, request.RuntimeID, request.Sequence, request.RequestDigest)
	if err != nil {
		return modelturn.Turn{}, err
	}
	if entry.TurnID != "" {
		return modelturn.Turn{
			RuntimeID: request.RuntimeID, ID: entry.TurnID, Sequence: request.Sequence,
			RequestDigest: request.RequestDigest, RequestRef: entry.RequestRef,
			OfferedToolIDs: remoteToolIDs(request.OfferedTools),
		}, nil
	}
	input := remoteTurnCreateRequest{
		CreateID: entry.CreateID, Sequence: request.Sequence, RequestDigest: request.RequestDigest,
		Payload: request.Payload, OfferedTools: request.OfferedTools, TTLMillis: int64(request.TTL / time.Millisecond),
	}
	path := remoteModelRuntimePrefix + request.RuntimeID + "/turns"
	var turn modelturn.Turn
	err = r.retry(ctx, func() (bool, error) {
		turn = modelturn.Turn{}
		status, callErr := r.signed.doLimited(ctx, http.MethodPost, path, input, &turn, remoteModelResponseLimit)
		if callErr != nil {
			return true, callErr
		}
		switch status {
		case http.StatusOK, http.StatusCreated:
			return false, nil
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true, fmt.Errorf("remote model create returned HTTP %d", status)
		case http.StatusUnauthorized, http.StatusForbidden:
			return false, errors.New("remote model device was rejected")
		case http.StatusConflict:
			return false, modelturn.ErrTurnConflict
		default:
			return false, fmt.Errorf("remote model create rejected with HTTP %d", status)
		}
	})
	if err != nil {
		return modelturn.Turn{}, err
	}
	if err := validateRemoteTurn(request, turn); err != nil {
		return modelturn.Turn{}, err
	}
	if err := r.journal.bindTurn(ctx, request.RuntimeID, request.Sequence, request.RequestDigest, turn); err != nil {
		return modelturn.Turn{}, err
	}
	r.updateRuntime(func(runtime *modelturn.Runtime) {
		runtime.State = modelturn.RuntimeStateAwaitingModel
		runtime.Status = modelturn.RuntimeRunning
		runtime.LastSequence = turn.Sequence
		runtime.ActiveTurnID = turn.ID
		runtime.ActiveTurnStatus = modelturn.StatusAwaitingModel
		runtime.UpdatedAt = time.Now().UTC()
	})
	return turn, nil
}

func (r *RemoteEdgeTransport) WaitResponse(ctx context.Context, turnID modelturn.TurnID) (modelturn.ModelResponse, error) {
	if r == nil {
		return modelturn.ModelResponse{}, modelturn.ErrInvalidRequest
	}
	entry, err := r.journal.ensureWaitID(ctx, turnID)
	if err != nil {
		return modelturn.ModelResponse{}, err
	}
	if entry.RuntimeID != r.lease.RuntimeID {
		return modelturn.ModelResponse{}, modelturn.ErrTurnNotFound
	}
	path := remoteModelRuntimePrefix + entry.RuntimeID + "/turns/" + string(turnID) + "/wait"
	for {
		if err := ctx.Err(); err != nil {
			return modelturn.ModelResponse{}, err
		}
		wait := r.waitForContext(ctx)
		var response modelturn.ModelResponse
		status, callErr := r.signed.doLimited(ctx, http.MethodPost, path, remoteTurnWaitRequest{WaitID: entry.WaitID, TimeoutSeconds: int(wait / time.Second)}, &response, remoteModelResponseLimit)
		if callErr == nil && status == http.StatusOK {
			if response.RuntimeID != entry.RuntimeID || response.TurnID != turnID || response.Sequence != entry.Sequence || response.RequestDigest != entry.RequestDigest {
				return modelturn.ModelResponse{}, modelturn.ErrSequenceMismatch
			}
			if err := r.journal.markConsumed(ctx, turnID); err != nil {
				return modelturn.ModelResponse{}, err
			}
			r.updateRuntime(func(runtime *modelturn.Runtime) {
				runtime.State = modelturn.RuntimeStateExecutingTools
				runtime.ActiveTurnStatus = modelturn.StatusConsumed
				runtime.UpdatedAt = time.Now().UTC()
			})
			return response, nil
		}
		if callErr == nil {
			switch status {
			case http.StatusNoContent, http.StatusConflict, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				// Recover using the same wait ID; the VPS replays the same consumed body.
			case http.StatusUnauthorized, http.StatusForbidden:
				return modelturn.ModelResponse{}, errors.New("remote model device was rejected")
			case http.StatusNotFound:
				return modelturn.ModelResponse{}, modelturn.ErrTurnNotFound
			default:
				return modelturn.ModelResponse{}, fmt.Errorf("remote model wait rejected with HTTP %d", status)
			}
		}
		if err := waitRetry(ctx, time.Second); err != nil {
			return modelturn.ModelResponse{}, err
		}
	}
}

func (r *RemoteEdgeTransport) Cancel(ctx context.Context, turnID modelturn.TurnID) error {
	if r == nil {
		return modelturn.ErrInvalidRequest
	}
	entry, err := r.journal.turnByID(ctx, turnID)
	if err != nil {
		return err
	}
	if entry.RuntimeID != r.lease.RuntimeID {
		return modelturn.ErrTurnNotFound
	}
	path := remoteModelRuntimePrefix + entry.RuntimeID + "/turns/" + string(turnID) + "/cancel"
	var output struct {
		RuntimeID string           `json:"runtime_id"`
		TurnID    modelturn.TurnID `json:"turn_id"`
		Status    modelturn.Status `json:"status"`
	}
	err = r.retry(ctx, func() (bool, error) {
		output = struct {
			RuntimeID string           `json:"runtime_id"`
			TurnID    modelturn.TurnID `json:"turn_id"`
			Status    modelturn.Status `json:"status"`
		}{}
		status, callErr := r.signed.doLimited(ctx, http.MethodPost, path, struct{}{}, &output, 64<<10)
		if callErr != nil {
			return true, callErr
		}
		switch status {
		case http.StatusOK:
			return false, nil
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true, fmt.Errorf("remote model cancel returned HTTP %d", status)
		case http.StatusNotFound:
			return false, modelturn.ErrTurnNotFound
		case http.StatusUnauthorized, http.StatusForbidden:
			return false, errors.New("remote model device was rejected")
		default:
			return false, modelturn.ErrTurnConflict
		}
	})
	if err != nil {
		return err
	}
	if output.RuntimeID != entry.RuntimeID || output.TurnID != turnID || output.Status != modelturn.StatusCancelled {
		return modelturn.ErrTurnConflict
	}
	if err := r.journal.markCancelled(ctx, turnID); err != nil {
		return err
	}
	r.updateRuntime(func(runtime *modelturn.Runtime) {
		runtime.State = modelturn.RuntimeStateDisconnected
		runtime.ActiveTurnStatus = modelturn.StatusCancelled
		runtime.UpdatedAt = time.Now().UTC()
	})
	return nil
}

func (r *RemoteEdgeTransport) Runtime(_ context.Context, runtimeID string) (modelturn.Runtime, error) {
	if r == nil || runtimeID != r.lease.RuntimeID {
		return modelturn.Runtime{}, modelturn.ErrTurnNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runtime, nil
}

func (r *RemoteEdgeTransport) Stats(ctx context.Context) (modelturn.StoreStats, error) {
	if r == nil {
		return modelturn.StoreStats{}, modelturn.ErrInvalidRequest
	}
	return r.journal.stats(ctx)
}

func (r *RemoteEdgeTransport) Started(ctx context.Context) (modelturn.Runtime, error) {
	return r.lifecycle(ctx, "started", "")
}

func (r *RemoteEdgeTransport) Heartbeat(ctx context.Context) (modelturn.Runtime, error) {
	return r.lifecycle(ctx, "heartbeat", "")
}

func (r *RemoteEdgeTransport) Failed(ctx context.Context, resultRef string) (modelturn.Runtime, error) {
	return r.lifecycle(ctx, "failed", resultRef)
}

func (r *RemoteEdgeTransport) Completed(ctx context.Context, resultRef string) (modelturn.Runtime, error) {
	return r.lifecycle(ctx, "completed", resultRef)
}

func (r *RemoteEdgeTransport) ReportPhase(ctx context.Context, phase modelturn.RuntimePhase, category modelturn.RuntimeRetryCategory, count uint32) (modelturn.Runtime, error) {
	return r.lifecycleRequest(ctx, "phase", remoteRuntimeLifecycleRequest{Phase: phase, RetryCategory: category, Count: count})
}

func (r *RemoteEdgeTransport) lifecycle(ctx context.Context, action, resultRef string) (modelturn.Runtime, error) {
	return r.lifecycleRequest(ctx, action, remoteRuntimeLifecycleRequest{ResultRef: resultRef})
}

func (r *RemoteEdgeTransport) lifecycleRequest(ctx context.Context, action string, input remoteRuntimeLifecycleRequest) (modelturn.Runtime, error) {
	if r == nil {
		return modelturn.Runtime{}, modelturn.ErrInvalidRequest
	}
	path := remoteModelRuntimePrefix + r.lease.RuntimeID + "/" + action
	var runtime modelturn.Runtime
	err := r.retry(ctx, func() (bool, error) {
		runtime = modelturn.Runtime{}
		status, callErr := r.signed.doLimited(ctx, http.MethodPost, path, input, &runtime, 256<<10)
		if callErr != nil {
			return true, callErr
		}
		switch status {
		case http.StatusOK:
			return false, nil
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true, fmt.Errorf("remote model lifecycle returned HTTP %d", status)
		case http.StatusNotFound:
			return false, modelturn.ErrTurnNotFound
		case http.StatusUnauthorized, http.StatusForbidden:
			return false, errors.New("remote model device was rejected")
		default:
			return false, modelturn.ErrTurnConflict
		}
	})
	if err != nil {
		return modelturn.Runtime{}, err
	}
	if runtime.RuntimeID != r.lease.RuntimeID || runtime.DeviceID != r.lease.DeviceID || runtime.WorkspaceID != r.lease.WorkspaceID || runtime.Controller != modelturn.ControllerRemoteEdge {
		return modelturn.Runtime{}, modelturn.ErrTurnConflict
	}
	r.mu.Lock()
	r.runtime = runtime
	r.mu.Unlock()
	return runtime, nil
}

func (r *RemoteEdgeTransport) Close() error {
	if r == nil {
		return nil
	}
	return r.journal.close()
}

func (r *RemoteEdgeTransport) retry(ctx context.Context, operation func() (bool, error)) error {
	delay := 250 * time.Millisecond
	for {
		retry, err := operation()
		if err == nil {
			return nil
		}
		if !retry {
			return err
		}
		if err := waitRetry(ctx, delay); err != nil {
			return err
		}
		if delay < 2*time.Second {
			delay *= 2
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
		}
	}
}

func (r *RemoteEdgeTransport) waitForContext(ctx context.Context) time.Duration {
	wait := r.longPoll
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < wait {
			wait = remaining
		}
	}
	if wait < time.Second {
		return time.Second
	}
	if wait > remoteModelMaxWait {
		return remoteModelMaxWait
	}
	return wait.Truncate(time.Second)
}

func (r *RemoteEdgeTransport) updateRuntime(update func(*modelturn.Runtime)) {
	r.mu.Lock()
	update(&r.runtime)
	r.mu.Unlock()
}

func validateModelRuntimeLease(identity Identity, lease ModelRuntimeLease) error {
	if !remoteRuntimeIDPattern.MatchString(lease.RuntimeID) || lease.DeviceID != identity.DeviceID || !workspaceIDPattern.MatchString(lease.WorkspaceID) || lease.Controller != modelturn.ControllerRemoteEdge || lease.ProviderProfile != remoteProviderProfile || lease.TimeoutSeconds < 1 || lease.State != modelturn.RuntimeStateStarting {
		return errors.New("model runtime lease is invalid")
	}
	if lease.Goal == "" || len(lease.Goal) > int(modelturn.MaxGoalBodyBytes) || !strings.HasPrefix(lease.GoalDigest, "sha256:") {
		return errors.New("model runtime lease is invalid")
	}
	sum := sha256.Sum256([]byte(lease.Goal))
	if lease.GoalDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		return errors.New("model runtime goal digest is invalid")
	}
	return nil
}

func validateRemoteTurn(request modelturn.ModelRequest, turn modelturn.Turn) error {
	if turn.RuntimeID != request.RuntimeID || turn.Sequence != request.Sequence || turn.RequestDigest != request.RequestDigest || !remoteTurnIDPattern.MatchString(string(turn.ID)) || !remoteBodyRefPattern.MatchString(turn.RequestRef) {
		return modelturn.ErrSequenceMismatch
	}
	wantTools := remoteToolIDs(request.OfferedTools)
	if len(turn.OfferedToolIDs) != len(wantTools) {
		return modelturn.ErrToolNotOffered
	}
	for index := range wantTools {
		if turn.OfferedToolIDs[index] != wantTools[index] {
			return modelturn.ErrToolNotOffered
		}
	}
	return nil
}

func remoteToolIDs(tools []modelturn.ToolDefinition) []string {
	ids := make([]string, 0, len(tools))
	for _, tool := range tools {
		ids = append(ids, tool.ID)
	}
	sort.Strings(ids)
	return ids
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
