package edgelifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const StateMigrationVersion = 1
const stateMigrationJournalName = ".mcp-edge-state-migration-v1.json"
const maxMigrationJournalBytes = 8 << 10

type MigrationStage string

const (
	MigrationStagePrepared MigrationStage = "prepared"
	MigrationStageRenamed  MigrationStage = "renamed"
	MigrationStageVerified MigrationStage = "verified"
)

type MigrationStatus string

const (
	MigrationStatusNotNeeded         MigrationStatus = "not_needed"
	MigrationStatusMigrated          MigrationStatus = "migrated"
	MigrationStatusRecoveredComplete MigrationStatus = "recovered_complete"
	MigrationStatusRecoveredRollback MigrationStatus = "recovered_rollback"
)

type MigrationErrorCode string

const (
	MigrationErrorInventoryBlocked   MigrationErrorCode = "inventory_blocked"
	MigrationErrorStateInvalid       MigrationErrorCode = "state_invalid"
	MigrationErrorOwnerMismatch      MigrationErrorCode = "owner_mismatch"
	MigrationErrorWrongExecutor      MigrationErrorCode = "wrong_executor"
	MigrationErrorPlanChanged        MigrationErrorCode = "plan_changed"
	MigrationErrorJournalExists      MigrationErrorCode = "journal_exists"
	MigrationErrorJournalInvalid     MigrationErrorCode = "journal_invalid"
	MigrationErrorPrepareFailed      MigrationErrorCode = "prepare_failed"
	MigrationErrorRenameFailed       MigrationErrorCode = "rename_failed"
	MigrationErrorVerificationFailed MigrationErrorCode = "verification_failed"
	MigrationErrorRollbackFailed     MigrationErrorCode = "rollback_failed"
	MigrationErrorRecoveryAmbiguous  MigrationErrorCode = "recovery_ambiguous"
)

type MigrationError struct {
	Code MigrationErrorCode
	Err  error
}

func (e *MigrationError) Error() string {
	return "edge state migration failed: " + string(e.Code)
}

func (e *MigrationError) Unwrap() error { return e.Err }

type StateMigrationConfig struct {
	Inventory   InventoryConfig
	ExpectedUID int
	ExpectedGID int
	Now         func() time.Time
}

type StateMigrationPlan struct {
	Version     int                  `json:"version"`
	Needed      bool                 `json:"needed"`
	Kind        MigrationDisposition `json:"kind"`
	Source      string               `json:"source"`
	Destination string               `json:"destination"`
	JournalPath string               `json:"journal_path"`
}

type MigrationHooks struct {
	AfterJournal func() error
	AfterRename  func() error
	AfterVerify  func() error
}

type StateMigrationResult struct {
	Status MigrationStatus `json:"status"`
}

type migrationJournal struct {
	Version     int                  `json:"version"`
	Kind        MigrationDisposition `json:"kind"`
	Source      string               `json:"source"`
	Destination string               `json:"destination"`
	Stage       MigrationStage       `json:"stage"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

func PlanLegacyStateMigration(config StateMigrationConfig) (StateMigrationPlan, error) {
	normalized, err := normalizeStateMigrationConfig(config)
	if err != nil {
		return StateMigrationPlan{}, err
	}
	plan := canonicalStateMigrationPlan(normalized)
	report, err := InspectLayout(normalized.Inventory)
	if err != nil {
		return StateMigrationPlan{}, migrationErr(MigrationErrorInventoryBlocked, err)
	}
	if report.StateMigration == MigrationBlocked {
		return StateMigrationPlan{}, migrationErr(MigrationErrorInventoryBlocked, errors.New("Edge layout blocks state migration"))
	}
	if report.StateMigration == MigrationNone {
		return plan, nil
	}
	if report.StateMigration != MigrationLegacyToPreferred {
		return StateMigrationPlan{}, migrationErr(MigrationErrorInventoryBlocked, errors.New("unsupported Edge state migration"))
	}
	if err := validatePrivateState(plan.Source, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
		return StateMigrationPlan{}, err
	}
	if _, err := os.Lstat(plan.JournalPath); err == nil {
		return StateMigrationPlan{}, migrationErr(MigrationErrorJournalExists, errors.New("state migration journal already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return StateMigrationPlan{}, migrationErr(MigrationErrorJournalInvalid, err)
	}
	plan.Needed = true
	plan.Kind = MigrationLegacyToPreferred
	return plan, nil
}

func ApplyLegacyStateMigration(config StateMigrationConfig, plan StateMigrationPlan, hooks MigrationHooks) (result StateMigrationResult, err error) {
	normalized, err := normalizeStateMigrationConfig(config)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if currentEUID() != normalized.ExpectedUID {
		return StateMigrationResult{}, migrationErr(MigrationErrorWrongExecutor, errors.New("migration must run as the Edge state owner"))
	}
	if err := validateMigrationPathAncestors(canonicalStateMigrationPlan(normalized)); err != nil {
		return StateMigrationResult{}, err
	}
	expected, err := PlanLegacyStateMigration(normalized)
	if err != nil {
		if migrationCode(err) == MigrationErrorJournalExists {
			return StateMigrationResult{}, err
		}
		if plan.Needed {
			return StateMigrationResult{}, migrationErr(MigrationErrorPlanChanged, err)
		}
		return StateMigrationResult{}, err
	}
	if !sameMigrationPlan(expected, plan) {
		return StateMigrationResult{}, migrationErr(MigrationErrorPlanChanged, errors.New("state migration plan changed"))
	}
	if !plan.Needed {
		return StateMigrationResult{Status: MigrationStatusNotNeeded}, nil
	}
	if err := ensurePrivateMigrationParent(plan, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
		return StateMigrationResult{}, err
	}

	now := normalized.Now().UTC()
	journal := newMigrationJournal(plan, MigrationStagePrepared, now)
	if err := writeMigrationJournal(plan.JournalPath, journal, normalized.ExpectedUID, normalized.ExpectedGID, true); err != nil {
		return StateMigrationResult{}, err
	}
	rollbackRequired := true
	defer func() {
		if err == nil || !rollbackRequired {
			return
		}
		if rollbackErr := rollbackStateMigration(plan, normalized.ExpectedUID, normalized.ExpectedGID); rollbackErr != nil {
			err = migrationErr(MigrationErrorRollbackFailed, errors.Join(err, rollbackErr))
		}
	}()
	if err := callMigrationHook(hooks.AfterJournal); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorPrepareFailed, err)
	}

	if err := renameNoReplace(plan.Source, plan.Destination); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorRenameFailed, err)
	}
	journal.Stage = MigrationStageRenamed
	journal.UpdatedAt = normalized.Now().UTC()
	if err := writeMigrationJournal(plan.JournalPath, journal, normalized.ExpectedUID, normalized.ExpectedGID, false); err != nil {
		return StateMigrationResult{}, err
	}
	if err := callMigrationHook(hooks.AfterRename); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorRenameFailed, err)
	}

	if err := verifyMigratedState(plan, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorVerificationFailed, err)
	}
	journal.Stage = MigrationStageVerified
	journal.UpdatedAt = normalized.Now().UTC()
	if err := writeMigrationJournal(plan.JournalPath, journal, normalized.ExpectedUID, normalized.ExpectedGID, false); err != nil {
		return StateMigrationResult{}, err
	}
	if err := callMigrationHook(hooks.AfterVerify); err != nil {
		return StateMigrationResult{}, migrationErr(MigrationErrorVerificationFailed, err)
	}

	rollbackRequired = false
	if err := removeMigrationJournal(plan.JournalPath); err != nil {
		return StateMigrationResult{}, err
	}
	return StateMigrationResult{Status: MigrationStatusMigrated}, nil
}

func RecoverLegacyStateMigration(config StateMigrationConfig) (StateMigrationResult, error) {
	normalized, err := normalizeStateMigrationConfig(config)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if currentEUID() != normalized.ExpectedUID {
		return StateMigrationResult{}, migrationErr(MigrationErrorWrongExecutor, errors.New("migration recovery must run as the Edge state owner"))
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
	if !validMigrationJournal(plan, journal) {
		return StateMigrationResult{}, migrationErr(MigrationErrorJournalInvalid, errors.New("state migration journal does not match canonical paths"))
	}

	sourceExists, sourceUnsafe, err := migrationPathState(plan.Source)
	if err != nil {
		return StateMigrationResult{}, err
	}
	destinationExists, destinationUnsafe, err := migrationPathState(plan.Destination)
	if err != nil {
		return StateMigrationResult{}, err
	}
	if sourceUnsafe || destinationUnsafe {
		return StateMigrationResult{}, migrationErr(MigrationErrorRecoveryAmbiguous, errors.New("migration path is not a plain directory"))
	}

	switch {
	case sourceExists && !destinationExists:
		if err := validatePrivateState(plan.Source, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
			return StateMigrationResult{}, err
		}
		if err := removeMigrationJournal(plan.JournalPath); err != nil {
			return StateMigrationResult{}, err
		}
		return StateMigrationResult{Status: MigrationStatusRecoveredRollback}, nil
	case !sourceExists && destinationExists:
		if err := verifyMigratedState(plan, normalized.ExpectedUID, normalized.ExpectedGID); err != nil {
			return StateMigrationResult{}, migrationErr(MigrationErrorVerificationFailed, err)
		}
		if err := removeMigrationJournal(plan.JournalPath); err != nil {
			return StateMigrationResult{}, err
		}
		return StateMigrationResult{Status: MigrationStatusRecoveredComplete}, nil
	default:
		return StateMigrationResult{}, migrationErr(MigrationErrorRecoveryAmbiguous, errors.New("migration source and destination state is ambiguous"))
	}
}

func normalizeStateMigrationConfig(config StateMigrationConfig) (StateMigrationConfig, error) {
	if config.ExpectedUID < 0 || config.ExpectedGID < 0 {
		return StateMigrationConfig{}, migrationErr(MigrationErrorOwnerMismatch, errors.New("expected Edge owner is invalid"))
	}
	home, installRoot, historical, err := normalizeInventoryConfig(config.Inventory)
	if err != nil {
		return StateMigrationConfig{}, migrationErr(MigrationErrorInventoryBlocked, err)
	}
	config.Inventory.HomeDir = home
	config.Inventory.InstallRoot = installRoot
	config.Inventory.HistoricalPaths = historical
	if config.Now == nil {
		config.Now = time.Now
	}
	return config, nil
}

func canonicalStateMigrationPlan(config StateMigrationConfig) StateMigrationPlan {
	home := config.Inventory.HomeDir
	return StateMigrationPlan{
		Version:     StateMigrationVersion,
		Kind:        MigrationNone,
		Source:      filepath.Join(home, ".config", "mcp-devbox-edge"),
		Destination: filepath.Join(home, ".local", "state", "mcp-edge"),
		JournalPath: filepath.Join(home, ".local", "state", stateMigrationJournalName),
	}
}

func sameMigrationPlan(expected, actual StateMigrationPlan) bool {
	return expected.Version == actual.Version &&
		expected.Needed == actual.Needed &&
		expected.Kind == actual.Kind &&
		expected.Source == actual.Source &&
		expected.Destination == actual.Destination &&
		expected.JournalPath == actual.JournalPath
}

func validatePrivateState(root string, expectedUID, expectedGID int) error {
	if _, _, err := edgeclient.LoadIdentity(root); err != nil {
		return migrationErr(MigrationErrorStateInvalid, err)
	}
	for _, path := range []string{root, filepath.Join(root, "identity.json"), filepath.Join(root, "device.key")} {
		if err := requireOwnedPath(path, expectedUID, expectedGID); err != nil {
			return migrationErr(MigrationErrorOwnerMismatch, err)
		}
	}
	return nil
}

func ensurePrivateMigrationParent(plan StateMigrationPlan, expectedUID, expectedGID int) error {
	parent := filepath.Dir(plan.Destination)
	if linked, err := hasSymlinkAncestor(plan.Destination); err != nil || linked {
		if err == nil {
			err = errors.New("migration destination contains a symlink ancestor")
		}
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return migrationErr(MigrationErrorPrepareFailed, errors.New("migration parent is unsafe"))
	}
	if err := requireOwnedPath(parent, expectedUID, expectedGID); err != nil {
		return migrationErr(MigrationErrorOwnerMismatch, err)
	}
	if _, err := os.Lstat(plan.Destination); err == nil {
		return migrationErr(MigrationErrorPlanChanged, errors.New("migration destination appeared"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	return nil
}

func newMigrationJournal(plan StateMigrationPlan, stage MigrationStage, now time.Time) migrationJournal {
	return migrationJournal{
		Version:     plan.Version,
		Kind:        plan.Kind,
		Source:      plan.Source,
		Destination: plan.Destination,
		Stage:       stage,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
}

func validMigrationJournal(plan StateMigrationPlan, journal migrationJournal) bool {
	if journal.Version != plan.Version || journal.Kind != MigrationLegacyToPreferred ||
		journal.Source != plan.Source || journal.Destination != plan.Destination ||
		journal.CreatedAt.IsZero() || journal.UpdatedAt.IsZero() {
		return false
	}
	switch journal.Stage {
	case MigrationStagePrepared, MigrationStageRenamed, MigrationStageVerified:
		return true
	default:
		return false
	}
}

func writeMigrationJournal(path string, journal migrationJournal, expectedUID, expectedGID int, exclusive bool) error {
	if existing, err := os.Lstat(path); err == nil {
		if exclusive {
			return migrationErr(MigrationErrorJournalExists, errors.New("state migration journal already exists"))
		}
		if !existing.Mode().IsRegular() || existing.Mode().Perm()&0o077 != 0 {
			return migrationErr(MigrationErrorJournalInvalid, errors.New("state migration journal is unsafe"))
		}
		if err := requireOwnedPath(path, expectedUID, expectedGID); err != nil {
			return migrationErr(MigrationErrorOwnerMismatch, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return migrationErr(MigrationErrorJournalInvalid, err)
	}

	content, err := json.Marshal(journal)
	if err != nil {
		return migrationErr(MigrationErrorJournalInvalid, err)
	}
	content = append(content, '\n')
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, ".mcp-edge-migration-*")
	if err != nil {
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	if err := temporary.Close(); err != nil {
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	if err := requireOwnedPath(temporaryPath, expectedUID, expectedGID); err != nil {
		return migrationErr(MigrationErrorOwnerMismatch, err)
	}
	if exclusive {
		err = renameNoReplace(temporaryPath, path)
	} else {
		err = os.Rename(temporaryPath, path)
	}
	if err != nil {
		if exclusive && !errors.Is(err, os.ErrExist) {
			return migrationErr(MigrationErrorPrepareFailed, err)
		}
		if exclusive {
			return migrationErr(MigrationErrorJournalExists, err)
		}
		return migrationErr(MigrationErrorJournalInvalid, err)
	}
	if err := syncDirectory(parent); err != nil {
		return migrationErr(MigrationErrorPrepareFailed, err)
	}
	return nil
}

func readMigrationJournal(path string, expectedUID, expectedGID int) (migrationJournal, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxMigrationJournalBytes {
		return migrationJournal{}, migrationErr(MigrationErrorJournalInvalid, errors.New("state migration journal is unavailable or unsafe"))
	}
	if err := requireOwnedPath(path, expectedUID, expectedGID); err != nil {
		return migrationJournal{}, migrationErr(MigrationErrorOwnerMismatch, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) > maxMigrationJournalBytes {
		return migrationJournal{}, migrationErr(MigrationErrorJournalInvalid, err)
	}
	var journal migrationJournal
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return migrationJournal{}, migrationErr(MigrationErrorJournalInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return migrationJournal{}, migrationErr(MigrationErrorJournalInvalid, errors.New("state migration journal has trailing data"))
	}
	return journal, nil
}

func verifyMigratedState(plan StateMigrationPlan, expectedUID, expectedGID int) error {
	if _, err := os.Lstat(plan.Source); !errors.Is(err, os.ErrNotExist) {
		return errors.New("legacy state still exists after migration")
	}
	return validatePrivateState(plan.Destination, expectedUID, expectedGID)
}

func rollbackStateMigration(plan StateMigrationPlan, expectedUID, expectedGID int) error {
	sourceExists, sourceUnsafe, err := migrationPathState(plan.Source)
	if err != nil {
		return err
	}
	destinationExists, destinationUnsafe, err := migrationPathState(plan.Destination)
	if err != nil {
		return err
	}
	if sourceUnsafe || destinationUnsafe || sourceExists == destinationExists {
		return errors.New("migration rollback state is ambiguous")
	}
	if destinationExists {
		if err := renameNoReplace(plan.Destination, plan.Source); err != nil {
			return err
		}
	}
	if err := validatePrivateState(plan.Source, expectedUID, expectedGID); err != nil {
		return err
	}
	return removeMigrationJournal(plan.JournalPath)
}

func migrationPathState(path string) (exists, unsafe bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, info.Mode()&os.ModeSymlink != 0 || !info.IsDir(), nil
}

func removeMigrationJournal(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return migrationErr(MigrationErrorJournalInvalid, err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return migrationErr(MigrationErrorJournalInvalid, err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func callMigrationHook(hook func() error) error {
	if hook == nil {
		return nil
	}
	return hook()
}

func migrationErr(code MigrationErrorCode, err error) error {
	if err == nil {
		err = errors.New(strings.ReplaceAll(string(code), "_", " "))
	}
	return &MigrationError{Code: code, Err: err}
}

func validateMigrationPathAncestors(plan StateMigrationPlan) error {
	for _, path := range []string{plan.Source, plan.Destination, plan.JournalPath} {
		linked, err := hasSymlinkAncestor(path)
		if err != nil {
			return migrationErr(MigrationErrorInventoryBlocked, err)
		}
		if linked {
			return migrationErr(MigrationErrorInventoryBlocked, errors.New("migration path contains a symlink ancestor"))
		}
	}
	return nil
}

func migrationCode(err error) MigrationErrorCode {
	var migrationFailure *MigrationError
	if errors.As(err, &migrationFailure) {
		return migrationFailure.Code
	}
	return ""
}
