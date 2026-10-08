package workqueue

import (
	"database/sql"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const MaxDevelopmentObjectives = 1024

// DevelopmentObjectiveRetentionTTL preserves terminal replay evidence for at
// least as long as development request idempotency. Retained requests and live
// provisioning jobs pin their objectives beyond this interval.
const DevelopmentObjectiveRetentionTTL = 30 * 24 * time.Hour

type DevelopmentObjectiveSummary struct {
	ObjectiveID string                     `json:"objective_id"`
	Revision    uint64                     `json:"revision"`
	State       development.ObjectiveState `json:"state"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

// DevelopmentProvisionOwner recovers an objective from a durable provisioning
// identity, including the boundary where enqueue committed but job binding did
// not. The search is confined to the bounded objective table, never the disk.
func (s *Store) DevelopmentProvisionOwner(provisionID string) (development.Objective, string, bool, error) {
	if s == nil || s.db == nil || !development.ValidObjectiveID(provisionID) {
		return development.Objective{}, "", false, errors.New("workqueue: provision identity is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT d.objective_id
		FROM development_objectives d, json_each(d.record_json,'$.steps') steps,
		json_each(steps.value,'$.provisioning') provisions
		WHERE json_extract(provisions.value,'$.provision_id')=? LIMIT 2`, provisionID)
	if err != nil {
		return development.Objective{}, "", false, errors.New("workqueue: provision lookup failed")
	}
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return development.Objective{}, "", false, errors.New("workqueue: provision lookup failed")
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || len(ids) > 1 {
		return development.Objective{}, "", false, errors.New("workqueue: provision ownership is ambiguous")
	}
	if len(ids) == 0 {
		return development.Objective{}, "", false, nil
	}
	objective, found, err := developmentObjectiveByID(s.db, ids[0])
	if err != nil || !found {
		return development.Objective{}, "", false, errors.New("workqueue: provision owner is invalid")
	}
	for _, step := range objective.Steps {
		for _, provision := range step.Provisioning {
			if provision.ProvisionID == provisionID {
				return objective, step.StepID, true, nil
			}
		}
	}
	return development.Objective{}, "", false, errors.New("workqueue: provision owner is invalid")
}

// SaveDevelopmentObjective creates revision one or atomically advances exactly
// one revision. The canonical objective record is content-free coordination
// metadata; every update is checked against the immutable transition contract.
func (s *Store) SaveDevelopmentObjective(objective development.Objective) (development.Objective, bool, error) {
	if s == nil || s.db == nil || !objective.Valid() {
		return development.Objective{}, false, errors.New("workqueue: development objective is invalid")
	}
	body, digest, err := objective.MarshalRecord()
	if err != nil {
		return development.Objective{}, false, errors.New("workqueue: development objective is invalid")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return development.Objective{}, false, errors.New("workqueue: development objective transaction failed")
	}
	defer tx.Rollback()

	existing, found, err := developmentObjectiveByIDTx(tx, objective.ObjectiveID)
	if err != nil {
		return development.Objective{}, false, err
	}
	if !found {
		if objective.Revision != 1 || objective.State != development.ObjectivePlanned {
			return development.Objective{}, false, errors.New("workqueue: development objective revision conflict")
		}
		now := s.now().UTC()
		if err := pruneExpiredDevelopmentObjectives(tx, now); err != nil {
			return development.Objective{}, false, err
		}
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM development_objectives`).Scan(&count); err != nil {
			return development.Objective{}, false, errors.New("workqueue: development objective capacity unavailable")
		}
		if count < 0 || count >= MaxDevelopmentObjectives {
			return development.Objective{}, false, errors.New("workqueue: development objective row bound exceeded")
		}
		if _, err := tx.Exec(`INSERT INTO development_objectives(objective_id,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			objective.ObjectiveID, objective.Revision, objective.State, digest, body, now.UnixNano(), now.UnixNano()); err != nil {
			return development.Objective{}, false, errors.New("workqueue: development objective persistence failed")
		}
		if err := tx.Commit(); err != nil {
			return development.Objective{}, false, errors.New("workqueue: development objective persistence failed")
		}
		return objective, true, nil
	}

	existingBody, existingDigest, err := existing.MarshalRecord()
	if err != nil || existingDigest == "" {
		return development.Objective{}, false, errors.New("workqueue: development objective record is corrupt")
	}
	_ = existingBody
	if objective.Revision == existing.Revision && digest == existingDigest {
		return existing, false, nil
	}
	if objective.Revision != existing.Revision+1 || development.ValidateTransition(existing, objective) != nil {
		return development.Objective{}, false, errors.New("workqueue: development objective revision conflict")
	}

	now := s.now().UTC()
	result, err := tx.Exec(`UPDATE development_objectives
		SET revision=?,state=?,record_digest=?,record_json=?,updated_at=?
		WHERE objective_id=? AND revision=? AND record_digest=?`,
		objective.Revision, objective.State, digest, body, now.UnixNano(),
		objective.ObjectiveID, existing.Revision, existingDigest)
	if err != nil {
		return development.Objective{}, false, errors.New("workqueue: development objective persistence failed")
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return development.Objective{}, false, errors.New("workqueue: development objective revision conflict")
	}
	if err := tx.Commit(); err != nil {
		return development.Objective{}, false, errors.New("workqueue: development objective persistence failed")
	}
	return objective, false, nil
}

// Pruning is part of new-objective admission, not replay or revision updates.
// It removes only canonical terminal coordination rows, never request bodies,
// job receipts or artifacts. Any uncertain protector aborts the transaction.
func pruneExpiredDevelopmentObjectives(tx *sql.Tx, now time.Time) error {
	cutoff := now.Add(-DevelopmentObjectiveRetentionTTL).UnixNano()
	rows, err := tx.Query(`SELECT objective_id,created_at,updated_at FROM development_objectives
		WHERE state IN (?,?,?) AND updated_at<? ORDER BY objective_id LIMIT ?`,
		development.ObjectiveAccepted, development.ObjectiveFailed, development.ObjectiveCancelled, cutoff, MaxDevelopmentObjectives+1)
	if err != nil {
		return errors.New("workqueue: development objective retention scan failed")
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &createdAt, &updatedAt); err != nil || !development.ValidObjectiveID(id) || createdAt > updatedAt {
			_ = rows.Close()
			return errors.New("workqueue: development objective retention record is corrupt")
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil || len(ids) > MaxDevelopmentObjectives {
		return errors.New("workqueue: development objective retention scan failed")
	}
	if len(ids) == 0 {
		return nil
	}
	objectives := make([]development.Objective, 0, len(ids))
	pinned := make(map[string]bool, len(ids))
	for _, id := range ids {
		objective, found, err := developmentObjectiveByIDTx(tx, id)
		if err != nil || !found {
			return errors.New("workqueue: development objective retention record is corrupt")
		}
		objectives = append(objectives, objective)
		pinned[id] = false
	}
	// Stream every physically retained request. Selecting only a JSON reference
	// would silently miss a corrupt protector; raw SQL TTL deletion is not safe
	// here. Memory is bounded by the objective table, not request history.
	rows, err = tx.Query(`SELECT request_id,revision,state,key_digest,record_digest,record_json FROM development_requests`)
	if err != nil {
		return errors.New("workqueue: development objective retention request scan failed")
	}
	for rows.Next() {
		request, found, err := scanDevelopmentRequest(rows)
		if err != nil || !found {
			_ = rows.Close()
			return errors.New("workqueue: development objective retention request is corrupt")
		}
		if _, candidate := pinned[request.ObjectiveID]; candidate {
			pinned[request.ObjectiveID] = true
		}
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil || closeErr != nil {
		return errors.New("workqueue: development objective retention request scan failed")
	}
	for _, objective := range objectives {
		for _, step := range objective.Steps {
			for _, provision := range step.Provisioning {
				// The durable key also covers enqueue-before-BindProvisionJob.
				job, found, err := jobByIdempotency(tx, "development:"+provision.ProvisionID)
				if err != nil {
					return errors.New("workqueue: development objective retention job is corrupt")
				}
				if found && !terminal(job.State) {
					pinned[objective.ObjectiveID] = true
				}
				if provision.JobID != "" {
					job, found, err = jobByID(tx, provision.JobID)
					if err != nil || !found {
						return errors.New("workqueue: development objective retention job is corrupt")
					}
					if !terminal(job.State) {
						pinned[objective.ObjectiveID] = true
					}
				}
			}
		}
	}
	// Validate all candidates/protectors before deleting any evidence. Rollback
	// also preserves every row when capacity remains exhausted after pruning.
	for _, objective := range objectives {
		if pinned[objective.ObjectiveID] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM development_objectives WHERE objective_id=?`, objective.ObjectiveID); err != nil {
			return errors.New("workqueue: expired development objective pruning failed")
		}
	}
	return nil
}

func (s *Store) DevelopmentObjective(objectiveID string) (development.Objective, bool, error) {
	if s == nil || s.db == nil || !development.ValidObjectiveID(objectiveID) {
		return development.Objective{}, false, errors.New("workqueue: development objective id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return developmentObjectiveByID(s.db, objectiveID)
}

func (s *Store) DevelopmentObjectiveSummaries(limit int) ([]DevelopmentObjectiveSummary, error) {
	if s == nil || s.db == nil || limit < 1 || limit > MaxListResults {
		return nil, errors.New("workqueue: development objective list limit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT objective_id,updated_at
		FROM development_objectives ORDER BY updated_at DESC,objective_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, errors.New("workqueue: development objective list failed")
	}
	type summaryRow struct {
		objectiveID string
		updatedAt   int64
	}
	index := make([]summaryRow, 0, limit)
	for rows.Next() {
		var row summaryRow
		if err := rows.Scan(&row.objectiveID, &row.updatedAt); err != nil ||
			!development.ValidObjectiveID(row.objectiveID) {
			_ = rows.Close()
			return nil, errors.New("workqueue: development objective list result is invalid")
		}
		index = append(index, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, errors.New("workqueue: development objective list failed")
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New("workqueue: development objective list failed")
	}
	summaries := make([]DevelopmentObjectiveSummary, 0, len(index))
	for _, row := range index {
		objective, found, err := developmentObjectiveByID(s.db, row.objectiveID)
		if err != nil || !found {
			return nil, errors.New("workqueue: development objective list result is invalid")
		}
		summaries = append(summaries, DevelopmentObjectiveSummary{
			ObjectiveID: objective.ObjectiveID,
			Revision:    objective.Revision,
			State:       objective.State,
			UpdatedAt:   time.Unix(0, row.updatedAt).UTC(),
		})
	}
	return summaries, nil
}

type developmentObjectiveScanner interface {
	Scan(dest ...any) error
}

func developmentObjectiveByID(db *sql.DB, objectiveID string) (development.Objective, bool, error) {
	return scanDevelopmentObjective(db.QueryRow(`SELECT objective_id,revision,state,record_digest,record_json
		FROM development_objectives WHERE objective_id=?`, objectiveID))
}

func developmentObjectiveByIDTx(tx *sql.Tx, objectiveID string) (development.Objective, bool, error) {
	return scanDevelopmentObjective(tx.QueryRow(`SELECT objective_id,revision,state,record_digest,record_json
		FROM development_objectives WHERE objective_id=?`, objectiveID))
}

func scanDevelopmentObjective(row developmentObjectiveScanner) (development.Objective, bool, error) {
	var objectiveID, state, digest string
	var revision int64
	var body []byte
	if err := row.Scan(&objectiveID, &revision, &state, &digest, &body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return development.Objective{}, false, nil
		}
		return development.Objective{}, false, errors.New("workqueue: development objective read failed")
	}
	if revision < 1 || !development.ValidObjectiveID(objectiveID) ||
		digest == "" || development.ObjectiveRecordDigest(body) != digest {
		return development.Objective{}, false, errors.New("workqueue: development objective record is corrupt")
	}
	objective, err := development.ParseObjectiveRecord(body)
	if err != nil || objective.ObjectiveID != objectiveID ||
		objective.Revision != uint64(revision) || string(objective.State) != state {
		return development.Objective{}, false, errors.New("workqueue: development objective record is corrupt")
	}
	canonicalBody, canonicalDigest, err := objective.MarshalRecord()
	if err != nil || canonicalDigest != digest || string(canonicalBody) != string(body) {
		return development.Objective{}, false, errors.New("workqueue: development objective record is corrupt")
	}
	return objective, true, nil
}
