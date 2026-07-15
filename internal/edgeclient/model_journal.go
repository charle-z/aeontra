package edgeclient

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"

	_ "modernc.org/sqlite"
)

const (
	modelRelayJournalFile  = "model-relay.db"
	modelRelayJournalQuota = int64(16 << 20)
)

var (
	localRequestRefPattern = regexp.MustCompile(`^lr_[a-f0-9]{32}$`)
	remoteLeaseIDPattern   = regexp.MustCompile(`^el_[a-f0-9]{32}$`)
	remoteCreateIDPattern  = regexp.MustCompile(`^ec_[a-f0-9]{32}$`)
	remoteWaitIDPattern    = regexp.MustCompile(`^ew_[a-f0-9]{32}$`)
	remoteRuntimeIDPattern = regexp.MustCompile(`^mr_[a-f0-9]{32}$`)
	remoteTurnIDPattern    = regexp.MustCompile(`^mt_[a-f0-9]{32}$`)
	remoteBodyRefPattern   = regexp.MustCompile(`^mb_[a-f0-9]{32}$`)
)

type modelJournal struct {
	db  *sql.DB
	now func() time.Time
}

type modelJournalTurn struct {
	RuntimeID     string
	Sequence      uint64
	RequestDigest string
	CreateID      string
	TurnID        modelturn.TurnID
	RequestRef    string
	WaitID        string
	State         string
}

func openModelJournal(stateRoot string) (*modelJournal, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Clean(stateRoot), modelRelayJournalFile)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("model relay journal is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("model relay journal is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("model relay journal is unavailable")
	}
	db.SetMaxOpenConns(1)
	journal := &modelJournal{db: db, now: time.Now}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=8192`,
		`CREATE TABLE IF NOT EXISTS staged_model_bodies (
			local_ref TEXT PRIMARY KEY,
			request_digest TEXT NOT NULL,
			content BLOB NOT NULL,
			content_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS remote_model_turns (
			runtime_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			request_digest TEXT NOT NULL,
			create_id TEXT NOT NULL UNIQUE,
			turn_id TEXT NOT NULL DEFAULT '',
			request_ref TEXT NOT NULL DEFAULT '',
			wait_id TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(runtime_id,sequence)
		) WITHOUT ROWID`,
		`CREATE UNIQUE INDEX IF NOT EXISTS remote_model_turn_id ON remote_model_turns(turn_id) WHERE turn_id<>''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS remote_model_wait_id ON remote_model_turns(wait_id) WHERE wait_id<>''`,
		`CREATE TRIGGER IF NOT EXISTS staged_model_bodies_immutable BEFORE UPDATE OF local_ref,request_digest,content,content_bytes,created_at,expires_at ON staged_model_bodies
		BEGIN SELECT RAISE(ABORT, 'staged model body is immutable'); END`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("model relay journal initialization failed")
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("model relay journal permissions failed")
	}
	return journal, nil
}

func (j *modelJournal) stageBody(ctx context.Context, payload json.RawMessage, digest string, ttl time.Duration) (modelturn.RequestBodyReference, error) {
	if j == nil || j.db == nil || ttl <= 0 || ttl > modelturn.MaxTurnTTL || int64(len(payload)) <= modelturn.MaxInlineRequestBytes || int64(len(payload)) > modelturn.MaxRequestBodyBytes {
		return modelturn.RequestBodyReference{}, modelturn.ErrInvalidRequest
	}
	actual, err := modelturn.ExactPayloadDigest(payload)
	if err != nil || actual != digest {
		return modelturn.RequestBodyReference{}, modelturn.ErrSequenceMismatch
	}
	ref, err := randomModelJournalID("lr_")
	if err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body id generation failed")
	}
	now := j.now().UTC()
	expires := now.Add(ttl)
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body transaction failed")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM staged_model_bodies WHERE expires_at<=?`, now.UnixNano()); err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body cleanup failed")
	}
	var used int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(content_bytes),0) FROM staged_model_bodies`).Scan(&used); err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body quota check failed")
	}
	if used+int64(len(payload)) > modelRelayJournalQuota {
		return modelturn.RequestBodyReference{}, modelturn.ErrTurnQuotaExceeded
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO staged_model_bodies(local_ref,request_digest,content,content_bytes,created_at,expires_at) VALUES(?,?,?,?,?,?)`, ref, digest, []byte(payload), len(payload), now.UnixNano(), expires.UnixNano()); err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return modelturn.RequestBodyReference{}, errors.New("model body commit failed")
	}
	return modelturn.RequestBodyReference{RequestRef: ref, RequestDigest: digest, ContentBytes: int64(len(payload)), ExpiresAt: expires}, nil
}

func (j *modelJournal) loadBody(ctx context.Context, ref, digest string) (json.RawMessage, error) {
	if j == nil || j.db == nil || !localRequestRefPattern.MatchString(ref) || !strings.HasPrefix(digest, "sha256:") {
		return nil, modelturn.ErrInvalidRequest
	}
	var content []byte
	var storedDigest string
	var contentBytes int64
	if err := j.db.QueryRowContext(ctx, `SELECT request_digest,content,content_bytes FROM staged_model_bodies WHERE local_ref=? AND expires_at>?`, ref, j.now().UTC().UnixNano()).Scan(&storedDigest, &content, &contentBytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, modelturn.ErrRequestRefConflict
		}
		return nil, errors.New("model body read failed")
	}
	if storedDigest != digest || contentBytes != int64(len(content)) {
		return nil, modelturn.ErrRequestRefConflict
	}
	actual, err := modelturn.ExactPayloadDigest(json.RawMessage(content))
	if err != nil || actual != digest {
		return nil, modelturn.ErrRequestRefConflict
	}
	return json.RawMessage(content), nil
}

func (j *modelJournal) deleteBody(ctx context.Context, ref string) {
	if j == nil || j.db == nil || !localRequestRefPattern.MatchString(ref) {
		return
	}
	_, _ = j.db.ExecContext(ctx, `DELETE FROM staged_model_bodies WHERE local_ref=?`, ref)
}

func (j *modelJournal) beginTurn(ctx context.Context, runtimeID string, sequence uint64, digest string) (modelJournalTurn, bool, error) {
	if j == nil || j.db == nil || !remoteRuntimeIDPattern.MatchString(runtimeID) || sequence == 0 || !strings.HasPrefix(digest, "sha256:") {
		return modelJournalTurn{}, false, modelturn.ErrInvalidRequest
	}
	createID, err := randomModelJournalID("ec_")
	if err != nil {
		return modelJournalTurn{}, false, errors.New("model create id generation failed")
	}
	now := j.now().UTC()
	result, err := j.db.ExecContext(ctx, `INSERT OR IGNORE INTO remote_model_turns(runtime_id,sequence,request_digest,create_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, runtimeID, sequence, digest, createID, "creating", now.UnixNano(), now.UnixNano())
	if err != nil {
		return modelJournalTurn{}, false, errors.New("model turn journal unavailable")
	}
	rows, _ := result.RowsAffected()
	entry, err := j.turnBySequence(ctx, runtimeID, sequence)
	if err != nil {
		return modelJournalTurn{}, false, err
	}
	if entry.RequestDigest != digest {
		return modelJournalTurn{}, false, modelturn.ErrSequenceMismatch
	}
	return entry, rows == 1, nil
}

func (j *modelJournal) bindTurn(ctx context.Context, runtimeID string, sequence uint64, digest string, turn modelturn.Turn) error {
	if turn.RuntimeID != runtimeID || turn.Sequence != sequence || turn.RequestDigest != digest || !remoteTurnIDPattern.MatchString(string(turn.ID)) || !remoteBodyRefPattern.MatchString(turn.RequestRef) {
		return modelturn.ErrSequenceMismatch
	}
	now := j.now().UTC()
	result, err := j.db.ExecContext(ctx, `UPDATE remote_model_turns SET turn_id=?,request_ref=?,state='awaiting_response',updated_at=? WHERE runtime_id=? AND sequence=? AND request_digest=? AND (turn_id='' OR (turn_id=? AND request_ref=?))`, turn.ID, turn.RequestRef, now.UnixNano(), runtimeID, sequence, digest, turn.ID, turn.RequestRef)
	if err != nil {
		return errors.New("model turn journal update failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return modelturn.ErrTurnConflict
	}
	return nil
}

func (j *modelJournal) turnBySequence(ctx context.Context, runtimeID string, sequence uint64) (modelJournalTurn, error) {
	var entry modelJournalTurn
	var turnID string
	if err := j.db.QueryRowContext(ctx, `SELECT runtime_id,sequence,request_digest,create_id,turn_id,request_ref,wait_id,state FROM remote_model_turns WHERE runtime_id=? AND sequence=?`, runtimeID, sequence).Scan(&entry.RuntimeID, &entry.Sequence, &entry.RequestDigest, &entry.CreateID, &turnID, &entry.RequestRef, &entry.WaitID, &entry.State); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return modelJournalTurn{}, modelturn.ErrTurnNotFound
		}
		return modelJournalTurn{}, errors.New("model turn journal read failed")
	}
	entry.TurnID = modelturn.TurnID(turnID)
	return entry, nil
}

func (j *modelJournal) turnByID(ctx context.Context, turnID modelturn.TurnID) (modelJournalTurn, error) {
	if !remoteTurnIDPattern.MatchString(string(turnID)) {
		return modelJournalTurn{}, modelturn.ErrInvalidRequest
	}
	var entry modelJournalTurn
	var storedTurnID string
	if err := j.db.QueryRowContext(ctx, `SELECT runtime_id,sequence,request_digest,create_id,turn_id,request_ref,wait_id,state FROM remote_model_turns WHERE turn_id=?`, turnID).Scan(&entry.RuntimeID, &entry.Sequence, &entry.RequestDigest, &entry.CreateID, &storedTurnID, &entry.RequestRef, &entry.WaitID, &entry.State); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return modelJournalTurn{}, modelturn.ErrTurnNotFound
		}
		return modelJournalTurn{}, errors.New("model turn journal read failed")
	}
	entry.TurnID = modelturn.TurnID(storedTurnID)
	return entry, nil
}

func (j *modelJournal) ensureWaitID(ctx context.Context, turnID modelturn.TurnID) (modelJournalTurn, error) {
	entry, err := j.turnByID(ctx, turnID)
	if err != nil {
		return modelJournalTurn{}, err
	}
	if entry.WaitID != "" {
		if !remoteWaitIDPattern.MatchString(entry.WaitID) {
			return modelJournalTurn{}, errors.New("model wait journal is invalid")
		}
		return entry, nil
	}
	waitID, err := randomModelJournalID("ew_")
	if err != nil {
		return modelJournalTurn{}, errors.New("model wait id generation failed")
	}
	result, err := j.db.ExecContext(ctx, `UPDATE remote_model_turns SET wait_id=?,updated_at=? WHERE turn_id=? AND wait_id=''`, waitID, j.now().UTC().UnixNano(), turnID)
	if err != nil {
		return modelJournalTurn{}, errors.New("model wait journal update failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return j.turnByID(ctx, turnID)
	}
	return j.turnByID(ctx, turnID)
}

func (j *modelJournal) markConsumed(ctx context.Context, turnID modelturn.TurnID) error {
	result, err := j.db.ExecContext(ctx, `UPDATE remote_model_turns SET state='consumed',updated_at=? WHERE turn_id=? AND state IN ('awaiting_response','consumed')`, j.now().UTC().UnixNano(), turnID)
	if err != nil {
		return errors.New("model turn journal update failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return modelturn.ErrTurnConflict
	}
	return nil
}

func (j *modelJournal) markCancelled(ctx context.Context, turnID modelturn.TurnID) error {
	result, err := j.db.ExecContext(ctx, `UPDATE remote_model_turns SET state='cancelled',updated_at=? WHERE turn_id=? AND state NOT IN ('consumed','cancelled')`, j.now().UTC().UnixNano(), turnID)
	if err != nil {
		return errors.New("model turn journal update failed")
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		entry, readErr := j.turnByID(ctx, turnID)
		if readErr == nil && entry.State == "cancelled" {
			return nil
		}
		return modelturn.ErrTurnConflict
	}
	return nil
}

func (j *modelJournal) stats(ctx context.Context) (modelturn.StoreStats, error) {
	var stats modelturn.StoreStats
	if err := j.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT runtime_id),COUNT(*),COALESCE(SUM(CASE WHEN state='awaiting_response' THEN 1 ELSE 0 END),0),0,COALESCE(SUM(CASE WHEN state='consumed' THEN 1 ELSE 0 END),0) FROM remote_model_turns`).Scan(&stats.RuntimeCount, &stats.TurnCount, &stats.AwaitingCount, &stats.RespondedCount, &stats.ConsumedCount); err != nil {
		return modelturn.StoreStats{}, errors.New("model relay statistics failed")
	}
	if err := j.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(content_bytes),0) FROM staged_model_bodies`).Scan(&stats.BodyBytes); err != nil {
		return modelturn.StoreStats{}, errors.New("model relay statistics failed")
	}
	return stats, nil
}

func (j *modelJournal) close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

func randomModelJournalID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}
