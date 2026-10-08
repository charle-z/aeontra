package edge

import (
	"database/sql"
	"errors"
	"time"
)

const (
	maxOperationLeaseAttempts                = 4
	maxPrivilegedOperationRecoveryWindow     = 20 * time.Minute
	privilegedOperationRecoveryExhaustedCode = "operation_recovery_exhausted"
	operationExecutionInterruptedCode        = "operation_execution_interrupted"
)

type operationLeaseRecoveryExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func recoverExpiredOperationLeases(executor operationLeaseRecoveryExecutor, now time.Time, scopeColumn string, scopeValue string) error {
	if executor == nil || (scopeColumn != "device_id" && scopeColumn != "operation_id") {
		return errors.New("edge operation recovery scope is invalid")
	}
	now = now.UTC()
	cutoff := now.Add(-maxPrivilegedOperationRecoveryWindow).UnixNano()
	_, err := executor.Exec(`UPDATE edge_operations SET
		state=CASE
			WHEN cancel_requested=1 THEN 'cancelled'
			WHEN progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair') THEN 'failed'
			WHEN lease_attempts>=? OR (kind IN ('bundle_update','bundle_rollback','edge_repair') AND
				first_leased_at IS NOT NULL AND first_leased_at<=?) THEN 'failed'
			ELSE 'queued'
		END,
		safe_code=CASE
			WHEN cancel_requested=1 THEN 'operation_cancelled'
			WHEN progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair') THEN 'operation_execution_interrupted'
			WHEN lease_attempts>=? OR (kind IN ('bundle_update','bundle_rollback','edge_repair') AND
				first_leased_at IS NOT NULL AND first_leased_at<=?) THEN 'operation_recovery_exhausted'
			ELSE ''
		END,
		progress_json=CASE WHEN cancel_requested=1 OR (progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair')) THEN progress_json ELSE NULL END,
		leased_at=CASE WHEN cancel_requested=1 OR (progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair')) THEN leased_at ELSE NULL END,
		running_at=CASE WHEN cancel_requested=1 OR (progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair')) THEN running_at ELSE NULL END,
		finalizing_at=CASE WHEN cancel_requested=1 OR (progress_json IS NOT NULL AND kind NOT IN ('bundle_status','onboarding_status','bundle_update','bundle_rollback','edge_repair')) THEN finalizing_at ELSE NULL END,
		lease_id=NULL,lease_until=NULL,updated_at=?
		WHERE `+scopeColumn+`=? AND state='leased' AND lease_until<=?`,
		maxOperationLeaseAttempts, cutoff,
		maxOperationLeaseAttempts, cutoff,
		now.UnixNano(), scopeValue, now.UnixNano())
	return err
}
