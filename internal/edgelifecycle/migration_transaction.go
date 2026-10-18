package edgelifecycle

import (
	"errors"
	"os"
)

func FinalizeLegacyStateMigration(config StateMigrationConfig) (StateMigrationResult, error) {
	normalized, err := normalizeStateMigrationConfig(config)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if currentEUID() != normalized.ExpectedUID {
		return StateMigrationResult{}, migrationErr(MigrationErrorWrongExecutor, errors.New("migration finalization must run as the Edge state owner"))
	}
	plan := canonicalStateMigrationPlan(normalized)
	if err := validateMigrationPathAncestors(plan); err != nil {
		return StateMigrationResult{}, err
	}
	if _, err := os.Lstat(plan.JournalPath); errors.Is(err, os.ErrNotExist) {
		return StateMigrationResult{Status: MigrationStatusNotNeeded}, nil
	} else if err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorJournalInvalid, err)
	}
	journal, err := readMigrationJournal(plan.JournalPath, normalized.ExpectedUID, normalized.ExpectedGID)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if !validMigrationJournal(plan, journal) || journal.Stage != MigrationStageVerified {
		return StateMigrationResult{}, migrationErr(MigrationErrorJournalInvalid, errors.New("state migration is not ready to finalize"))
	}
	if err := verifyMigratedState(plan, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorVerificationFailed, err)
	}
	if err := removeMigrationJournal(plan.JournalPath); err != nil {
		return StateMigrationResult{}, err
	}
	return StateMigrationResult{Status: MigrationStatusMigrated}, nil
}

func RollbackPreparedLegacyStateMigration(config StateMigrationConfig) (StateMigrationResult, error) {
	normalized, err := normalizeStateMigrationConfig(config)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if currentEUID() != normalized.ExpectedUID {
		return StateMigrationResult{}, migrationErr(MigrationErrorWrongExecutor, errors.New("migration rollback must run as the Edge state owner"))
	}
	plan := canonicalStateMigrationPlan(normalized)
	if err := validateMigrationPathAncestors(plan); err != nil {
		return StateMigrationResult{}, err
	}
	if _, err := os.Lstat(plan.JournalPath); errors.Is(err, os.ErrNotExist) {
		return StateMigrationResult{Status: MigrationStatusNotNeeded}, nil
	} else if err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorJournalInvalid, err)
	}
	journal, err := readMigrationJournal(plan.JournalPath, normalized.ExpectedUID, normalized.ExpectedGID)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if !validMigrationJournal(plan, journal) || journal.Stage != MigrationStageVerified {
		return StateMigrationResult{}, migrationErr(MigrationErrorJournalInvalid, errors.New("state migration is not ready to roll back"))
	}
	if err := rollbackStateMigration(plan, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorRollbackFailed, err)
	}
	return StateMigrationResult{Status: MigrationStatusRecoveredRollback}, nil
}
