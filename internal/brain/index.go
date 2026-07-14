package brain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
	_ "modernc.org/sqlite"
)

const (
	IndexFileName            = "brain.db"
	IndexSchemaVersion       = 1
	MaxIndexedNotes          = 10_000
	MaxAggregateSourceBytes  = int64(64 << 20)
	MaxQueryBytes            = 256
	MaxQueryTerms            = 32
	DefaultTopK              = 5
	MaxTopK                  = 20
	MaxExcerptBytes          = 480
	MaxSearchProvenanceBytes = 240
	MaxSearchResponseBytes   = 16 << 10
	MaxBacklinks             = 128
	indexQueryTimeout        = 5 * time.Second
	indexRebuildTimeout      = 45 * time.Second
)

// SearchResult is the bounded public-safe result shape used by the later Brain tools.
type SearchResult struct {
	Slug       string     `json:"slug"`
	Trust      TrustLevel `json:"trust"`
	Title      string     `json:"title"`
	Type       NoteType   `json:"type"`
	Author     string     `json:"author"`
	Updated    string     `json:"updated"`
	ReviewBy   string     `json:"review_by,omitempty"`
	Expired    bool       `json:"expired"`
	Provenance string     `json:"provenance"`
	Excerpt    string     `json:"excerpt"`
	Score      float64    `json:"score"`
}

// IndexStatus describes only bounded derived cache state, never private paths.
type IndexStatus struct {
	Ready           bool   `json:"ready"`
	SchemaVersion   int    `json:"schema_version"`
	NoteCount       int    `json:"note_count"`
	SourceBytes     int64  `json:"source_bytes"`
	LinkCount       int    `json:"link_count"`
	BrokenLinkCount int    `json:"broken_link_count"`
	IndexedAt       string `json:"indexed_at,omitempty"`
}

type Index struct {
	db   *sql.DB
	path string
	now  func() time.Time
}

type indexedSource struct {
	note        Note
	sourceBytes int64
}

// OpenIndex creates or verifies the private disposable SQLite/FTS5 cache.
func (s *Store) OpenIndex(ctx context.Context) error {
	if s == nil || s.jail == nil {
		return errors.New("brain: store is unavailable")
	}
	if ctx == nil {
		return errors.New("brain: context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.index != nil {
		return nil
	}
	index, err := openIndex(ctx, s.root, s.now)
	if err != nil {
		return err
	}
	s.index = index
	return nil
}

func openIndex(ctx context.Context, root string, now func() time.Time) (*Index, error) {
	cacheDirectory := filepath.Join(root, CacheDir)
	if err := ensurePrivateDirectory(cacheDirectory); err != nil {
		return nil, err
	}
	path := filepath.Join(cacheDirectory, IndexFileName)
	if err := preparePrivateCache(path); err != nil {
		return nil, err
	}
	cacheURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	parameters := cacheURL.Query()
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "foreign_keys(1)")
	parameters.Add("_pragma", "trusted_schema(OFF)")
	cacheURL.RawQuery = parameters.Encode()
	database, err := sql.Open("sqlite", cacheURL.String())
	if err != nil {
		return nil, errors.New("brain: SQLite cache could not be opened")
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	index := &Index{db: database, path: path, now: now}
	queryContext, cancel := boundedContext(ctx, indexRebuildTimeout)
	defer cancel()
	if err := index.initialize(queryContext); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := verifyPrivateCache(path); err != nil {
		_ = database.Close()
		return nil, err
	}
	return index, nil
}

func preparePrivateCache(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return errors.New("brain: SQLite cache could not be created")
		}
		if closeErr := file.Close(); closeErr != nil {
			return errors.New("brain: SQLite cache could not be closed")
		}
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("brain: SQLite cache must be a private regular file")
	}
	return nil
}

func verifyPrivateCache(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("brain: SQLite cache permissions are unsafe")
	}
	return nil
}

func (i *Index) initialize(ctx context.Context) error {
	if i == nil || i.db == nil {
		return errors.New("brain: SQLite cache is unavailable")
	}
	for _, statement := range []string{
		"PRAGMA journal_mode = DELETE",
		"PRAGMA synchronous = FULL",
		"PRAGMA temp_store = MEMORY",
	} {
		if _, err := i.db.ExecContext(ctx, statement); err != nil {
			return errors.New("brain: SQLite security configuration failed")
		}
	}
	schema := []string{
		`CREATE TABLE IF NOT EXISTS brain_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS notes (
			slug TEXT PRIMARY KEY,
			trust TEXT NOT NULL,
			title TEXT NOT NULL,
			note_type TEXT NOT NULL,
			author TEXT NOT NULL,
			created TEXT NOT NULL,
			updated TEXT NOT NULL,
			provenance TEXT NOT NULL,
			review_by TEXT NOT NULL,
			expired INTEGER NOT NULL CHECK (expired IN (0,1)),
			body TEXT NOT NULL,
			source_bytes INTEGER NOT NULL CHECK (source_bytes >= 0)
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS links (
			source_slug TEXT NOT NULL,
			target_slug TEXT NOT NULL,
			PRIMARY KEY (source_slug, target_slug),
			FOREIGN KEY (source_slug) REFERENCES notes(slug) ON DELETE CASCADE
		) STRICT`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
			slug UNINDEXED,
			title,
			body,
			tokenize = 'unicode61 remove_diacritics 2'
		)`,
	}
	for _, statement := range schema {
		if _, err := i.db.ExecContext(ctx, statement); err != nil {
			return errors.New("brain: SQLite/FTS5 schema initialization failed")
		}
	}
	if _, err := i.db.ExecContext(ctx, `INSERT INTO brain_meta(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO NOTHING`, IndexSchemaVersion); err != nil {
		return errors.New("brain: SQLite schema version initialization failed")
	}
	var version int
	if err := i.db.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM brain_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != IndexSchemaVersion {
		return errors.New("brain: SQLite cache schema version is unsupported")
	}
	var check string
	if err := i.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return errors.New("brain: SQLite cache integrity check failed")
	}
	return nil
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

// Close releases the disposable cache. Markdown and local Git remain untouched.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.index == nil {
		return nil
	}
	err := s.index.db.Close()
	s.index = nil
	if err != nil {
		return errors.New("brain: SQLite cache close failed")
	}
	return nil
}

func (s *Store) withIndex(fn func(*Index) error) error {
	if s == nil {
		return errors.New("brain: store is unavailable")
	}
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.index == nil {
		return errors.New("brain: SQLite cache is not open")
	}
	return fn(s.index)
}

// Reindex atomically replaces the cache from strict Markdown truth.
func (s *Store) Reindex(ctx context.Context) (IndexStatus, error) {
	if s == nil || s.jail == nil {
		return IndexStatus{}, errors.New("brain: store is unavailable")
	}
	if ctx == nil {
		return IndexStatus{}, errors.New("brain: context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sources, err := s.scanSources()
	if err != nil {
		return IndexStatus{}, err
	}
	var status IndexStatus
	err = s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexRebuildTimeout)
		defer cancel()
		var replaceErr error
		status, replaceErr = index.replaceAll(queryContext, sources)
		return replaceErr
	})
	return status, err
}

func (s *Store) scanSources() ([]indexedSource, error) {
	sources := make([]indexedSource, 0)
	seen := make(map[string]TrustLevel)
	var aggregate int64
	for _, trust := range []TrustLevel{TrustCurated, TrustWorking} {
		directoryName, _ := trustDirectory(trust)
		directory := filepath.Join(s.root, directoryName)
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, errors.New("brain: source directory could not be read")
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				return nil, errors.New("brain: source directories may contain only regular Markdown notes")
			}
			slug := strings.TrimSuffix(entry.Name(), ".md")
			if err := ValidateSlug(slug); err != nil {
				return nil, err
			}
			if _, exists := seen[slug]; exists {
				return nil, errors.New("brain: duplicate slug exists in curated and working")
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > MaxFileBytes {
				return nil, errors.New("brain: source note permissions or size are invalid")
			}
			aggregate += info.Size()
			if err := validateIndexBounds(len(sources)+1, aggregate); err != nil {
				return nil, err
			}
			note, err := s.ReadSource(trust, slug)
			if err != nil {
				return nil, err
			}
			seen[slug] = trust
			sources = append(sources, indexedSource{note: sanitizeIndexedNote(note), sourceBytes: info.Size()})
		}
	}
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].note.Metadata.Slug < sources[right].note.Metadata.Slug
	})
	return sources, nil
}

func validateIndexBounds(notes int, sourceBytes int64) error {
	if notes < 0 || notes > MaxIndexedNotes {
		return fmt.Errorf("brain: indexed note count exceeds %d", MaxIndexedNotes)
	}
	if sourceBytes < 0 || sourceBytes > MaxAggregateSourceBytes {
		return fmt.Errorf("brain: indexed source bytes exceed %d", MaxAggregateSourceBytes)
	}
	return nil
}

func sanitizeIndexedNote(note Note) Note {
	note.Metadata.Title, _ = policy.Redact(note.Metadata.Title)
	note.Metadata.Provenance, _ = policy.Redact(note.Metadata.Provenance)
	note.Body, _ = policy.Redact(note.Body)
	return note
}

func (i *Index) replaceAll(ctx context.Context, sources []indexedSource) (IndexStatus, error) {
	transaction, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return IndexStatus{}, errors.New("brain: SQLite rebuild transaction could not start")
	}
	defer transaction.Rollback()
	for _, statement := range []string{"DELETE FROM links", "DELETE FROM notes_fts", "DELETE FROM notes"} {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return IndexStatus{}, errors.New("brain: SQLite rebuild reset failed")
		}
	}
	for _, source := range sources {
		if err := upsertNoteTx(ctx, transaction, source.note, source.sourceBytes); err != nil {
			return IndexStatus{}, err
		}
	}
	indexedAt := i.now().UTC().Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx, `INSERT INTO brain_meta(key,value) VALUES('indexed_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, indexedAt); err != nil {
		return IndexStatus{}, errors.New("brain: SQLite rebuild metadata failed")
	}
	if err := transaction.Commit(); err != nil {
		return IndexStatus{}, errors.New("brain: SQLite rebuild commit failed")
	}
	return i.status(ctx)
}

func upsertNoteTx(ctx context.Context, transaction *sql.Tx, note Note, sourceBytes int64) error {
	note = sanitizeIndexedNote(note)
	expired := 0
	if note.Expired {
		expired = 1
	}
	metadata := note.Metadata
	if _, err := transaction.ExecContext(ctx, `INSERT INTO notes(
		slug,trust,title,note_type,author,created,updated,provenance,review_by,expired,body,source_bytes
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(slug) DO UPDATE SET
		trust=excluded.trust,title=excluded.title,note_type=excluded.note_type,
		author=excluded.author,created=excluded.created,updated=excluded.updated,
		provenance=excluded.provenance,review_by=excluded.review_by,
		expired=excluded.expired,body=excluded.body,source_bytes=excluded.source_bytes`,
		metadata.Slug, note.Trust, metadata.Title, metadata.Type, metadata.Author,
		metadata.Created, metadata.Updated, metadata.Provenance, metadata.ReviewBy,
		expired, note.Body, sourceBytes,
	); err != nil {
		return errors.New("brain: SQLite note update failed")
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM notes_fts WHERE slug=?`, metadata.Slug); err != nil {
		return errors.New("brain: SQLite FTS update failed")
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO notes_fts(slug,title,body) VALUES(?,?,?)`, metadata.Slug, metadata.Title, note.Body); err != nil {
		return errors.New("brain: SQLite FTS update failed")
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM links WHERE source_slug=?`, metadata.Slug); err != nil {
		return errors.New("brain: SQLite link update failed")
	}
	for _, target := range note.Links {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO links(source_slug,target_slug) VALUES(?,?)`, metadata.Slug, target); err != nil {
			return errors.New("brain: SQLite link update failed")
		}
	}
	return nil
}

func (i *Index) upsert(ctx context.Context, note Note, sourceBytes int64) error {
	transaction, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("brain: SQLite incremental transaction could not start")
	}
	defer transaction.Rollback()
	if err := upsertNoteTx(ctx, transaction, note, sourceBytes); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO brain_meta(key,value) VALUES('indexed_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, i.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return errors.New("brain: SQLite incremental metadata failed")
	}
	if err := transaction.Commit(); err != nil {
		return errors.New("brain: SQLite incremental commit failed")
	}
	return nil
}

func (i *Index) delete(ctx context.Context, slug string) error {
	transaction, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("brain: SQLite rollback transaction could not start")
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM notes_fts WHERE slug=?`, slug); err != nil {
		return errors.New("brain: SQLite rollback failed")
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM notes WHERE slug=?`, slug); err != nil {
		return errors.New("brain: SQLite rollback failed")
	}
	if err := transaction.Commit(); err != nil {
		return errors.New("brain: SQLite rollback commit failed")
	}
	return nil
}

// IndexStatus returns derived cache counts only.
func (s *Store) IndexStatus(ctx context.Context) (IndexStatus, error) {
	if ctx == nil {
		return IndexStatus{}, errors.New("brain: context is required")
	}
	var status IndexStatus
	err := s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexQueryTimeout)
		defer cancel()
		var statusErr error
		status, statusErr = index.status(queryContext)
		return statusErr
	})
	return status, err
}

func (i *Index) status(ctx context.Context) (IndexStatus, error) {
	status := IndexStatus{Ready: true, SchemaVersion: IndexSchemaVersion}
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(source_bytes),0) FROM notes`).Scan(&status.NoteCount, &status.SourceBytes); err != nil {
		return IndexStatus{}, errors.New("brain: SQLite status query failed")
	}
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links`).Scan(&status.LinkCount); err != nil {
		return IndexStatus{}, errors.New("brain: SQLite status query failed")
	}
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links l LEFT JOIN notes n ON n.slug=l.target_slug WHERE n.slug IS NULL`).Scan(&status.BrokenLinkCount); err != nil {
		return IndexStatus{}, errors.New("brain: SQLite status query failed")
	}
	_ = i.db.QueryRowContext(ctx, `SELECT value FROM brain_meta WHERE key='indexed_at'`).Scan(&status.IndexedAt)
	return status, nil
}

// Search performs bounded plain-text FTS5 BM25 retrieval.
func (s *Store) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if ctx == nil {
		return nil, errors.New("brain: context is required")
	}
	ftsQuery, terms, err := plainTextFTSQuery(query)
	if err != nil {
		return nil, err
	}
	if topK == 0 {
		topK = DefaultTopK
	}
	if topK < 1 || topK > MaxTopK {
		return nil, fmt.Errorf("brain: top_k must be between 1 and %d", MaxTopK)
	}
	results := make([]SearchResult, 0, topK)
	err = s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexQueryTimeout)
		defer cancel()
		rows, err := index.db.QueryContext(queryContext, `SELECT
			n.slug,n.trust,n.title,n.note_type,n.author,n.updated,n.review_by,n.expired,
			n.provenance,n.body,bm25(notes_fts,0.0,5.0,1.0) AS score
		FROM notes_fts JOIN notes n ON n.slug=notes_fts.slug
		WHERE notes_fts MATCH ?
		ORDER BY score ASC, CASE n.trust WHEN 'curated' THEN 0 ELSE 1 END, n.slug ASC
		LIMIT ?`, ftsQuery, topK)
		if err != nil {
			return errors.New("brain: SQLite search failed")
		}
		defer rows.Close()
		for rows.Next() {
			var result SearchResult
			var expired int
			var body string
			if err := rows.Scan(&result.Slug, &result.Trust, &result.Title, &result.Type,
				&result.Author, &result.Updated, &result.ReviewBy, &expired,
				&result.Provenance, &body, &result.Score); err != nil {
				return errors.New("brain: SQLite search result failed")
			}
			result.Expired = expired == 1
			result.Provenance = truncateUTF8(strings.TrimSpace(result.Provenance), MaxSearchProvenanceBytes)
			result.Excerpt = makeExcerpt(body, terms)
			candidate := append(results, result)
			encoded, err := json.Marshal(candidate)
			if err != nil {
				return errors.New("brain: search result encoding failed")
			}
			if len(encoded) > MaxSearchResponseBytes {
				break
			}
			results = candidate
		}
		if err := rows.Err(); err != nil {
			return errors.New("brain: SQLite search iteration failed")
		}
		return nil
	})
	return results, err
}

func plainTextFTSQuery(query string) (string, []string, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > MaxQueryBytes || !utf8.ValidString(query) {
		return "", nil, fmt.Errorf("brain: query must be 1-%d valid UTF-8 bytes", MaxQueryBytes)
	}
	terms := make([]string, 0)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		terms = append(terms, strings.ToLower(builder.String()))
		builder.Reset()
	}
	for _, value := range query {
		if unicode.IsLetter(value) || unicode.IsNumber(value) || value == '_' {
			builder.WriteRune(value)
		} else {
			flush()
		}
	}
	flush()
	if len(terms) == 0 || len(terms) > MaxQueryTerms {
		return "", nil, errors.New("brain: query contains no usable terms or too many terms")
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, "\""+term+"\"")
	}
	return strings.Join(quoted, " AND "), terms, nil
}

func makeExcerpt(body string, terms []string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	match := -1
	lower := strings.ToLower(body)
	for _, term := range terms {
		if position := strings.Index(lower, strings.ToLower(term)); position >= 0 && (match < 0 || position < match) {
			match = position
		}
	}
	start := match
	if start < 0 {
		start = 0
	}
	if start > MaxExcerptBytes/3 {
		start -= MaxExcerptBytes / 3
	} else {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	fragment := body[start:]
	fragment = truncateUTF8(fragment, MaxExcerptBytes)
	if start > 0 {
		fragment = "…" + truncateUTF8(fragment, MaxExcerptBytes-len("…"))
	}
	if start+len(fragment) < len(body) {
		fragment = truncateUTF8(fragment, MaxExcerptBytes-len("…")) + "…"
	}
	return fragment
}

func truncateUTF8(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	if len(value) <= maximum {
		return value
	}
	end := maximum
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

// Backlinks returns at most MaxBacklinks source slugs in deterministic order.
func (s *Store) Backlinks(ctx context.Context, slug string) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("brain: context is required")
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	backlinks := make([]string, 0)
	err := s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexQueryTimeout)
		defer cancel()
		rows, err := index.db.QueryContext(queryContext, `SELECT source_slug FROM links WHERE target_slug=? ORDER BY source_slug LIMIT ?`, slug, MaxBacklinks)
		if err != nil {
			return errors.New("brain: SQLite backlink query failed")
		}
		defer rows.Close()
		for rows.Next() {
			var source string
			if err := rows.Scan(&source); err != nil {
				return errors.New("brain: SQLite backlink result failed")
			}
			backlinks = append(backlinks, source)
		}
		if err := rows.Err(); err != nil {
			return errors.New("brain: SQLite backlink iteration failed")
		}
		return nil
	})
	return backlinks, err
}
