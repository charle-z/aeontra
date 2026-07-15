package modelturn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultTurnTTL        = 15 * time.Minute
	MaxTurnTTL            = time.Hour
	DefaultTurnQuotaBytes = int64(64 << 20)
	MaxTurnQuotaBytes     = int64(256 << 20)
	MaxInlineRequestBytes = int64(64 << 10)
	MaxRequestBodyBytes   = int64(4 << 20)
	turnDatabaseFilename  = "model-turns.db"
)

type Status string

const (
	StatusCreated       Status = "created"
	StatusAwaitingModel Status = "awaiting_model"
	StatusResponded     Status = "responded"
	StatusConsumed      Status = "consumed"
	StatusCancelled     Status = "cancelled"
	StatusExpired       Status = "expired"
	StatusDisconnected  Status = "disconnected"
	StatusFailed        Status = "failed"
)

var (
	ErrInvalidRequest     = errors.New("model turn request is invalid")
	ErrSequenceMismatch   = errors.New("model turn sequence compare-and-swap failed")
	ErrTurnNotFound       = errors.New("model turn not found")
	ErrTurnConflict       = errors.New("model turn state conflict")
	ErrResponseReplay     = errors.New("model turn response replay rejected")
	ErrLateResponse       = errors.New("late model turn response rejected")
	ErrToolNotOffered     = errors.New("model response referenced a tool that was not offered")
	ErrTurnQuotaExceeded  = errors.New("model turn result-store quota exceeded")
	ErrBodyTooLarge       = errors.New("model turn body exceeds the configured limit")
	ErrRequestRefConflict = errors.New("model request reference is invalid, expired, changed, or already bound")
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type StoreConfig struct {
	Root       string
	QuotaBytes int64
	DefaultTTL time.Duration
	Now        func() time.Time
}

type Record struct {
	RuntimeID      string     `json:"runtime_id"`
	TurnID         TurnID     `json:"turn_id"`
	Sequence       uint64     `json:"sequence"`
	RequestDigest  string     `json:"request_digest"`
	RequestRef     string     `json:"request_ref"`
	ResponseDigest string     `json:"response_digest"`
	ResponseRef    string     `json:"response_ref"`
	Status         Status     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	RespondedAt    *time.Time `json:"responded_at"`
	ConsumedAt     *time.Time `json:"consumed_at"`
}

type Offer struct {
	Record
	RequestPayload json.RawMessage `json:"request_payload"`
	OfferedToolIDs []string        `json:"offered_tool_ids"`
}

type RequestBodyReference struct {
	RequestRef    string    `json:"request_ref"`
	RequestDigest string    `json:"request_digest"`
	ContentBytes  int64     `json:"content_bytes"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ResponseSubmission struct {
	RuntimeID        string
	TurnID           TurnID
	ExpectedSequence uint64
	RequestDigest    string
	Payload          json.RawMessage
	UsedToolIDs      []string
}

type Store struct {
	mu         sync.Mutex
	db         *sql.DB
	root       string
	quotaBytes int64
	defaultTTL time.Duration
	now        func() time.Time
	wake       chan struct{}
}

func OpenStore(cfg StoreConfig) (*Store, error) {
	root := filepath.Clean(strings.TrimSpace(cfg.Root))
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("model turn root must be absolute")
	}
	quota := cfg.QuotaBytes
	if quota == 0 {
		quota = DefaultTurnQuotaBytes
	}
	if quota < 1024 || quota > MaxTurnQuotaBytes {
		return nil, errors.New("model turn quota is invalid")
	}
	defaultTTL := cfg.DefaultTTL
	if defaultTTL == 0 {
		defaultTTL = DefaultTurnTTL
	}
	if defaultTTL <= 0 || defaultTTL > MaxTurnTTL {
		return nil, errors.New("model turn TTL is invalid")
	}
	if err := prepareTurnRoot(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, turnDatabaseFilename)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("model turn database is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("model turn database is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("model turn database is unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, root: root, quotaBytes: quota, defaultTTL: defaultTTL, now: cfg.Now, wake: make(chan struct{}, 1)}
	if store.now == nil {
		store.now = time.Now
	}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("model turn database permissions could not be secured")
	}
	if err := store.Cleanup(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize() error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS turn_bodies (
			body_ref TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK(kind IN ('request','response')),
			content BLOB NOT NULL,
			content_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS model_runtimes (
			runtime_id TEXT PRIMARY KEY,
			status TEXT NOT NULL CHECK(status IN ('ready','running','completed','cancelled','failed')),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS model_runtimes_status ON model_runtimes(status,updated_at)`,
		`CREATE INDEX IF NOT EXISTS turn_bodies_expiry ON turn_bodies(expires_at)`,
		`CREATE TABLE IF NOT EXISTS model_turns (
			turn_id TEXT PRIMARY KEY,
			runtime_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			request_digest TEXT NOT NULL,
			request_ref TEXT NOT NULL,
			response_digest TEXT NOT NULL DEFAULT '',
			response_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK(status IN ('created','awaiting_model','responded','consumed','cancelled','expired','disconnected','failed')),
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			responded_at INTEGER,
			consumed_at INTEGER,
			offered_tools_json TEXT NOT NULL,
			UNIQUE(runtime_id, sequence),
			FOREIGN KEY(request_ref) REFERENCES turn_bodies(body_ref)
		)`,
		`CREATE INDEX IF NOT EXISTS model_turns_runtime_status ON model_turns(runtime_id,status,sequence)`,
		`CREATE INDEX IF NOT EXISTS model_turns_expiry ON model_turns(expires_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS model_turns_request_ref_unique ON model_turns(request_ref)`,
		`CREATE TRIGGER IF NOT EXISTS turn_bodies_immutable BEFORE UPDATE OF kind,content,content_bytes,created_at,expires_at ON turn_bodies
		BEGIN
			SELECT RAISE(ABORT, 'turn body is immutable');
		END`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("model turn database initialization failed: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateTurn(ctx context.Context, request ModelRequest) (Turn, error) {
	if !safeIdentifier.MatchString(request.RuntimeID) || request.Sequence == 0 {
		return Turn{}, ErrInvalidRequest
	}
	var payload []byte
	var err error
	if request.CanonicalPayload {
		payload, err = exactJSON(request.Payload)
	} else {
		payload, err = canonicalJSON(request.Payload)
	}
	if err != nil {
		return Turn{}, ErrInvalidRequest
	}
	if int64(len(payload)) > MaxInlineRequestBytes {
		return Turn{}, ErrBodyTooLarge
	}
	offered, err := normalizeOfferedTools(request.OfferedTools)
	if err != nil {
		return Turn{}, err
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl <= 0 || ttl > MaxTurnTTL {
		return Turn{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	expires := now.Add(ttl)
	turnID, err := newOpaqueID("mt_")
	if err != nil {
		return Turn{}, errors.New("model turn id generation failed")
	}
	requestRef, err := newOpaqueID("mb_")
	if err != nil {
		return Turn{}, errors.New("model body id generation failed")
	}
	digest := digestBytes(payload)
	offeredJSON, _ := json.Marshal(offered)

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Turn{}, errors.New("model turn transaction failed")
	}
	defer tx.Rollback()
	if err := s.cleanupLocked(ctx, tx, now); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO model_runtimes(runtime_id,status,created_at,updated_at) VALUES(?,'running',?,?)`, request.RuntimeID, now.UnixNano(), now.UnixNano()); err != nil {
		return Turn{}, errors.New("model runtime persistence failed")
	}
	result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET status='running',updated_at=? WHERE runtime_id=? AND status IN ('ready','running')`, now.UnixNano(), request.RuntimeID)
	if err != nil {
		return Turn{}, errors.New("model runtime activation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Turn{}, ErrTurnConflict
	}
	var maxSequence sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM model_turns WHERE runtime_id=?`, request.RuntimeID).Scan(&maxSequence); err != nil {
		return Turn{}, errors.New("model turn sequence read failed")
	}
	expected := uint64(1)
	if maxSequence.Valid {
		expected = uint64(maxSequence.Int64) + 1
	}
	if request.Sequence != expected {
		return Turn{}, ErrSequenceMismatch
	}
	if err := s.makeRoomLocked(ctx, tx, int64(len(payload))); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO turn_bodies(body_ref,kind,content,content_bytes,created_at,expires_at) VALUES(?,?,?,?,?,?)`, requestRef, "request", payload, len(payload), now.UnixNano(), expires.UnixNano()); err != nil {
		return Turn{}, errors.New("model request persistence failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO model_turns(turn_id,runtime_id,sequence,request_digest,request_ref,status,created_at,expires_at,offered_tools_json) VALUES(?,?,?,?,?,'created',?,?,?)`, turnID, request.RuntimeID, request.Sequence, digest, requestRef, now.UnixNano(), expires.UnixNano(), string(offeredJSON)); err != nil {
		return Turn{}, errors.New("model turn persistence failed")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='awaiting_model' WHERE turn_id=? AND status='created'`, turnID); err != nil {
		return Turn{}, errors.New("model turn activation failed")
	}
	if err := tx.Commit(); err != nil {
		return Turn{}, errors.New("model turn commit failed")
	}
	s.signal()
	return Turn{RuntimeID: request.RuntimeID, ID: TurnID(turnID), Sequence: request.Sequence, RequestDigest: digest, OfferedToolIDs: offered, CreatedAt: now, ExpiresAt: expires}, nil
}

func (s *Store) Next(ctx context.Context, runtimeID string) (Offer, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Offer{}, ErrInvalidRequest
	}
	for {
		offer, err := s.nextOnce(ctx, runtimeID)
		if err == nil || !errors.Is(err, ErrTurnNotFound) {
			return offer, err
		}
		select {
		case <-ctx.Done():
			return Offer{}, ctx.Err()
		case <-s.wake:
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Store) nextOnce(ctx context.Context, runtimeID string) (Offer, error) {
	now := s.now().UTC()
	if err := s.Cleanup(ctx); err != nil {
		return Offer{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json
		FROM model_turns WHERE runtime_id=? AND status IN ('awaiting_model','disconnected') ORDER BY sequence ASC LIMIT 1`, runtimeID)
	record, offeredJSON, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Offer{}, ErrTurnNotFound
	}
	if err != nil {
		return Offer{}, errors.New("model turn read failed")
	}
	if !now.Before(record.ExpiresAt) {
		return Offer{}, ErrLateResponse
	}
	payload, err := s.readBody(ctx, record.RequestRef)
	if err != nil {
		return Offer{}, err
	}
	var offered []string
	if err := json.Unmarshal([]byte(offeredJSON), &offered); err != nil {
		return Offer{}, errors.New("offered tool metadata is invalid")
	}
	return Offer{Record: record, RequestPayload: payload, OfferedToolIDs: offered}, nil
}

func (s *Store) Respond(ctx context.Context, submission ResponseSubmission) (Record, error) {
	if !safeIdentifier.MatchString(submission.RuntimeID) || submission.TurnID == "" || submission.ExpectedSequence == 0 || submission.RequestDigest == "" {
		return Record{}, ErrInvalidRequest
	}
	payload, err := canonicalJSON(submission.Payload)
	if err != nil {
		return Record{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, errors.New("model response transaction failed")
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json FROM model_turns WHERE turn_id=?`, submission.TurnID)
	record, offeredJSON, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrTurnNotFound
	}
	if err != nil {
		return Record{}, errors.New("model turn read failed")
	}
	if record.Status == StatusResponded || record.Status == StatusConsumed {
		return Record{}, ErrResponseReplay
	}
	if record.RuntimeID != submission.RuntimeID || record.Sequence != submission.ExpectedSequence || record.RequestDigest != submission.RequestDigest {
		return Record{}, ErrSequenceMismatch
	}
	if !now.Before(record.ExpiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE model_turns SET status='expired' WHERE turn_id=? AND status IN ('created','awaiting_model','disconnected')`, submission.TurnID)
		_ = tx.Commit()
		return Record{}, ErrLateResponse
	}
	if record.Status != StatusAwaitingModel && record.Status != StatusDisconnected {
		return Record{}, ErrTurnConflict
	}
	var offered []string
	if err := json.Unmarshal([]byte(offeredJSON), &offered); err != nil {
		return Record{}, errors.New("offered tool metadata is invalid")
	}
	if !toolSubset(submission.UsedToolIDs, offered) {
		return Record{}, ErrToolNotOffered
	}
	if err := s.makeRoomLocked(ctx, tx, int64(len(payload))); err != nil {
		return Record{}, err
	}
	responseRef, err := newOpaqueID("mb_")
	if err != nil {
		return Record{}, errors.New("model body id generation failed")
	}
	responseDigest := digestBytes(payload)
	if _, err := tx.ExecContext(ctx, `INSERT INTO turn_bodies(body_ref,kind,content,content_bytes,created_at,expires_at) VALUES(?,?,?,?,?,?)`, responseRef, "response", payload, len(payload), now.UnixNano(), record.ExpiresAt.UnixNano()); err != nil {
		return Record{}, errors.New("model response persistence failed")
	}
	result, err := tx.ExecContext(ctx, `UPDATE model_turns SET response_digest=?,response_ref=?,status='responded',responded_at=? WHERE turn_id=? AND runtime_id=? AND sequence=? AND request_digest=? AND status IN ('awaiting_model','disconnected')`, responseDigest, responseRef, now.UnixNano(), submission.TurnID, submission.RuntimeID, submission.ExpectedSequence, submission.RequestDigest)
	if err != nil {
		return Record{}, errors.New("model response compare-and-swap failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Record{}, ErrTurnConflict
	}
	if err := tx.Commit(); err != nil {
		return Record{}, errors.New("model response commit failed")
	}
	record.ResponseDigest = responseDigest
	record.ResponseRef = responseRef
	record.Status = StatusResponded
	record.RespondedAt = pointerTime(now)
	s.signal()
	return record, nil
}

func (s *Store) WaitResponse(ctx context.Context, turnID TurnID) (ModelResponse, error) {
	for {
		response, ready, err := s.consumeOnce(ctx, turnID)
		if err != nil || ready {
			return response, err
		}
		select {
		case <-ctx.Done():
			return ModelResponse{}, ctx.Err()
		case <-s.wake:
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Store) consumeOnce(ctx context.Context, turnID TurnID) (ModelResponse, bool, error) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelResponse{}, false, errors.New("model response transaction failed")
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json FROM model_turns WHERE turn_id=?`, turnID)
	record, _, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelResponse{}, false, ErrTurnNotFound
	}
	if err != nil {
		return ModelResponse{}, false, errors.New("model turn read failed")
	}
	switch record.Status {
	case StatusConsumed:
		return ModelResponse{}, false, ErrResponseReplay
	case StatusCancelled, StatusExpired, StatusFailed:
		return ModelResponse{}, false, ErrTurnConflict
	case StatusResponded:
		var payload []byte
		if err := tx.QueryRowContext(ctx, `SELECT content FROM turn_bodies WHERE body_ref=? AND expires_at>?`, record.ResponseRef, now.UnixNano()).Scan(&payload); err != nil {
			return ModelResponse{}, false, errors.New("model response body unavailable")
		}
		result, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='consumed',consumed_at=? WHERE turn_id=? AND status='responded'`, now.UnixNano(), turnID)
		if err != nil {
			return ModelResponse{}, false, errors.New("model response consume failed")
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			return ModelResponse{}, false, ErrResponseReplay
		}
		if err := tx.Commit(); err != nil {
			return ModelResponse{}, false, errors.New("model response commit failed")
		}
		return ModelResponse{RuntimeID: record.RuntimeID, TurnID: record.TurnID, Sequence: record.Sequence, RequestDigest: record.RequestDigest, Payload: payload}, true, nil
	default:
		if !now.Before(record.ExpiresAt) {
			_, _ = tx.ExecContext(ctx, `UPDATE model_turns SET status='expired' WHERE turn_id=? AND status IN ('created','awaiting_model','disconnected')`, turnID)
			_ = tx.Commit()
			return ModelResponse{}, false, ErrLateResponse
		}
		return ModelResponse{}, false, nil
	}
}

func (s *Store) Cancel(ctx context.Context, turnID TurnID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_turns SET status='cancelled' WHERE turn_id=? AND status IN ('created','awaiting_model','disconnected')`, turnID)
	if err != nil {
		return errors.New("model turn cancellation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	s.signal()
	return nil
}

func (s *Store) Fail(ctx context.Context, turnID TurnID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_turns SET status='failed' WHERE turn_id=? AND status IN ('created','awaiting_model','disconnected','responded')`, turnID)
	if err != nil {
		return errors.New("model turn failure transition failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	s.signal()
	return nil
}

func (s *Store) MarkDisconnected(ctx context.Context, turnID TurnID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_turns SET status='disconnected' WHERE turn_id=? AND status='awaiting_model'`, turnID)
	if err != nil {
		return errors.New("model turn disconnect failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	s.signal()
	return nil
}

func (s *Store) ResumeRuntime(ctx context.Context, runtimeID string) (int64, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return 0, ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE model_turns SET status='awaiting_model' WHERE runtime_id=? AND status='disconnected' AND expires_at>?`, runtimeID, s.now().UTC().UnixNano())
	if err != nil {
		return 0, errors.New("model runtime resume failed")
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		s.signal()
	}
	return rows, nil
}

func (s *Store) Get(ctx context.Context, turnID TurnID) (Record, error) {
	row := s.db.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json FROM model_turns WHERE turn_id=?`, turnID)
	record, _, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrTurnNotFound
	}
	if err != nil {
		return Record{}, errors.New("model turn read failed")
	}
	return record, nil
}

func (s *Store) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("model turn cleanup failed")
	}
	defer tx.Rollback()
	if err := s.cleanupLocked(ctx, tx, s.now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) cleanupLocked(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='expired' WHERE expires_at<=? AND status IN ('created','awaiting_model','disconnected')`, now.UnixNano()); err != nil {
		return errors.New("model turn expiry failed")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM turn_bodies WHERE expires_at<=?`, now.UnixNano()); err != nil {
		return errors.New("model body cleanup failed")
	}
	return nil
}

func (s *Store) makeRoomLocked(ctx context.Context, tx *sql.Tx, incoming int64) error {
	if incoming > s.quotaBytes {
		return ErrTurnQuotaExceeded
	}
	for {
		var used int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(content_bytes),0) FROM turn_bodies`).Scan(&used); err != nil {
			return errors.New("model turn quota check failed")
		}
		if used+incoming <= s.quotaBytes {
			return nil
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM turn_bodies WHERE body_ref=(
			SELECT b.body_ref FROM turn_bodies b JOIN model_turns t ON b.body_ref IN (t.request_ref,t.response_ref)
			WHERE t.status IN ('consumed','cancelled','expired','failed') ORDER BY b.created_at ASC LIMIT 1
		)`)
		if err != nil {
			return errors.New("model turn quota eviction failed")
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return ErrTurnQuotaExceeded
		}
	}
}

func (s *Store) readBody(ctx context.Context, ref string) (json.RawMessage, error) {
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT content FROM turn_bodies WHERE body_ref=? AND expires_at>?`, ref, s.now().UTC().UnixNano()).Scan(&payload); err != nil {
		return nil, errors.New("model body unavailable")
	}
	return payload, nil
}

func scanRecord(scanner interface{ Scan(...any) error }) (Record, string, error) {
	var record Record
	var turnID string
	var createdAt, expiresAt int64
	var respondedAt, consumedAt sql.NullInt64
	var offeredJSON string
	err := scanner.Scan(&turnID, &record.RuntimeID, &record.Sequence, &record.RequestDigest, &record.RequestRef, &record.ResponseDigest, &record.ResponseRef, &record.Status, &createdAt, &expiresAt, &respondedAt, &consumedAt, &offeredJSON)
	if err != nil {
		return Record{}, "", err
	}
	record.TurnID = TurnID(turnID)
	record.CreatedAt = time.Unix(0, createdAt).UTC()
	record.ExpiresAt = time.Unix(0, expiresAt).UTC()
	if respondedAt.Valid {
		value := time.Unix(0, respondedAt.Int64).UTC()
		record.RespondedAt = &value
	}
	if consumedAt.Valid {
		value := time.Unix(0, consumedAt.Int64).UTC()
		record.ConsumedAt = &value
	}
	return record, offeredJSON, nil
}

func normalizeOfferedTools(tools []ToolDefinition) ([]string, error) {
	seen := make(map[string]struct{}, len(tools))
	ids := make([]string, 0, len(tools))
	for _, tool := range tools {
		if !safeIdentifier.MatchString(tool.ID) || !safeIdentifier.MatchString(tool.Name) {
			return nil, ErrInvalidRequest
		}
		if _, err := canonicalJSON(tool.Schema); err != nil {
			return nil, ErrInvalidRequest
		}
		if _, exists := seen[tool.ID]; exists {
			return nil, ErrInvalidRequest
		}
		seen[tool.ID] = struct{}{}
		ids = append(ids, tool.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

func toolSubset(used, offered []string) bool {
	allowed := make(map[string]struct{}, len(offered))
	for _, id := range offered {
		allowed[id] = struct{}{}
	}
	for _, id := range used {
		if !safeIdentifier.MatchString(id) {
			return false
		}
		if _, ok := allowed[id]; !ok {
			return false
		}
	}
	return true
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return marshalCanonical(value)
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newOpaqueID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func pointerTime(value time.Time) *time.Time {
	return &value
}

func prepareTurnRoot(root string) error {
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			return errors.New("model turn root is not private")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("model turn root is unavailable")
	}
	if err := rejectTurnSymlinkAncestors(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New("model turn root is unavailable")
	}
	return nil
}

func rejectTurnSymlinkAncestors(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("model turn root ancestry is unsafe")
		}
	}
	return nil
}

func (s *Store) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

var _ ModelTurnTransport = (*Store)(nil)
