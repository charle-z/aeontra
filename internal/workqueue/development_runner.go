package workqueue

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const MaxDevelopmentRunnerEffects = 1024

var runnerEffectPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runnerDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var runnerJobPattern = regexp.MustCompile(`^wj_[a-f0-9]{32}$`)

// DevelopmentRunnerEffect stores non-secret immutable dispatch identity in the
// existing queue database. Commands, tokens, source bodies and logs are excluded.
type DevelopmentRunnerEffect struct {
	EffectID       string                   `json:"effect_id"`
	Revision       uint64                   `json:"revision"`
	BindingDigest  string                   `json:"binding_digest"`
	TemplateDigest string                   `json:"template_digest"`
	WorkflowID     int64                    `json:"workflow_id"`
	CommandProfile string                   `json:"command_profile"`
	JobID          string                   `json:"job_id"`
	Fence          uint64                   `json:"fence"`
	State          string                   `json:"state"`
	RunID          int64                    `json:"run_id,omitempty"`
	StartedAt      time.Time                `json:"started_at"`
	CompletedAt    time.Time                `json:"completed_at,omitempty"`
	ReceiptDigest  string                   `json:"receipt_digest,omitempty"`
	Failure        development.FailureClass `json:"failure_class,omitempty"`
}

func (r DevelopmentRunnerEffect) valid() bool {
	if r.CommandProfile != "probe-only" && r.CommandProfile != "make-validate-all" && r.CommandProfile != "go-test-all" {
		return false
	}
	if !runnerEffectPattern.MatchString(r.EffectID) || !runnerDigestPattern.MatchString(r.BindingDigest) || !runnerDigestPattern.MatchString(r.TemplateDigest) || r.WorkflowID < 1 || !runnerJobPattern.MatchString(r.JobID) || r.Fence == 0 || r.Revision == 0 || r.Revision > 1<<20 || r.StartedAt.IsZero() || r.RunID < 0 {
		return false
	}
	if !r.CompletedAt.IsZero() && (r.CompletedAt.Before(r.StartedAt) || r.State != "succeeded" && r.State != "failed" && r.State != "cancelled") {
		return false
	}
	switch r.State {
	case "dispatch_intent", "pending", "cancel_intent", "cancelled":
		return r.ReceiptDigest == "" && r.Failure == ""
	case "failed":
		_, ok := development.ContinuationForFailure(r.Failure)
		return r.ReceiptDigest == "" && ok
	case "succeeded":
		return r.RunID > 0 && runnerDigestPattern.MatchString(r.ReceiptDigest) && r.Failure == ""
	default:
		return false
	}
}

func (s *Store) DevelopmentRunnerEffect(effectID string) (DevelopmentRunnerEffect, bool, error) {
	if s == nil || s.db == nil || !runnerEffectPattern.MatchString(effectID) {
		return DevelopmentRunnerEffect{}, false, errors.New("workqueue: invalid runner effect")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readRunnerEffect(s.db.QueryRow(`SELECT revision,record_json FROM development_runner_effects WHERE effect_id=?`, effectID))
}

func readRunnerEffect(row *sql.Row) (DevelopmentRunnerEffect, bool, error) {
	var revision uint64
	var body []byte
	err := row.Scan(&revision, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return DevelopmentRunnerEffect{}, false, nil
	}
	r, decodeErr := decodeRunnerEffect(body)
	if err != nil || decodeErr != nil || revision != r.Revision {
		return DevelopmentRunnerEffect{}, false, errors.New("workqueue: corrupt runner effect")
	}
	return r, true, nil
}

func decodeRunnerEffect(body []byte) (DevelopmentRunnerEffect, error) {
	var record DevelopmentRunnerEffect
	if len(body) == 0 || len(body) > 4096 {
		return record, errors.New("workqueue: corrupt runner effect")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || !record.valid() {
		return record, errors.New("workqueue: corrupt runner effect")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return record, errors.New("workqueue: corrupt runner effect")
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, body) {
		return record, errors.New("workqueue: corrupt runner effect")
	}
	return record, nil
}

func (s *Store) developmentRunnerIntegrity() error {
	rows, err := s.db.Query(`SELECT effect_id,revision,record_json FROM development_runner_effects ORDER BY effect_id`)
	if err != nil {
		return errors.New("workqueue: runner journal scan failed")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var revision uint64
		var body []byte
		count++
		if count > MaxDevelopmentRunnerEffects || rows.Scan(&id, &revision, &body) != nil {
			return errors.New("workqueue: runner journal integrity failed")
		}
		r, err := decodeRunnerEffect(body)
		if err != nil || id != r.EffectID || revision != r.Revision {
			return errors.New("workqueue: runner journal integrity failed")
		}
	}
	if rows.Err() != nil {
		return errors.New("workqueue: runner journal scan failed")
	}
	return nil
}

// SaveDevelopmentRunnerEffect is fenced CAS. Revision one is persisted before
// any POST. Only the caller that created that row may dispatch; later callers
// reconcile it, including after process restart and lost acknowledgement.
func (s *Store) SaveDevelopmentRunnerEffect(r DevelopmentRunnerEffect, lease Lease) (bool, error) {
	if s == nil || s.db == nil || !r.valid() {
		return false, errors.New("workqueue: invalid runner effect")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, errors.New("workqueue: runner transaction failed")
	}
	defer tx.Rollback()
	job, found, err := jobByID(tx, r.JobID)
	if err != nil || !found || job.State != StateLeased || job.LeaseID != lease.ID || job.Fence != lease.Fence || r.Fence != lease.Fence || !s.clock().Before(job.LeaseExpiresAt) {
		return false, errors.New("workqueue: runner lease is stale")
	}
	old, found, err := readRunnerEffect(tx.QueryRow(`SELECT revision,record_json FROM development_runner_effects WHERE effect_id=?`, r.EffectID))
	if err != nil {
		return false, err
	}
	if found {
		if old.BindingDigest != r.BindingDigest || old.TemplateDigest != r.TemplateDigest || old.WorkflowID != r.WorkflowID || old.CommandProfile != r.CommandProfile || old.JobID != r.JobID || !old.StartedAt.Equal(r.StartedAt) || old.Fence > r.Fence || old.RunID != 0 && old.RunID != r.RunID {
			return false, errors.New("workqueue: runner identity conflict")
		}
		if old.Revision == r.Revision {
			return false, errors.New("workqueue: runner revision conflict")
		}
		if old.Revision+1 != r.Revision || old.State == "succeeded" || old.State == "failed" || old.State == "cancelled" || old.State == "cancel_intent" && r.State != "cancel_intent" && r.State != "cancelled" && r.State != "failed" {
			return false, errors.New("workqueue: runner transition conflict")
		}
	} else {
		if r.Revision != 1 || r.State != "dispatch_intent" || r.RunID != 0 {
			return false, errors.New("workqueue: runner must start with intent")
		}
		var count int
		if tx.QueryRow(`SELECT COUNT(*) FROM development_runner_effects`).Scan(&count) != nil || count >= MaxDevelopmentRunnerEffects {
			return false, errors.New("workqueue: runner journal capacity reached")
		}
	}
	if r.State == "succeeded" || r.State == "failed" || r.State == "cancelled" {
		if r.CompletedAt.IsZero() {
			r.CompletedAt = s.clock().UTC()
			if r.CompletedAt.Before(r.StartedAt) {
				r.CompletedAt = r.StartedAt
			}
		}
	}
	body, err := json.Marshal(r)
	if err != nil || len(body) > 4096 {
		return false, errors.New("workqueue: runner record invalid")
	}
	if found {
		_, err = tx.Exec(`UPDATE development_runner_effects SET revision=?,record_json=? WHERE effect_id=? AND revision=?`, r.Revision, body, r.EffectID, old.Revision)
	} else {
		_, err = tx.Exec(`INSERT INTO development_runner_effects(effect_id,revision,record_json) VALUES(?,?,?)`, r.EffectID, r.Revision, body)
	}
	if err != nil || tx.Commit() != nil {
		return false, errors.New("workqueue: runner persistence failed")
	}
	return !found, nil
}

// PruneDevelopmentRunnerEffects expires only settled effects after thirty days.
// Active effects, effects still needed by a nonterminal queue job, and the
// administrator's current calibration stay durable; no unknown POST is replayed.
func (s *Store) PruneDevelopmentRunnerEffects(retained []string) error {
	if s == nil || s.db == nil || len(retained) > 2 {
		return errors.New("workqueue: runner retention invalid")
	}
	keep := map[string]bool{}
	for _, id := range retained {
		if !runnerEffectPattern.MatchString(id) {
			return errors.New("workqueue: runner retention invalid")
		}
		keep[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("workqueue: runner retention transaction failed")
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT effect_id,revision,record_json FROM development_runner_effects ORDER BY effect_id LIMIT ?`, MaxDevelopmentRunnerEffects+1)
	if err != nil {
		return errors.New("workqueue: runner retention unavailable")
	}
	type expiredEffect struct {
		id       string
		revision uint64
		job      string
	}
	expired := []expiredEffect{}
	count := 0
	cutoff := s.clock().UTC().Add(-30 * 24 * time.Hour)
	for rows.Next() {
		var id string
		var revision uint64
		var body []byte
		count++
		if count > MaxDevelopmentRunnerEffects || rows.Scan(&id, &revision, &body) != nil {
			_ = rows.Close()
			return errors.New("workqueue: runner retention corrupt")
		}
		record, err := decodeRunnerEffect(body)
		if err != nil || record.EffectID != id || record.Revision != revision {
			_ = rows.Close()
			return errors.New("workqueue: runner retention corrupt")
		}
		at := record.CompletedAt
		if at.IsZero() {
			at = record.StartedAt
		}
		if !keep[id] && !at.After(cutoff) && (record.State == "succeeded" || record.State == "failed" || record.State == "cancelled") {
			expired = append(expired, expiredEffect{id, revision, record.JobID})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return errors.New("workqueue: runner retention scan failed")
	}
	_ = rows.Close()
	for _, effect := range expired {
		job, found, err := jobByID(tx, effect.job)
		if err != nil || !found {
			return errors.New("workqueue: runner retention job unavailable")
		}
		if !terminal(job.State) {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM development_runner_effects WHERE effect_id=? AND revision=?`, effect.id, effect.revision); err != nil {
			return errors.New("workqueue: runner retention failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("workqueue: runner retention failed")
	}
	return nil
}
