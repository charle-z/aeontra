// Package resultstore persists redacted, bounded tool output behind opaque refs.
package resultstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
	_ "modernc.org/sqlite"
)

const (
	SuccessTTL        = 24 * time.Hour
	FailureTTL        = 7 * 24 * time.Hour
	DefaultQuotaBytes = int64(256 << 20)
	MaxFragmentBytes  = 16 << 10
	MaxSummaryRunes   = 240
	MaxFindLimit      = 20
	MaxStages         = 32
	databaseFilename  = "results.db"
)

type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
)

type Config struct {
	Root       string
	QuotaBytes int64
	Now        func() time.Time
}

type Input struct {
	Status     Status
	Summary    string
	ExitStatus int
	Content    string
	Stages     []StageInput
}

type StageInput struct {
	Name   string
	Status Status
	Start  int64
	End    int64
}

type Stage struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Start  int64  `json:"-"`
	End    int64  `json:"-"`
}

type persistedStage struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
}

type Metadata struct {
	Status      Status    `json:"status"`
	Summary     string    `json:"summary"`
	Stages      []Stage   `json:"stages"`
	ExitStatus  int       `json:"exit_status"`
	OutputBytes int64     `json:"output_bytes"`
	ResultRef   string    `json:"result_ref"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Fragment struct {
	Metadata
	Fragment   string `json:"fragment"`
	NextOffset int64  `json:"next_offset"`
}

type StageFragment struct {
	Metadata
	Stage      Stage  `json:"stage"`
	Fragment   string `json:"fragment"`
	NextOffset int64  `json:"next_offset"`
}

func (f Fragment) JSON() string {
	data, _ := json.Marshal(f)
	return string(data)
}

type Store struct {
	mu         sync.Mutex
	db         *sql.DB
	root       string
	quotaBytes int64
	now        func() time.Time
}

var opaqueRefPattern = regexp.MustCompile(`^rs_[a-f0-9]{32}$`)
var safeStagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

func Open(cfg Config) (*Store, error) {
	root := filepath.Clean(strings.TrimSpace(cfg.Root))
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("result store root must be absolute")
	}
	quota := cfg.QuotaBytes
	if quota == 0 {
		quota = DefaultQuotaBytes
	}
	if quota < 1024 || quota > DefaultQuotaBytes {
		return nil, errors.New("result store quota is invalid")
	}
	if err := preparePrivateRoot(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, databaseFilename)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("result database is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("result database is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("result database is unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, root: root, quotaBytes: quota, now: cfg.Now}
	if store.now == nil {
		store.now = time.Now
	}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("result database permissions could not be secured")
	}
	if err := store.Cleanup(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize() error {
	statements := []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=65536`,
		`CREATE TABLE IF NOT EXISTS results (
			result_ref TEXT PRIMARY KEY,
			status TEXT NOT NULL CHECK(status IN ('success','failure')),
			summary TEXT NOT NULL,
			stages_json TEXT NOT NULL,
			exit_status INTEGER NOT NULL,
			output_bytes INTEGER NOT NULL,
			content TEXT NOT NULL,
			content_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS results_expiry ON results(expires_at)`,
		`CREATE INDEX IF NOT EXISTS results_created ON results(created_at)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("result database initialization failed")
		}
	}
	return nil
}

func (s *Store) Put(input Input) (Metadata, error) {
	if input.Status != StatusSuccess && input.Status != StatusFailure {
		return Metadata{}, errors.New("result status is invalid")
	}
	redacted, _ := policy.Redact(input.Content)
	contentBytes := int64(len([]byte(redacted)))
	if contentBytes > s.quotaBytes {
		return Metadata{}, errors.New("result exceeds store quota")
	}
	stages, err := normalizeStages(input.Stages, contentBytes, input.Status)
	if err != nil {
		return Metadata{}, err
	}
	ref, err := newRef()
	if err != nil {
		return Metadata{}, errors.New("result reference generation failed")
	}
	now := s.now().UTC()
	ttl := SuccessTTL
	if input.Status == StatusFailure {
		ttl = FailureTTL
	}
	meta := Metadata{
		Status: input.Status, Summary: safeSummary(input.Summary), Stages: stages,
		ExitStatus: input.ExitStatus, OutputBytes: int64(len([]byte(input.Content))),
		ResultRef: ref, ExpiresAt: now.Add(ttl),
	}
	stagesJSON, _ := json.Marshal(persistedStages(stages))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupLocked(now); err != nil {
		return Metadata{}, err
	}
	if err := s.makeRoomLocked(contentBytes); err != nil {
		return Metadata{}, err
	}
	_, err = s.db.Exec(`INSERT INTO results(result_ref,status,summary,stages_json,exit_status,output_bytes,content,content_bytes,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, ref, input.Status, meta.Summary, string(stagesJSON), input.ExitStatus,
		meta.OutputBytes, redacted, contentBytes, now.Unix(), meta.ExpiresAt.Unix())
	if err != nil {
		return Metadata{}, errors.New("result persistence failed")
	}
	return meta, nil
}

func persistedStages(stages []Stage) []persistedStage {
	result := make([]persistedStage, 0, len(stages))
	for _, stage := range stages {
		result = append(result, persistedStage(stage))
	}
	return result
}

func decodePersistedStages(encoded string) ([]Stage, error) {
	var persisted []persistedStage
	if err := json.Unmarshal([]byte(encoded), &persisted); err != nil {
		return nil, err
	}
	stages := make([]Stage, 0, len(persisted))
	for _, stage := range persisted {
		stages = append(stages, Stage(stage))
	}
	return stages, nil
}

func normalizeStages(input []StageInput, contentBytes int64, fallback Status) ([]Stage, error) {
	if len(input) == 0 {
		input = []StageInput{{Name: "result", Status: fallback, Start: 0, End: contentBytes}}
	}
	if len(input) > MaxStages {
		return nil, errors.New("too many result stages")
	}
	stages := make([]Stage, 0, len(input))
	for _, candidate := range input {
		name := strings.ToLower(strings.TrimSpace(candidate.Name))
		if !safeStagePattern.MatchString(name) {
			name = "redacted"
		}
		status := candidate.Status
		if status != StatusSuccess && status != StatusFailure {
			status = fallback
		}
		start, end := candidate.Start, candidate.End
		if start < 0 || end < 0 || start > end || end > contentBytes {
			return nil, errors.New("result stage bounds are invalid")
		}
		if start == 0 && end == 0 {
			end = contentBytes
		}
		stages = append(stages, Stage{Name: name, Status: status, Start: start, End: end})
	}
	return stages, nil
}

func safeSummary(value string) string {
	value, _ = policy.Redact(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) > MaxSummaryRunes {
		runes = runes[:MaxSummaryRunes]
	}
	return string(runes)
}

func newRef() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "rs_" + hex.EncodeToString(value[:]), nil
}

func (s *Store) Read(ref string, offset int64, limit int) (Fragment, error) {
	meta, content, err := s.load(ref)
	if err != nil {
		return Fragment{}, err
	}
	fragment, next, err := boundedFragment(content, offset, limit, 0, int64(len([]byte(content))))
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Metadata: meta, Fragment: fragment, NextOffset: next}, nil
}

func (s *Store) ReadStage(ref string, index int, limit int) (StageFragment, error) {
	meta, content, err := s.load(ref)
	if err != nil {
		return StageFragment{}, err
	}
	if index < 0 || index >= len(meta.Stages) {
		return StageFragment{}, errors.New("result stage not found")
	}
	stage := meta.Stages[index]
	fragment, next, err := boundedFragment(content, stage.Start, limit, stage.Start, stage.End)
	if err != nil {
		return StageFragment{}, err
	}
	return StageFragment{Metadata: meta, Stage: stage, Fragment: fragment, NextOffset: next}, nil
}

func boundedFragment(content string, offset int64, limit int, minimum, maximum int64) (string, int64, error) {
	if offset < minimum || offset > maximum {
		return "", 0, errors.New("result offset is invalid")
	}
	if limit <= 0 || limit > MaxFragmentBytes {
		limit = MaxFragmentBytes
	}
	data := []byte(content)
	end := offset + int64(limit)
	if end > maximum {
		end = maximum
	}
	start := offset
	for start < end && !utf8.RuneStart(data[start]) {
		start++
	}
	for end > start && end < int64(len(data)) && !utf8.RuneStart(data[end]) {
		end--
	}
	return string(data[start:end]), end, nil
}

func (s *Store) load(ref string) (Metadata, string, error) {
	if !opaqueRefPattern.MatchString(ref) {
		return Metadata{}, "", errors.New("result reference is invalid")
	}
	var meta Metadata
	var stagesJSON, content string
	var expires int64
	err := s.db.QueryRow(`SELECT status,summary,stages_json,exit_status,output_bytes,content,expires_at FROM results WHERE result_ref=? AND expires_at>?`, ref, s.now().UTC().Unix()).
		Scan(&meta.Status, &meta.Summary, &stagesJSON, &meta.ExitStatus, &meta.OutputBytes, &content, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Metadata{}, "", errors.New("result not found")
	}
	if err != nil {
		return Metadata{}, "", errors.New("result read failed")
	}
	meta.Stages, err = decodePersistedStages(stagesJSON)
	if err != nil {
		return Metadata{}, "", errors.New("result read failed")
	}
	meta.ResultRef = ref
	meta.ExpiresAt = time.Unix(expires, 0).UTC()
	return meta, content, nil
}

func (s *Store) FindExact(query string, limit int) ([]Metadata, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]byte(query)) > MaxFragmentBytes {
		return nil, errors.New("exact result query is invalid")
	}
	if limit <= 0 || limit > MaxFindLimit {
		limit = MaxFindLimit
	}
	if err := s.Cleanup(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT result_ref,status,summary,stages_json,exit_status,output_bytes,expires_at
		FROM results WHERE expires_at>? AND instr(content,?)>0 ORDER BY created_at DESC LIMIT ?`, s.now().UTC().Unix(), query, limit)
	if err != nil {
		return nil, errors.New("result search failed")
	}
	defer rows.Close()
	found := make([]Metadata, 0)
	for rows.Next() {
		var meta Metadata
		var stagesJSON string
		var expires int64
		if err := rows.Scan(&meta.ResultRef, &meta.Status, &meta.Summary, &stagesJSON, &meta.ExitStatus, &meta.OutputBytes, &expires); err != nil {
			return nil, errors.New("result search failed")
		}
		meta.Stages, err = decodePersistedStages(stagesJSON)
		if err != nil {
			return nil, errors.New("result search failed")
		}
		meta.ExpiresAt = time.Unix(expires, 0).UTC()
		found = append(found, meta)
	}
	return found, rows.Err()
}

func (s *Store) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked(s.now().UTC())
}

func (s *Store) cleanupLocked(now time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM results WHERE expires_at<=?`, now.Unix()); err != nil {
		return errors.New("result cleanup failed")
	}
	return nil
}

func (s *Store) makeRoomLocked(incoming int64) error {
	for {
		var used int64
		if err := s.db.QueryRow(`SELECT COALESCE(SUM(content_bytes),0) FROM results`).Scan(&used); err != nil {
			return errors.New("result quota check failed")
		}
		if used+incoming <= s.quotaBytes {
			return nil
		}
		result, err := s.db.Exec(`DELETE FROM results WHERE result_ref=(SELECT result_ref FROM results ORDER BY created_at ASC,result_ref ASC LIMIT 1)`)
		if err != nil {
			return errors.New("result quota eviction failed")
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return errors.New("result quota unavailable")
		}
	}
}

func preparePrivateRoot(root string) error {
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			return errors.New("result store root is not private")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("result store root is unavailable")
	}
	if err := rejectSymlinkAncestors(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New("result store root is unavailable")
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
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
			return errors.New("result store ancestry is unsafe")
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
