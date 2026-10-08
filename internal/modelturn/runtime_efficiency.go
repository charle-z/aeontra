package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const MaxEfficiencyTurnSamples = 4096

// RuntimeEfficiencySnapshot is advisory, content-free evidence from existing
// runtime metadata. Durations overlap and must not be added as elapsed-time
// components. Turn counts and response waits cover only the bounded latest sample.
// An interrupted turn does not establish duplicate work, token usage, or cost.
type RuntimeEfficiencySnapshot struct {
	ElapsedMS                 *int64 `json:"elapsed_ms,omitempty"`
	QueueMS                   *int64 `json:"queue_ms,omitempty"`
	StartupMS                 *int64 `json:"startup_ms,omitempty"`
	TimeToFirstTurnMS         *int64 `json:"time_to_first_turn_ms,omitempty"`
	RecordedLeaseRetries      uint32 `json:"recorded_lease_retries"`
	RetryMeasurementKnown     bool   `json:"retry_measurement_known"`
	RetryCountLowerBound      bool   `json:"retry_count_lower_bound"`
	ObservedTurns             int64  `json:"observed_turns"`
	TurnSampleTruncated       bool   `json:"turn_sample_truncated"`
	RespondedTurns            int64  `json:"responded_turns"`
	ConsumedTurns             int64  `json:"consumed_turns"`
	UnrespondedTerminalTurns  int64  `json:"unresponded_terminal_turns"`
	PendingTurns              int64  `json:"pending_turns"`
	ResponseWaitMS            int64  `json:"response_wait_ms"`
	MeasuredResponseWaitTurns int64  `json:"measured_response_wait_turns"`
}

// RuntimeEfficiency performs an optional bounded metadata-only read. It does not
// reconcile/expire runtimes, load request/response bodies, signal workers, or grant
// authority. Missing recorded timestamps are unknown, not zero-duration evidence.
func (s *Store) RuntimeEfficiency(ctx context.Context, runtimeID string) (RuntimeEfficiencySnapshot, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return RuntimeEfficiencySnapshot{}, ErrInvalidRequest
	}
	if s == nil || s.db == nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency read failed")
	}
	defer tx.Rollback()
	var state RuntimeState
	var createdAt int64
	if err := tx.QueryRowContext(ctx, `SELECT state,created_at FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(&state, &createdAt); errors.Is(err, sql.ErrNoRows) {
		return RuntimeEfficiencySnapshot{}, ErrTurnNotFound
	} else if err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency read failed")
	}
	if !validRuntimeState(state) {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency state invalid")
	}
	created := time.Unix(0, createdAt).UTC()
	result := RuntimeEfficiencySnapshot{}
	var lease, firstTurn, terminal *time.Time
	rows, err := tx.QueryContext(ctx, `SELECT phase,category,count,occurred_at FROM runtime_phase_events WHERE runtime_id=? ORDER BY occurred_at,phase,category LIMIT ?`, runtimeID, MaxRuntimePhaseEvents+1)
	if err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency phases failed")
	}
	phaseCount := 0
	for rows.Next() {
		var phase RuntimePhase
		var category RuntimeRetryCategory
		var count uint32
		var occurredAt int64
		if err := rows.Scan(&phase, &category, &count, &occurredAt); err != nil || !validRuntimePhase(phase) || (phase != RuntimePhaseLeaseRetry && !validRuntimePhaseCategory(phase, category, count)) {
			_ = rows.Close()
			return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency phases failed")
		}
		phaseCount++
		if phaseCount > MaxRuntimePhaseEvents {
			_ = rows.Close()
			return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency phase limit exceeded")
		}
		stamp := time.Unix(0, occurredAt).UTC()
		// An aggregated retry's last_at is not a milestone timestamp.
		// Preserve recorded endpoints rather than moving later rows forward
		// to infer ordering; reversed/missing intervals stay unknown.
		switch phase {
		case RuntimePhaseLeaseAssigned:
			if !stamp.Before(created) {
				lease = &stamp
			}
		case RuntimePhaseFirstTurnCreated:
			if !stamp.Before(created) {
				firstTurn = &stamp
			}
		case RuntimePhaseTerminal:
			if !stamp.Before(created) {
				terminal = &stamp
			}
		case RuntimePhaseLeaseRetry:
			// Persistence caps each category at 1000. That value is a lower
			// bound; absent retry reports cannot prove that retries were zero.
			if count < 1 || count > 1000 || !validRuntimePhaseCategory(phase, category, 1) {
				_ = rows.Close()
				return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency retry measurement invalid")
			}
			result.RecordedLeaseRetries += count
			result.RetryMeasurementKnown = true
			result.RetryCountLowerBound = result.RetryCountLowerBound || count == 1000
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency phases failed")
	}
	if err := rows.Close(); err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency phases failed")
	}
	if terminalRuntimeState(state) {
		if terminal != nil {
			result.ElapsedMS = efficiencyMilliseconds(created, *terminal)
		}
	} else {
		result.ElapsedMS = efficiencyMilliseconds(created, s.now().UTC())
	}
	if lease != nil {
		result.QueueMS = efficiencyMilliseconds(created, *lease)
	}
	if firstTurn != nil {
		result.TimeToFirstTurnMS = efficiencyMilliseconds(created, *firstTurn)
		if lease != nil {
			result.StartupMS = efficiencyMilliseconds(*lease, *firstTurn)
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT status,created_at,responded_at FROM model_turns WHERE runtime_id=? ORDER BY sequence DESC LIMIT ?`, runtimeID, MaxEfficiencyTurnSamples+1)
	if err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency turns failed")
	}
	defer rows.Close()
	for rows.Next() {
		if result.ObservedTurns == MaxEfficiencyTurnSamples {
			result.TurnSampleTruncated = true
			break
		}
		var status Status
		var turnCreatedAt int64
		var respondedAt sql.NullInt64
		if err := rows.Scan(&status, &turnCreatedAt, &respondedAt); err != nil {
			return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency turns failed")
		}
		result.ObservedTurns++
		responded := respondedAt.Valid || status == StatusResponded || status == StatusConsumed
		if responded {
			result.RespondedTurns++
		}
		if status == StatusConsumed {
			result.ConsumedTurns++
		}
		if (status == StatusCancelled || status == StatusExpired || status == StatusFailed) && !responded {
			result.UnrespondedTerminalTurns++
		}
		if status == StatusCreated || status == StatusAwaitingModel || status == StatusDisconnected {
			result.PendingTurns++
		}
		if respondedAt.Valid && respondedAt.Int64 >= turnCreatedAt {
			result.ResponseWaitMS += nonNegativeMilliseconds(time.Unix(0, respondedAt.Int64).Sub(time.Unix(0, turnCreatedAt)))
			result.MeasuredResponseWaitTurns++
		}
	}
	if err := rows.Err(); err != nil {
		return RuntimeEfficiencySnapshot{}, errors.New("model runtime efficiency turns failed")
	}
	return result, nil
}

func efficiencyMilliseconds(start, end time.Time) *int64 {
	if end.Before(start) {
		return nil
	}
	value := nonNegativeMilliseconds(end.Sub(start))
	return &value
}
