package modelturn

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
)

type RuntimeStatus string
type RuntimeState string
type RuntimeController string

const (
	RuntimeReady     RuntimeStatus = "ready"
	RuntimeRunning   RuntimeStatus = "running"
	RuntimeCompleted RuntimeStatus = "completed"
	RuntimeCancelled RuntimeStatus = "cancelled"
	RuntimeFailed    RuntimeStatus = "failed"
)

const (
	RuntimeStateRequested      RuntimeState = "requested"
	RuntimeStateAwaitingEdge   RuntimeState = "awaiting_edge"
	RuntimeStateStarting       RuntimeState = "starting"
	RuntimeStateAwaitingModel  RuntimeState = "awaiting_model"
	RuntimeStateExecutingTools RuntimeState = "executing_tools"
	RuntimeStateCompleted      RuntimeState = "completed"
	RuntimeStateFailed         RuntimeState = "failed"
	RuntimeStateCancelled      RuntimeState = "cancelled"
	RuntimeStateDisconnected   RuntimeState = "disconnected"
	RuntimeStateExpired        RuntimeState = "expired"
)

const (
	ControllerPullRendezvous RuntimeController = "pull_rendezvous"
	ControllerRemoteEdge     RuntimeController = "remote_edge"
	ControllerMCPSampling    RuntimeController = "mcp_sampling"
)

const OpenCodeProviderProfile = "opencode-external-v1"

const MaxGoalBodyBytes = int64(64 << 10)

var (
	deviceIDPattern          = regexp.MustCompile(`^ed_[a-f0-9]{32}$`)
	workspaceIDPattern       = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)
	goalSummaryPattern       = regexp.MustCompile(`^goal:sha256:[a-f0-9]{24}$`)
	goalDigestPattern        = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	idempotencyDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	resultReferencePattern   = regexp.MustCompile(`^(?:rs|mb)_[a-f0-9]{32,64}$`)
)

type Runtime struct {
	RuntimeID        string            `json:"runtime_id"`
	DeviceID         string            `json:"device_id,omitempty"`
	WorkspaceID      string            `json:"workspace_id,omitempty"`
	Controller       RuntimeController `json:"controller"`
	State            RuntimeState      `json:"state"`
	Status           RuntimeStatus     `json:"status"`
	GoalSummary      string            `json:"goal_summary,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	ExpiresAt        time.Time         `json:"expires_at"`
	LastHeartbeat    time.Time         `json:"last_heartbeat,omitempty"`
	LastSequence     uint64            `json:"last_sequence"`
	ActiveTurnID     TurnID            `json:"active_turn_id,omitempty"`
	ActiveTurnStatus Status            `json:"active_turn_status,omitempty"`
	ResultRef        string            `json:"result_ref,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at"`

	goalRef              string
	goalDigest           string
	idempotencyKeyDigest string
}

type BoundRuntimeRequest struct {
	DeviceID             string
	WorkspaceID          string
	Controller           RuntimeController
	GoalSummary          string
	GoalRef              string
	GoalDigest           string
	IdempotencyKeyDigest string
	TTL                  time.Duration
}

type RuntimeBodyReference struct {
	BodyRef       string    `json:"body_ref"`
	ContentDigest string    `json:"content_digest"`
	ContentBytes  int64     `json:"content_bytes"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func GoalSummary(goal []byte) string {
	sum := sha256.Sum256(goal)
	return "goal:sha256:" + hex.EncodeToString(sum[:12])
}

func IdempotencyDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Store) StageRuntimeGoal(ctx context.Context, content []byte, ttl time.Duration) (RuntimeBodyReference, error) {
	if len(content) == 0 || int64(len(content)) > MaxGoalBodyBytes {
		return RuntimeBodyReference{}, ErrBodyTooLarge
	}
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl <= 0 || ttl > MaxTurnTTL {
		return RuntimeBodyReference{}, ErrInvalidRequest
	}
	ref, err := newOpaqueID("mb_")
	if err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal reference generation failed")
	}
	now := s.now().UTC()
	expires := now.Add(ttl)
	digest := digestBytes(content)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal transaction failed")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_bodies WHERE expires_at<=?`, now.UnixNano()); err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal cleanup failed")
	}
	var used int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COALESCE(SUM(content_bytes),0) FROM turn_bodies)+(SELECT COALESCE(SUM(content_bytes),0) FROM runtime_bodies)`).Scan(&used); err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal quota check failed")
	}
	if used+int64(len(content)) > s.quotaBytes {
		return RuntimeBodyReference{}, ErrTurnQuotaExceeded
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_bodies(body_ref,kind,content,content_digest,content_bytes,created_at,expires_at) VALUES(?,'goal',?,?,?,?,?)`, ref, content, digest, len(content), now.UnixNano(), expires.UnixNano()); err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return RuntimeBodyReference{}, errors.New("runtime goal commit failed")
	}
	return RuntimeBodyReference{BodyRef: ref, ContentDigest: digest, ContentBytes: int64(len(content)), ExpiresAt: expires}, nil
}

func (s *Store) StartRuntime(ctx context.Context) (Runtime, error) {
	now := s.now().UTC()
	return s.startRuntime(ctx, BoundRuntimeRequest{Controller: ControllerPullRendezvous, TTL: MaxTurnTTL}, now)
}

func (s *Store) StartBoundRuntime(ctx context.Context, request BoundRuntimeRequest) (Runtime, bool, error) {
	if !deviceIDPattern.MatchString(request.DeviceID) || !workspaceIDPattern.MatchString(request.WorkspaceID) || request.Controller != ControllerRemoteEdge || !goalSummaryPattern.MatchString(request.GoalSummary) || !strings.HasPrefix(request.GoalRef, "mb_") || !goalDigestPattern.MatchString(request.GoalDigest) || !idempotencyDigestPattern.MatchString(request.IdempotencyKeyDigest) {
		return Runtime{}, false, ErrInvalidRequest
	}
	if request.TTL <= 0 || request.TTL > MaxTurnTTL {
		return Runtime{}, false, ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	var goalDigest string
	if err := s.db.QueryRowContext(ctx, `SELECT content_digest FROM runtime_bodies WHERE body_ref=? AND kind='goal' AND expires_at>?`, request.GoalRef, now.UnixNano()).Scan(&goalDigest); err != nil {
		return Runtime{}, false, ErrRequestRefConflict
	}
	if goalDigest != request.GoalDigest || !strings.HasPrefix(goalDigest, "sha256:") || len(goalDigest) != len("sha256:")+64 || request.GoalSummary != "goal:sha256:"+goalDigest[len("sha256:"):len("sha256:")+24] {
		return Runtime{}, false, ErrRequestRefConflict
	}
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT runtime_id FROM model_runtimes WHERE device_id=? AND idempotency_key_digest=?`, request.DeviceID, request.IdempotencyKeyDigest).Scan(&existingID)
	if err == nil {
		runtime, readErr := s.runtimeLocked(ctx, existingID)
		if readErr != nil {
			return Runtime{}, false, readErr
		}
		if runtime.WorkspaceID != request.WorkspaceID || runtime.Controller != request.Controller || runtime.GoalSummary != request.GoalSummary || runtime.goalDigest != request.GoalDigest {
			return Runtime{}, false, ErrTurnConflict
		}
		return runtime, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, false, errors.New("model runtime idempotency lookup failed")
	}
	runtime, err := s.startRuntimeLocked(ctx, request, now)
	return runtime, err == nil, err
}

func (s *Store) startRuntime(ctx context.Context, request BoundRuntimeRequest, now time.Time) (Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startRuntimeLocked(ctx, request, now)
}

func (s *Store) startRuntimeLocked(ctx context.Context, request BoundRuntimeRequest, now time.Time) (Runtime, error) {
	runtimeID, err := newOpaqueID("mr_")
	if err != nil {
		return Runtime{}, errors.New("model runtime id generation failed")
	}
	controller := request.Controller
	if controller == "" {
		controller = ControllerPullRendezvous
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = MaxTurnTTL
	}
	state := RuntimeStateRequested
	if controller == ControllerRemoteEdge {
		state = RuntimeStateAwaitingEdge
	} else if controller == ControllerPullRendezvous {
		state = RuntimeStateAwaitingModel
	}
	expires := now.Add(ttl)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO model_runtimes(runtime_id,status,device_id,workspace_id,controller,state,goal_summary,goal_ref,goal_digest,expires_at,last_heartbeat,last_sequence,active_turn_id,result_ref,idempotency_key_digest,created_at,updated_at) VALUES(?,'ready',?,?,?,?,?,?,?,?,0,0,'','',?,?,?)`,
		runtimeID, request.DeviceID, request.WorkspaceID, controller, state, request.GoalSummary, request.GoalRef, request.GoalDigest, expires.UnixNano(), request.IdempotencyKeyDigest, now.UnixNano(), now.UnixNano()); err != nil {
		return Runtime{}, errors.New("model runtime persistence failed")
	}
	s.signal()
	return Runtime{RuntimeID: runtimeID, DeviceID: request.DeviceID, WorkspaceID: request.WorkspaceID, Controller: controller, State: state, Status: RuntimeReady, GoalSummary: request.GoalSummary, CreatedAt: now, ExpiresAt: expires, UpdatedAt: now, goalRef: request.GoalRef, goalDigest: request.GoalDigest, idempotencyKeyDigest: request.IdempotencyKeyDigest}, nil
}

func (s *Store) SetRuntimeRunning(ctx context.Context, runtimeID string) error {
	return s.SetRuntimeState(ctx, runtimeID, RuntimeStateStarting, RuntimeStateRequested, RuntimeStateAwaitingEdge, RuntimeStateStarting)
}

func (s *Store) CompleteRuntime(ctx context.Context, runtimeID string) error {
	return s.SetRuntimeState(ctx, runtimeID, RuntimeStateCompleted, RuntimeStateRequested, RuntimeStateAwaitingEdge, RuntimeStateStarting, RuntimeStateAwaitingModel, RuntimeStateExecutingTools, RuntimeStateDisconnected)
}

func (s *Store) FailRuntime(ctx context.Context, runtimeID string) error {
	return s.SetRuntimeState(ctx, runtimeID, RuntimeStateFailed, RuntimeStateRequested, RuntimeStateAwaitingEdge, RuntimeStateStarting, RuntimeStateAwaitingModel, RuntimeStateExecutingTools, RuntimeStateDisconnected)
}

func (s *Store) SetRuntimeState(ctx context.Context, runtimeID string, target RuntimeState, allowed ...RuntimeState) error {
	if !safeIdentifier.MatchString(runtimeID) || !validRuntimeState(target) || len(allowed) == 0 {
		return ErrInvalidRequest
	}
	now := s.now().UTC()
	args := make([]any, 0, len(allowed)+4)
	query := `UPDATE model_runtimes SET state=?,status=?,updated_at=? WHERE runtime_id=? AND state IN (`
	args = append(args, target, legacyStatusForState(target), now.UnixNano(), runtimeID)
	for index, state := range allowed {
		if !validRuntimeState(state) {
			return ErrInvalidRequest
		}
		if index > 0 {
			query += ","
		}
		query += "?"
		args = append(args, state)
	}
	query += ")"
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return errors.New("model runtime transition failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	s.signal()
	return nil
}

func (s *Store) HeartbeatRuntime(ctx context.Context, runtimeID, deviceID string) (Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) || !deviceIDPattern.MatchString(deviceID) {
		return Runtime{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_runtimes SET last_heartbeat=?,updated_at=? WHERE runtime_id=? AND device_id=? AND state NOT IN ('completed','failed','cancelled','expired') AND expires_at>?`, now.UnixNano(), now.UnixNano(), runtimeID, deviceID, now.UnixNano())
	if err != nil {
		return Runtime{}, errors.New("model runtime heartbeat failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Runtime{}, ErrTurnConflict
	}
	return s.runtimeLocked(ctx, runtimeID)
}

func (s *Store) LeaseNextRuntime(ctx context.Context, deviceID string) (Runtime, bool, error) {
	if !deviceIDPattern.MatchString(deviceID) {
		return Runtime{}, false, ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Runtime{}, false, errors.New("model runtime lease failed")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET state='expired',status='failed',updated_at=? WHERE state NOT IN ('completed','failed','cancelled','expired') AND expires_at<=?`, now.UnixNano(), now.UnixNano()); err != nil {
		return Runtime{}, false, errors.New("model runtime expiry failed")
	}
	var runtimeID string
	if err := tx.QueryRowContext(ctx, `SELECT runtime_id FROM model_runtimes WHERE device_id=? AND controller='remote_edge' AND state='awaiting_edge' AND expires_at>? ORDER BY created_at,runtime_id LIMIT 1`, deviceID, now.UnixNano()).Scan(&runtimeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Runtime{}, false, nil
		}
		return Runtime{}, false, errors.New("model runtime lease failed")
	}
	result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET state='starting',status='running',last_heartbeat=?,updated_at=? WHERE runtime_id=? AND device_id=? AND state='awaiting_edge'`, now.UnixNano(), now.UnixNano(), runtimeID, deviceID)
	if err != nil {
		return Runtime{}, false, errors.New("model runtime lease failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Runtime{}, false, ErrTurnConflict
	}
	if err := tx.Commit(); err != nil {
		return Runtime{}, false, errors.New("model runtime lease commit failed")
	}
	runtime, err := s.runtimeLocked(ctx, runtimeID)
	return runtime, err == nil, err
}

func (s *Store) SetRuntimeResult(ctx context.Context, runtimeID, deviceID, resultRef string) error {
	if !safeIdentifier.MatchString(runtimeID) || !deviceIDPattern.MatchString(deviceID) || (resultRef != "" && !resultReferencePattern.MatchString(resultRef)) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_runtimes SET result_ref=?,updated_at=? WHERE runtime_id=? AND device_id=? AND (result_ref='' OR result_ref=?)`, resultRef, s.now().UTC().UnixNano(), runtimeID, deviceID, resultRef)
	if err != nil {
		return errors.New("model runtime result update failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	return nil
}

func (s *Store) RuntimeGoal(ctx context.Context, runtimeID, deviceID string) ([]byte, string, error) {
	if !safeIdentifier.MatchString(runtimeID) || !deviceIDPattern.MatchString(deviceID) {
		return nil, "", ErrInvalidRequest
	}
	var ref string
	var expires int64
	if err := s.db.QueryRowContext(ctx, `SELECT goal_ref,expires_at FROM model_runtimes WHERE runtime_id=? AND device_id=?`, runtimeID, deviceID).Scan(&ref, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrTurnNotFound
		}
		return nil, "", errors.New("model runtime goal lookup failed")
	}
	if ref == "" || expires <= s.now().UTC().UnixNano() {
		return nil, "", ErrTurnConflict
	}
	var content []byte
	var digest string
	if err := s.db.QueryRowContext(ctx, `SELECT content,content_digest FROM runtime_bodies WHERE body_ref=? AND expires_at>?`, ref, s.now().UTC().UnixNano()).Scan(&content, &digest); err != nil {
		return nil, "", errors.New("model runtime goal unavailable")
	}
	return content, digest, nil
}

func (s *Store) CancelRuntime(ctx context.Context, runtimeID string) error {
	if !safeIdentifier.MatchString(runtimeID) {
		return ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("model runtime cancellation failed")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET status='cancelled',state='cancelled',updated_at=? WHERE runtime_id=? AND state NOT IN ('completed','failed','cancelled','expired')`, now.UnixNano(), runtimeID)
	if err != nil {
		return errors.New("model runtime cancellation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='cancelled' WHERE runtime_id=? AND status IN ('created','awaiting_model','disconnected')`, runtimeID); err != nil {
		return errors.New("model runtime turn cancellation failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("model runtime cancellation commit failed")
	}
	s.signal()
	return nil
}

func (s *Store) Runtime(ctx context.Context, runtimeID string) (Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Runtime{}, ErrInvalidRequest
	}
	return s.runtimeLocked(ctx, runtimeID)
}

func (s *Store) RuntimeForDevice(ctx context.Context, runtimeID, deviceID string) (Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) || !deviceIDPattern.MatchString(deviceID) {
		return Runtime{}, ErrInvalidRequest
	}
	runtime, err := s.Runtime(ctx, runtimeID)
	if err != nil {
		return Runtime{}, err
	}
	if runtime.DeviceID != deviceID {
		return Runtime{}, ErrTurnNotFound
	}
	return runtime, nil
}

func (s *Store) runtimeLocked(ctx context.Context, runtimeID string) (Runtime, error) {
	var runtime Runtime
	var createdAt, updatedAt, expiresAt, lastHeartbeat int64
	err := s.db.QueryRowContext(ctx, `SELECT runtime_id,device_id,workspace_id,controller,state,status,goal_summary,goal_ref,goal_digest,idempotency_key_digest,created_at,expires_at,last_heartbeat,last_sequence,active_turn_id,result_ref,updated_at FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(
		&runtime.RuntimeID, &runtime.DeviceID, &runtime.WorkspaceID, &runtime.Controller, &runtime.State, &runtime.Status, &runtime.GoalSummary, &runtime.goalRef, &runtime.goalDigest, &runtime.idempotencyKeyDigest, &createdAt, &expiresAt, &lastHeartbeat, &runtime.LastSequence, &runtime.ActiveTurnID, &runtime.ResultRef, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, ErrTurnNotFound
	}
	if err != nil {
		return Runtime{}, errors.New("model runtime read failed")
	}
	runtime.CreatedAt = time.Unix(0, createdAt).UTC()
	runtime.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if expiresAt > 0 {
		runtime.ExpiresAt = time.Unix(0, expiresAt).UTC()
	}
	if lastHeartbeat > 0 {
		runtime.LastHeartbeat = time.Unix(0, lastHeartbeat).UTC()
	}
	var turnID sql.NullString
	var status sql.NullString
	var sequence sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT turn_id,status,sequence FROM model_turns WHERE runtime_id=? ORDER BY sequence DESC LIMIT 1`, runtimeID).Scan(&turnID, &status, &sequence)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, errors.New("model runtime turn read failed")
	}
	if turnID.Valid {
		runtime.ActiveTurnID = TurnID(turnID.String)
		runtime.ActiveTurnStatus = Status(status.String)
		runtime.LastSequence = uint64(sequence.Int64)
	}
	return runtime, nil
}

func (s *Store) Poll(ctx context.Context, runtimeID string) (Offer, bool, error) {
	offer, err := s.nextOnce(ctx, runtimeID)
	if errors.Is(err, ErrTurnNotFound) {
		return Offer{}, false, nil
	}
	return offer, err == nil, err
}

func (s *Store) PollAfter(ctx context.Context, runtimeID string, afterSequence uint64) (Offer, bool, Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Offer{}, false, Runtime{}, ErrInvalidRequest
	}
	offer, err := s.nextOnce(ctx, runtimeID)
	if err == nil && offer.Sequence > afterSequence {
		runtime, runtimeErr := s.Runtime(ctx, runtimeID)
		return offer, true, runtime, runtimeErr
	}
	if err != nil && !errors.Is(err, ErrTurnNotFound) {
		return Offer{}, false, Runtime{}, err
	}
	runtime, err := s.Runtime(ctx, runtimeID)
	if err != nil {
		return Offer{}, false, Runtime{}, err
	}
	return Offer{}, false, runtime, nil
}

func (s *Store) WaitNextAfter(ctx context.Context, runtimeID string, afterSequence uint64) (Offer, bool, Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Offer{}, false, Runtime{}, ErrInvalidRequest
	}
	var lastRuntime Runtime
	for {
		if err := ctx.Err(); err != nil {
			return Offer{}, false, lastRuntime, err
		}
		wake := s.waitChannel()
		offer, pending, runtime, err := s.PollAfter(ctx, runtimeID, afterSequence)
		if runtime.RuntimeID != "" {
			lastRuntime = runtime
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Offer{}, false, runtime, ctxErr
			}
			return Offer{}, false, runtime, err
		}
		if pending || runtimeStopsWait(runtime, afterSequence) {
			return offer, pending, runtime, nil
		}
		select {
		case <-ctx.Done():
			return Offer{}, false, runtime, ctx.Err()
		case <-wake:
		case <-time.After(time.Second):
		}
	}
}

func runtimeStopsWait(runtime Runtime, afterSequence uint64) bool {
	switch runtime.State {
	case RuntimeStateCompleted, RuntimeStateCancelled, RuntimeStateFailed, RuntimeStateExpired:
		return true
	}
	switch runtime.Status {
	case RuntimeCompleted, RuntimeCancelled, RuntimeFailed:
		return true
	}
	if runtime.LastSequence > afterSequence {
		return false
	}
	switch runtime.ActiveTurnStatus {
	case StatusDisconnected, StatusCancelled, StatusExpired, StatusFailed:
		return true
	default:
		return false
	}
}

func validRuntimeState(state RuntimeState) bool {
	switch state {
	case RuntimeStateRequested, RuntimeStateAwaitingEdge, RuntimeStateStarting, RuntimeStateAwaitingModel, RuntimeStateExecutingTools, RuntimeStateCompleted, RuntimeStateFailed, RuntimeStateCancelled, RuntimeStateDisconnected, RuntimeStateExpired:
		return true
	default:
		return false
	}
}

func legacyStatusForState(state RuntimeState) RuntimeStatus {
	switch state {
	case RuntimeStateCompleted:
		return RuntimeCompleted
	case RuntimeStateCancelled:
		return RuntimeCancelled
	case RuntimeStateFailed, RuntimeStateExpired:
		return RuntimeFailed
	case RuntimeStateStarting, RuntimeStateAwaitingModel, RuntimeStateExecutingTools, RuntimeStateDisconnected:
		return RuntimeRunning
	default:
		return RuntimeReady
	}
}
