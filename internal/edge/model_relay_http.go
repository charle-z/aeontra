package edge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	modelRuntimeLeasePath = "/edge/v1/model-runtimes/lease"
	modelRuntimePrefix    = "/edge/v1/model-runtimes/"
	maxModelRelayBody     = modelturn.MaxRequestBodyBytes + (256 << 10)
	maxModelLongPoll      = 180 * time.Second
	providerProfile       = "opencode-external-v1"
)

type modelRelay struct {
	devices *Store
	turns   *modelturn.Store
	waitMu  sync.Mutex
	waits   map[string]struct{}
}

type modelRuntimeLeaseRequest struct {
	LeaseID     string `json:"lease_id"`
	WaitSeconds int    `json:"wait_seconds"`
}

type modelRuntimeLeaseResponse struct {
	RuntimeID       string                      `json:"runtime_id"`
	DeviceID        string                      `json:"device_id"`
	WorkspaceID     string                      `json:"workspace_id"`
	Controller      modelturn.RuntimeController `json:"controller"`
	State           modelturn.RuntimeState      `json:"state"`
	Goal            string                      `json:"goal"`
	GoalDigest      string                      `json:"goal_digest"`
	TimeoutSeconds  int                         `json:"timeout_seconds"`
	ProviderProfile string                      `json:"provider_profile"`
}

type modelRuntimeLifecycleRequest struct {
	ResultRef string `json:"result_ref,omitempty"`
}

type modelTurnCreateRequest struct {
	CreateID      string                     `json:"create_id"`
	Sequence      uint64                     `json:"sequence"`
	RequestDigest string                     `json:"request_digest"`
	Payload       json.RawMessage            `json:"payload"`
	OfferedTools  []modelturn.ToolDefinition `json:"offered_tools,omitempty"`
	TTLMillis     int64                      `json:"ttl_ms"`
}

type modelTurnWaitRequest struct {
	WaitID         string `json:"wait_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func registerModelRelayRoutes(mux *http.ServeMux, devices *Store, turns *modelturn.Store) {
	relay := &modelRelay{devices: devices, turns: turns, waits: make(map[string]struct{})}
	mux.Handle(modelRuntimeLeasePath, devices.requireDevice(http.HandlerFunc(relay.handleLease), maxSignedBody))
	mux.Handle(modelRuntimePrefix, devices.requireDevice(http.HandlerFunc(relay.handleRuntime), maxModelRelayBody))
}

func (r *modelRelay) handleLease(w http.ResponseWriter, request *http.Request) {
	if !requirePOST(w, request) {
		return
	}
	var input modelRuntimeLeaseRequest
	if !decodeStrictJSON(w, request, &input) {
		return
	}
	if !modelLeaseIDPattern.MatchString(input.LeaseID) {
		http.Error(w, "lease identity is invalid", http.StatusBadRequest)
		return
	}
	if input.WaitSeconds == 0 {
		input.WaitSeconds = 120
	}
	if input.WaitSeconds < 1 || time.Duration(input.WaitSeconds)*time.Second > maxModelLongPoll {
		http.Error(w, "lease wait is invalid", http.StatusBadRequest)
		return
	}
	device := DeviceFromContext(request.Context())
	if receipt, err := r.devices.modelRuntimeLeaseReceipt(device.ID, input.LeaseID); err == nil {
		runtime, runtimeErr := r.turns.RuntimeForDevice(request.Context(), receipt.RuntimeID, device.ID)
		if runtimeErr != nil || runtime.Controller != modelturn.ControllerRemoteEdge {
			http.Error(w, "runtime lease receipt unavailable", http.StatusConflict)
			return
		}
		r.writeLease(w, request, device, runtime)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), time.Duration(input.WaitSeconds)*time.Second)
	defer cancel()
	runtime, found, err := r.turns.WaitLeaseNextRuntime(ctx, device.ID)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "runtime lease rejected", http.StatusConflict)
		return
	}
	if err := r.devices.recordModelRuntimeLease(device.ID, input.LeaseID, runtime.RuntimeID); err != nil {
		http.Error(w, "runtime lease receipt rejected", http.StatusConflict)
		return
	}
	r.writeLease(w, request, device, runtime)
}

func (r *modelRelay) writeLease(w http.ResponseWriter, request *http.Request, device Device, runtime modelturn.Runtime) {
	goal, digest, err := r.turns.RuntimeGoal(request.Context(), runtime.RuntimeID, device.ID)
	if err != nil || !utf8.Valid(goal) {
		_ = r.turns.FailRuntime(context.Background(), runtime.RuntimeID)
		http.Error(w, "runtime goal unavailable", http.StatusConflict)
		return
	}
	remaining := time.Until(runtime.ExpiresAt)
	if remaining <= 0 {
		_ = r.turns.FailRuntime(context.Background(), runtime.RuntimeID)
		http.Error(w, "runtime expired", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, modelRuntimeLeaseResponse{
		RuntimeID: runtime.RuntimeID, DeviceID: device.ID, WorkspaceID: runtime.WorkspaceID,
		Controller: runtime.Controller, State: runtime.State, Goal: string(goal), GoalDigest: digest,
		TimeoutSeconds: int(remaining.Round(time.Second) / time.Second), ProviderProfile: providerProfile,
	})
}

func (r *modelRelay) handleRuntime(w http.ResponseWriter, request *http.Request) {
	if !requirePOST(w, request) {
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, modelRuntimePrefix)
	parts := strings.Split(remainder, "/")
	if len(parts) < 2 || parts[0] == "" {
		http.NotFound(w, request)
		return
	}
	device := DeviceFromContext(request.Context())
	runtime, err := r.turns.RuntimeForDevice(request.Context(), parts[0], device.ID)
	if err != nil || runtime.Controller != modelturn.ControllerRemoteEdge {
		http.NotFound(w, request)
		return
	}
	if len(parts) == 2 {
		if parts[1] == "turns" {
			r.handleCreateTurn(w, request, device, runtime)
		} else {
			r.handleRuntimeAction(w, request, device, runtime, parts[1])
		}
		return
	}
	if parts[1] != "turns" {
		http.NotFound(w, request)
		return
	}
	if len(parts) != 4 {
		http.NotFound(w, request)
		return
	}
	turnID := modelturn.TurnID(parts[2])
	record, err := r.turns.Get(request.Context(), turnID)
	if err != nil || record.RuntimeID != runtime.RuntimeID {
		http.NotFound(w, request)
		return
	}
	switch parts[3] {
	case "wait":
		r.handleWaitTurn(w, request, device, runtime, turnID)
	case "cancel":
		var input struct{}
		if !decodeStrictJSON(w, request, &input) {
			return
		}
		if record.Status == modelturn.StatusCancelled {
			writeJSON(w, http.StatusOK, map[string]any{"runtime_id": runtime.RuntimeID, "turn_id": turnID, "status": modelturn.StatusCancelled})
			return
		}
		if err := r.turns.Cancel(request.Context(), turnID); err != nil {
			http.Error(w, "turn cancellation rejected", http.StatusConflict)
			return
		}
		_ = r.turns.SetRuntimeState(context.Background(), runtime.RuntimeID, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateExecutingTools, modelturn.RuntimeStateDisconnected)
		writeJSON(w, http.StatusOK, map[string]any{"runtime_id": runtime.RuntimeID, "turn_id": turnID, "status": modelturn.StatusCancelled})
	default:
		http.NotFound(w, request)
	}
}

func (r *modelRelay) handleRuntimeAction(w http.ResponseWriter, request *http.Request, device Device, runtime modelturn.Runtime, action string) {
	var input modelRuntimeLifecycleRequest
	if !decodeStrictJSON(w, request, &input) {
		return
	}
	switch action {
	case "heartbeat":
		switch runtime.State {
		case modelturn.RuntimeStateCompleted, modelturn.RuntimeStateFailed, modelturn.RuntimeStateCancelled, modelturn.RuntimeStateExpired:
			writeJSON(w, http.StatusOK, runtime)
			return
		}
		updated, err := r.turns.HeartbeatRuntime(request.Context(), runtime.RuntimeID, device.ID)
		if err != nil {
			http.Error(w, "runtime heartbeat rejected", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case "started":
		_, _ = r.turns.ResumeRuntime(request.Context(), runtime.RuntimeID)
		if err := r.turns.SetRuntimeState(request.Context(), runtime.RuntimeID, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateStarting, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateAwaitingModel); err != nil {
			http.Error(w, "runtime start rejected", http.StatusConflict)
			return
		}
		updated, _ := r.turns.HeartbeatRuntime(request.Context(), runtime.RuntimeID, device.ID)
		writeJSON(w, http.StatusOK, updated)
	case "failed":
		if runtime.State == modelturn.RuntimeStateFailed {
			if input.ResultRef != "" && input.ResultRef != runtime.ResultRef {
				http.Error(w, "runtime result conflict", http.StatusConflict)
				return
			}
			writeJSON(w, http.StatusOK, runtime)
			return
		}
		if input.ResultRef != "" {
			if err := r.turns.SetRuntimeResult(request.Context(), runtime.RuntimeID, device.ID, input.ResultRef); err != nil {
				http.Error(w, "runtime result rejected", http.StatusConflict)
				return
			}
		}
		if err := r.turns.FailRuntime(request.Context(), runtime.RuntimeID); err != nil {
			http.Error(w, "runtime failure rejected", http.StatusConflict)
			return
		}
		updated, _ := r.turns.RuntimeForDevice(request.Context(), runtime.RuntimeID, device.ID)
		writeJSON(w, http.StatusOK, updated)
	case "completed":
		if runtime.State == modelturn.RuntimeStateCompleted {
			if input.ResultRef != "" && input.ResultRef != runtime.ResultRef {
				http.Error(w, "runtime result conflict", http.StatusConflict)
				return
			}
			writeJSON(w, http.StatusOK, runtime)
			return
		}
		if input.ResultRef != "" {
			if err := r.turns.SetRuntimeResult(request.Context(), runtime.RuntimeID, device.ID, input.ResultRef); err != nil {
				http.Error(w, "runtime result rejected", http.StatusConflict)
				return
			}
		}
		if err := r.turns.CompleteRuntime(request.Context(), runtime.RuntimeID); err != nil {
			http.Error(w, "runtime completion rejected", http.StatusConflict)
			return
		}
		updated, _ := r.turns.RuntimeForDevice(request.Context(), runtime.RuntimeID, device.ID)
		writeJSON(w, http.StatusOK, updated)
	default:
		http.NotFound(w, request)
	}
}

func (r *modelRelay) handleCreateTurn(w http.ResponseWriter, request *http.Request, device Device, runtime modelturn.Runtime) {
	switch runtime.State {
	case modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateExecutingTools, modelturn.RuntimeStateDisconnected:
	default:
		http.Error(w, "runtime is not ready for model turns", http.StatusConflict)
		return
	}
	var input modelTurnCreateRequest
	if !decodeStrictJSON(w, request, &input) {
		return
	}
	if !modelCreateIDPattern.MatchString(input.CreateID) || input.Sequence == 0 || input.TTLMillis <= 0 || time.Duration(input.TTLMillis)*time.Millisecond > modelturn.MaxTurnTTL || len(input.Payload) == 0 || int64(len(input.Payload)) > modelturn.MaxRequestBodyBytes {
		http.Error(w, "turn request is invalid", http.StatusBadRequest)
		return
	}
	digest, err := modelturn.ExactPayloadDigest(input.Payload)
	if err != nil || digest != input.RequestDigest {
		http.Error(w, "turn digest is invalid", http.StatusBadRequest)
		return
	}
	if receipt, err := r.devices.modelCreateReceipt(device.ID, input.CreateID); err == nil {
		if receipt.RuntimeID != runtime.RuntimeID || receipt.Sequence != input.Sequence || receipt.RequestDigest != input.RequestDigest {
			http.Error(w, "turn create conflict", http.StatusConflict)
			return
		}
		turn, readErr := r.turns.TurnBySequence(request.Context(), runtime.RuntimeID, input.Sequence)
		if readErr != nil || turn.ID != receipt.TurnID || turn.RequestRef != receipt.RequestRef {
			http.Error(w, "turn create receipt unavailable", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, turn)
		return
	}
	if existing, err := r.turns.TurnBySequence(request.Context(), runtime.RuntimeID, input.Sequence); err == nil {
		if existing.RequestDigest != input.RequestDigest {
			http.Error(w, "turn sequence conflict", http.StatusConflict)
			return
		}
		if err := r.devices.recordModelCreate(device.ID, runtime.RuntimeID, input.CreateID, existing); err != nil {
			http.Error(w, "turn create receipt conflict", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, existing)
		return
	}
	modelRequest := modelturn.ModelRequest{
		RuntimeID: runtime.RuntimeID, Sequence: input.Sequence, Payload: input.Payload,
		CanonicalPayload: true, RequestDigest: input.RequestDigest, OfferedTools: input.OfferedTools,
		TTL: time.Duration(input.TTLMillis) * time.Millisecond,
	}
	var turn modelturn.Turn
	if int64(len(input.Payload)) > modelturn.MaxInlineRequestBytes {
		body, stageErr := r.turns.StageRequestBody(request.Context(), input.Payload, true, modelRequest.TTL)
		if stageErr != nil || body.RequestDigest != input.RequestDigest {
			http.Error(w, "turn request body rejected", http.StatusConflict)
			return
		}
		modelRequest.Payload = nil
		modelRequest.RequestRef = body.RequestRef
		turn, err = r.turns.CreateTurnFromReference(request.Context(), modelRequest)
	} else {
		turn, err = r.turns.CreateTurn(request.Context(), modelRequest)
	}
	if err != nil || turn.RequestDigest != input.RequestDigest || turn.RuntimeID != runtime.RuntimeID || turn.Sequence != input.Sequence {
		http.Error(w, "turn creation rejected", http.StatusConflict)
		return
	}
	if err := r.devices.recordModelCreate(device.ID, runtime.RuntimeID, input.CreateID, turn); err != nil {
		http.Error(w, "turn create receipt rejected", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusCreated, turn)
}

func (r *modelRelay) handleWaitTurn(w http.ResponseWriter, request *http.Request, device Device, runtime modelturn.Runtime, turnID modelturn.TurnID) {
	var input modelTurnWaitRequest
	if !decodeStrictJSON(w, request, &input) {
		return
	}
	if !modelWaitIDPattern.MatchString(input.WaitID) || input.TimeoutSeconds < 1 || time.Duration(input.TimeoutSeconds)*time.Second > maxModelLongPoll {
		http.Error(w, "turn wait is invalid", http.StatusBadRequest)
		return
	}
	_, _, err := r.devices.ensureModelWaitReceipt(device.ID, runtime.RuntimeID, turnID, input.WaitID)
	if err != nil {
		http.Error(w, "turn wait conflict", http.StatusConflict)
		return
	}
	key := device.ID + "/" + runtime.RuntimeID
	if !r.acquireWait(key) {
		http.Error(w, "runtime already has an active wait", http.StatusConflict)
		return
	}
	defer r.releaseWait(key)

	if snapshot, status, err := r.turns.ResponseSnapshot(request.Context(), turnID); err == nil && status == modelturn.StatusConsumed {
		writeJSON(w, http.StatusOK, snapshot)
		return
	}
	if runtime.State == modelturn.RuntimeStateDisconnected {
		_, _ = r.turns.ResumeRuntime(request.Context(), runtime.RuntimeID)
		_ = r.turns.SetRuntimeState(request.Context(), runtime.RuntimeID, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateAwaitingModel)
	}
	ctx, cancel := context.WithTimeout(request.Context(), time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	go r.cancelWhenRevoked(ctx, cancel, device.ID)
	response, err := r.turns.WaitResponse(ctx, turnID)
	if err == nil {
		_ = r.turns.SetRuntimeState(context.Background(), runtime.RuntimeID, modelturn.RuntimeStateExecutingTools, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateExecutingTools)
		writeJSON(w, http.StatusOK, response)
		return
	}
	if !r.devices.deviceActive(device.ID) {
		http.Error(w, "edge authentication failed", http.StatusUnauthorized)
		return
	}
	if errors.Is(err, modelturn.ErrResponseReplay) {
		if snapshot, _, snapshotErr := r.turns.ResponseSnapshot(request.Context(), turnID); snapshotErr == nil {
			writeJSON(w, http.StatusOK, snapshot)
			return
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		_ = r.turns.MarkDisconnected(context.Background(), turnID)
		_ = r.turns.SetRuntimeState(context.Background(), runtime.RuntimeID, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateExecutingTools, modelturn.RuntimeStateDisconnected)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "turn wait rejected", http.StatusConflict)
}

func (r *modelRelay) acquireWait(key string) bool {
	r.waitMu.Lock()
	defer r.waitMu.Unlock()
	if _, exists := r.waits[key]; exists {
		return false
	}
	r.waits[key] = struct{}{}
	return true
}

func (r *modelRelay) releaseWait(key string) {
	r.waitMu.Lock()
	delete(r.waits, key)
	r.waitMu.Unlock()
}

func (r *modelRelay) cancelWhenRevoked(ctx context.Context, cancel context.CancelFunc, deviceID string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !r.devices.deviceActive(deviceID) {
				cancel()
				return
			}
		}
	}
}
