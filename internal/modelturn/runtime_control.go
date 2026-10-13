package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

var ErrRuntimeControlConflict = errors.New("model runtime controller generation or handoff conflict")
var controllerIDPattern = regexp.MustCompile(`^mc_[a-f0-9]{32}$`)

// RuntimeControl coordinates model responses, not host or worktree authority.
// A handoff keeps the same runtime and captured worker lease. Pending effects
// remain owned by that worker and must be reconciled before further work.
type RuntimeControl struct {
	RuntimeID          string `json:"runtime_id"`
	ControllerID       string `json:"controller_id"`
	Generation         uint64 `json:"generation"`
	Phase              string `json:"phase"`
	SuccessorID        string `json:"successor_id,omitempty"`
	TurnID             TurnID `json:"turn_id"`
	Sequence           uint64 `json:"sequence"`
	RequestDigest      string `json:"request_digest"`
	FormerControllerID string `json:"former_controller_id,omitempty"`
	FormerGeneration   uint64 `json:"former_generation,omitempty"`
	ReleasePending     bool   `json:"release_pending"`
}

type RuntimeControlRequest struct {
	RuntimeID        string `json:"runtime_id"`
	Action           string `json:"action"`
	ControllerID     string `json:"controller_id"`
	Generation       uint64 `json:"generation"`
	SuccessorID      string `json:"successor_id,omitempty"`
	TurnID           TurnID `json:"turn_id"`
	ExpectedSequence uint64 `json:"expected_sequence"`
	RequestDigest    string `json:"request_digest"`
}

func (s *Store) ensureControlSchema() error {
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("model controller migration unavailable")
	}
	defer tx.Rollback()
	rows, err := tx.Query(`PRAGMA table_info(model_turns)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, definition string }{{"response_controller_id", `TEXT NOT NULL DEFAULT ''`}, {"response_control_generation", `INTEGER NOT NULL DEFAULT 0`}} {
		if !columns[col.name] {
			if _, err := tx.Exec(`ALTER TABLE model_turns ADD COLUMN ` + col.name + ` ` + col.definition); err != nil {
				return err
			}
		}
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS runtime_controls (
		 runtime_id TEXT PRIMARY KEY,controller_id TEXT NOT NULL,generation INTEGER NOT NULL,
		 phase TEXT NOT NULL CHECK(phase IN ('owned','prepared','acknowledged')),
		 successor_id TEXT NOT NULL DEFAULT '',turn_id TEXT NOT NULL,sequence INTEGER NOT NULL,request_digest TEXT NOT NULL,
		 former_controller_id TEXT NOT NULL DEFAULT '',former_generation INTEGER NOT NULL DEFAULT 0,
		 release_pending INTEGER NOT NULL DEFAULT 0 CHECK(release_pending IN (0,1))) WITHOUT ROWID`,
		// Also fences writes from an older binary whose UPDATE does not know
		// the admission columns. Uncontrolled legacy runtimes are unchanged.
		`CREATE TRIGGER IF NOT EXISTS model_response_controller_fence BEFORE UPDATE OF response_ref ON model_turns
		 WHEN EXISTS (SELECT 1 FROM runtime_controls c WHERE c.runtime_id=NEW.runtime_id AND
		 (c.phase<>'owned' OR c.controller_id<>NEW.response_controller_id OR c.generation<>NEW.response_control_generation))
		 BEGIN SELECT RAISE(ABORT,'model controller fence rejected'); END`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return errors.New("model controller migration failed")
		}
	}
	return tx.Commit()
}

type controlReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readRuntimeControl(ctx context.Context, reader controlReader, runtimeID string) (*RuntimeControl, error) {
	c := &RuntimeControl{RuntimeID: runtimeID}
	err := reader.QueryRowContext(ctx, `SELECT controller_id,generation,phase,successor_id,turn_id,sequence,request_digest,former_controller_id,former_generation,release_pending FROM runtime_controls WHERE runtime_id=?`, runtimeID).Scan(&c.ControllerID, &c.Generation, &c.Phase, &c.SuccessorID, &c.TurnID, &c.Sequence, &c.RequestDigest, &c.FormerControllerID, &c.FormerGeneration, &c.ReleasePending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("model controller record unavailable")
	}
	if !controllerIDPattern.MatchString(c.ControllerID) || c.Generation == 0 || c.Generation > 1000000000 || c.TurnID == "" || c.Sequence == 0 || !goalDigestPattern.MatchString(c.RequestDigest) || (c.Phase != "owned" && c.Phase != "prepared" && c.Phase != "acknowledged") || (c.Phase == "owned" && c.SuccessorID != "") || (c.Phase != "owned" && (!controllerIDPattern.MatchString(c.SuccessorID) || c.SuccessorID == c.ControllerID)) || (c.FormerControllerID != "" && (!controllerIDPattern.MatchString(c.FormerControllerID) || c.FormerGeneration == 0 || c.FormerGeneration >= c.Generation)) || (c.FormerControllerID == "" && (c.FormerGeneration != 0 || c.ReleasePending)) {
		return nil, errors.New("model controller record invalid")
	}
	return c, nil
}

func (s *Store) RuntimeControl(ctx context.Context, runtimeID string) (*RuntimeControl, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return nil, ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readRuntimeControl(ctx, s.db, runtimeID)
}

// ControlRuntime is a bounded CAS protocol: claim, prepare, ACK, transfer,
// release. Abort restores the original controller at a new generation if a
// successor disappears before transfer. No expiry silently grants ownership.
func (s *Store) ControlRuntime(ctx context.Context, r RuntimeControlRequest) (RuntimeControl, error) {
	if !safeIdentifier.MatchString(r.RuntimeID) || !controllerIDPattern.MatchString(r.ControllerID) || r.Generation > 1000000000 || r.TurnID == "" || r.ExpectedSequence == 0 || !goalDigestPattern.MatchString(r.RequestDigest) {
		return RuntimeControl{}, ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeControl{}, errors.New("model controller transaction failed")
	}
	defer tx.Rollback()
	c, err := readRuntimeControl(ctx, tx, r.RuntimeID)
	if err != nil {
		return RuntimeControl{}, err
	}
	if r.Action == "release" {
		if c == nil || c.Phase != "owned" || c.FormerControllerID != r.ControllerID || c.FormerGeneration != r.Generation || c.TurnID != r.TurnID || c.Sequence != r.ExpectedSequence || c.RequestDigest != r.RequestDigest {
			return RuntimeControl{}, ErrRuntimeControlConflict
		}
		c.ReleasePending = false
	} else {
		if err := requireControlledRuntimeActive(ctx, tx, r.RuntimeID, s.now().UTC()); err != nil {
			return RuntimeControl{}, err
		}
		var status Status
		var sequence uint64
		var digest string
		var expires int64
		if err := tx.QueryRowContext(ctx, `SELECT status,sequence,request_digest,expires_at FROM model_turns WHERE runtime_id=? AND turn_id=? AND sequence=(SELECT MAX(sequence) FROM model_turns WHERE runtime_id=?)`, r.RuntimeID, r.TurnID, r.RuntimeID).Scan(&status, &sequence, &digest, &expires); err != nil {
			return RuntimeControl{}, ErrRuntimeControlConflict
		}
		// The pending turn is the driver rendezvous: already responded or
		// consumed tool batches cannot be transferred or replayed.
		if sequence != r.ExpectedSequence || digest != r.RequestDigest || (status != StatusAwaitingModel && status != StatusDisconnected) || expires <= s.now().UTC().UnixNano() {
			return RuntimeControl{}, ErrRuntimeControlConflict
		}
		if c == nil {
			if r.Action != "claim" || r.Generation != 0 || r.SuccessorID != "" {
				return RuntimeControl{}, ErrRuntimeControlConflict
			}
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_controls`).Scan(&count); err != nil || count >= 4096 {
				return RuntimeControl{}, errors.New("model controller capacity unavailable")
			}
			c = &RuntimeControl{RuntimeID: r.RuntimeID, ControllerID: r.ControllerID, Generation: 1, Phase: "owned", TurnID: r.TurnID, Sequence: r.ExpectedSequence, RequestDigest: r.RequestDigest}
		} else {
			if c.Generation != r.Generation || c.Generation >= 1000000000 {
				return RuntimeControl{}, ErrRuntimeControlConflict
			}
			bound := c.TurnID == r.TurnID && c.Sequence == r.ExpectedSequence && c.RequestDigest == r.RequestDigest
			switch r.Action {
			case "claim":
				if c.Phase != "owned" || c.ControllerID != r.ControllerID || !bound || r.SuccessorID != "" {
					return RuntimeControl{}, ErrRuntimeControlConflict
				}
			case "prepare":
				if c.Phase != "owned" || c.ReleasePending || c.ControllerID != r.ControllerID || !controllerIDPattern.MatchString(r.SuccessorID) || r.SuccessorID == c.ControllerID {
					return RuntimeControl{}, ErrRuntimeControlConflict
				}
				c.Phase, c.SuccessorID = "prepared", r.SuccessorID
				c.TurnID, c.Sequence, c.RequestDigest = r.TurnID, r.ExpectedSequence, r.RequestDigest
				c.FormerControllerID, c.FormerGeneration, c.ReleasePending = "", 0, false
			case "ack":
				if (c.Phase != "prepared" && c.Phase != "acknowledged") || c.SuccessorID != r.ControllerID || !bound || r.SuccessorID != "" {
					return RuntimeControl{}, ErrRuntimeControlConflict
				}
				c.Phase = "acknowledged"
			case "transfer":
				if c.Phase != "acknowledged" || (c.ControllerID != r.ControllerID && c.SuccessorID != r.ControllerID) || !bound || r.SuccessorID != "" {
					return RuntimeControl{}, ErrRuntimeControlConflict
				}
				c.FormerControllerID, c.FormerGeneration, c.ReleasePending = c.ControllerID, c.Generation, true
				c.ControllerID, c.SuccessorID, c.Phase, c.Generation = c.SuccessorID, "", "owned", c.Generation+1
			case "abort":
				if c.Phase == "owned" || c.ControllerID != r.ControllerID || !bound || r.SuccessorID != "" {
					return RuntimeControl{}, ErrRuntimeControlConflict
				}
				c.Phase, c.SuccessorID, c.Generation = "owned", "", c.Generation+1
			default:
				return RuntimeControl{}, ErrInvalidRequest
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runtime_controls(runtime_id,controller_id,generation,phase,successor_id,turn_id,sequence,request_digest,former_controller_id,former_generation,release_pending) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(runtime_id) DO UPDATE SET controller_id=excluded.controller_id,generation=excluded.generation,phase=excluded.phase,successor_id=excluded.successor_id,turn_id=excluded.turn_id,sequence=excluded.sequence,request_digest=excluded.request_digest,former_controller_id=excluded.former_controller_id,former_generation=excluded.former_generation,release_pending=excluded.release_pending`, c.RuntimeID, c.ControllerID, c.Generation, c.Phase, c.SuccessorID, c.TurnID, c.Sequence, c.RequestDigest, c.FormerControllerID, c.FormerGeneration, c.ReleasePending)
	if err != nil {
		return RuntimeControl{}, errors.New("model controller persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return RuntimeControl{}, errors.New("model controller commit failed")
	}
	s.signal()
	return *c, nil
}

func requireControlledRuntimeActive(ctx context.Context, reader controlReader, runtimeID string, now time.Time) error {
	var status RuntimeStatus
	var state RuntimeState
	var expires int64
	if err := reader.QueryRowContext(ctx, `SELECT status,state,expires_at FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(&status, &state, &expires); err != nil || (status != RuntimeReady && status != RuntimeRunning) || !validRuntimeState(state) || terminalRuntimeState(state) || expires <= now.UnixNano() {
		return ErrRuntimeControlConflict
	}
	return nil
}
