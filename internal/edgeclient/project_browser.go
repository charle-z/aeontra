package edgeclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/chromedp/cdproto/network"
	_ "modernc.org/sqlite"
)

type BrowserStep = edge.BrowserStep

type BrowserPageRequest struct {
	ProfilePath       string
	NetworkScope      string
	InitialOrigin     string
	CurrentURL        string
	Steps             []BrowserStep
	Capture           string
	FullPage          bool
	ViewportWidth     int
	ViewportHeight    int
	IgnoreHTTPSErrors bool
	TimeoutSeconds    int
	Cookies           []*network.CookieParam
}

type BrowserPageResult struct {
	URL        string
	Title      string
	Text       string
	Screenshot []byte
	Cookies    []*network.CookieParam
}

type BrowserRunner interface {
	Run(context.Context, BrowserPageRequest) (BrowserPageResult, error)
}

type ProjectBrowserManagerConfig struct {
	Root   string
	Runner BrowserRunner
	Now    func() time.Time
	NewID  func(string) (string, error)
}

type ProjectBrowserManager struct {
	db                              *sql.DB
	root, profileRoot, artifactRoot string
	runner                          BrowserRunner
	now                             func() time.Time
	newID                           func(string) (string, error)
	mu                              sync.Mutex
}

type ProjectBrowserCreateRequest struct {
	IdempotencyKey                string
	Resolution                    ProjectResolution
	NetworkScope, InitialURL      string
	ViewportWidth, ViewportHeight int
	IgnoreHTTPSErrors             bool
	Cookies                       []*network.CookieParam
}

type ProjectBrowserReadRequest struct {
	Resolution ProjectResolution
	SessionID  string
}
type ProjectBrowserListRequest struct {
	Resolution ProjectResolution
	Limit      int
}
type ProjectBrowserRunRequest struct {
	OperationID, IdempotencyKey string
	Resolution                  ProjectResolution
	SessionID                   string
	Steps                       []BrowserStep
	Capture                     string
	FullPage                    bool
	TimeoutSeconds              int
}
type ProjectBrowserArtifactReadRequest struct {
	Resolution            ProjectResolution
	SessionID, ArtifactID string
	Offset                int64
	Limit                 int
}
type ProjectBrowserCloseRequest struct {
	Resolution ProjectResolution
	SessionID  string
}
type ProjectBrowserCleanupRequest struct {
	Resolution ProjectResolution
	SessionID  string
}

type ProjectBrowserSnapshot struct {
	SessionID, State, NetworkScope, SafeURL, Title string
	Revision                                       uint64
	CreatedAt, UpdatedAt                           string
	Text                                           string
	TextTruncated                                  bool
	ArtifactID, ArtifactMediaType                  string
	ArtifactBytes                                  int64
	ArtifactSHA256                                 string
}

type ProjectBrowserArtifactChunk struct {
	SessionID, ArtifactID, MediaType, SHA256, DataBase64 string
	Bytes, Offset, Next                                  int64
	EOF                                                  bool
}

type ProjectBrowserCleanupResult struct{ Removed, Artifacts int }

type browserSessionRecord struct {
	SessionID, IdempotencyKey, RequestDigest, WorkspaceID, Alias, Target, NetworkScope, InitialOrigin, CurrentURL, SafeURL, Title, State, ProfilePath string
	ViewportWidth, ViewportHeight                                                                                                                     int
	IgnoreHTTPSErrors                                                                                                                                 bool
	Cookies                                                                                                                                           []*network.CookieParam
	Revision                                                                                                                                          uint64
	CreatedAt, UpdatedAt                                                                                                                              time.Time
}

var browserOperationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
var browserManagedSessionIDPattern = regexp.MustCompile(`^br_[a-f0-9]{32}$`)
var browserManagedArtifactIDPattern = regexp.MustCompile(`^ba_[a-f0-9]{32}$`)

type browserArtifactRecord struct {
	ArtifactID, SessionID, MediaType, SHA256, Path string
	Bytes                                          int64
	CreatedAt                                      time.Time
}

func OpenProjectBrowserManager(config ProjectBrowserManagerConfig) (*ProjectBrowserManager, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Runner == nil {
		return nil, errors.New("project browser manager contract is invalid")
	}
	root := filepath.Clean(config.Root)
	for _, dir := range []string{root, filepath.Join(root, "profiles"), filepath.Join(root, "artifacts")} {
		if err := ensurePrivateBrowserDirectory(dir); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "browser.db"))
	if err != nil {
		return nil, errors.New("project browser journal unavailable")
	}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; CREATE TABLE IF NOT EXISTS browser_sessions(
		session_id TEXT PRIMARY KEY,idempotency_key TEXT NOT NULL UNIQUE,request_digest TEXT NOT NULL,workspace_id TEXT NOT NULL,project_alias TEXT NOT NULL,target_alias TEXT NOT NULL,
		network_scope TEXT NOT NULL,initial_origin TEXT NOT NULL,current_url TEXT NOT NULL,safe_url TEXT NOT NULL,title TEXT NOT NULL,state TEXT NOT NULL,
		profile_path TEXT NOT NULL,viewport_width INTEGER NOT NULL,viewport_height INTEGER NOT NULL,ignore_https_errors INTEGER NOT NULL,cookies_json BLOB NOT NULL,revision INTEGER NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
		CREATE TABLE IF NOT EXISTS browser_artifacts(artifact_id TEXT PRIMARY KEY,session_id TEXT NOT NULL,media_type TEXT NOT NULL,bytes INTEGER NOT NULL,sha256 TEXT NOT NULL,path TEXT NOT NULL,created_at INTEGER NOT NULL,FOREIGN KEY(session_id) REFERENCES browser_sessions(session_id) ON DELETE CASCADE);
		CREATE UNIQUE INDEX IF NOT EXISTS browser_sessions_idempotency ON browser_sessions(workspace_id,project_alias,target_alias,idempotency_key); CREATE INDEX IF NOT EXISTS browser_sessions_project ON browser_sessions(workspace_id,project_alias,target_alias,updated_at); CREATE INDEX IF NOT EXISTS browser_artifacts_session ON browser_artifacts(session_id,created_at); CREATE TABLE IF NOT EXISTS browser_receipts(operation_id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,session_id TEXT NOT NULL,request_digest TEXT NOT NULL,state TEXT NOT NULL,result_json BLOB,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);`); err != nil {
		_ = db.Close()
		return nil, errors.New("project browser journal unavailable")
	}
	if err := os.Chmod(filepath.Join(root, "browser.db"), 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("project browser journal permissions invalid")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = newBrowserOpaqueID
	}
	manager := &ProjectBrowserManager{db: db, root: root, profileRoot: filepath.Join(root, "profiles"), artifactRoot: filepath.Join(root, "artifacts"), runner: config.Runner, now: now, newID: newID}
	_, _ = db.Exec(`UPDATE browser_sessions SET state='ready',revision=revision+1,updated_at=? WHERE state='busy'`, now().UTC().UnixNano())
	_, _ = db.Exec(`UPDATE browser_receipts SET state='indeterminate',updated_at=? WHERE state='running'`, now().UTC().UnixNano())
	return manager, nil
}

func (m *ProjectBrowserManager) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func (m *ProjectBrowserManager) Create(ctx context.Context, request ProjectBrowserCreateRequest) (ProjectBrowserSnapshot, bool, error) {
	if err := validateBrowserResolution(request.Resolution); err != nil {
		return ProjectBrowserSnapshot{}, false, err
	}
	origin := ""
	if request.InitialURL != "" {
		origin = browserOrigin(request.InitialURL)
	}
	if err := ValidateBrowserURL(ctx, request.NetworkScope, origin, request.InitialURL, nil); request.InitialURL != "" && err != nil {
		return ProjectBrowserSnapshot{}, false, err
	}
	digestBody, _ := json.Marshal(struct {
		Workspace, Scope, URL string
		Width, Height         int
		Ignore                bool
	}{request.Resolution.Workspace.ID, request.NetworkScope, request.InitialURL, request.ViewportWidth, request.ViewportHeight, request.IgnoreHTTPSErrors})
	digest := sha256.Sum256(digestBody)
	digestText := hex.EncodeToString(digest[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, err := m.recordByIdempotency(request.Resolution, request.IdempotencyKey); err == nil {
		if existing.RequestDigest != digestText {
			return ProjectBrowserSnapshot{}, false, errors.New("project browser idempotency conflict")
		}
		return snapshotBrowser(existing), true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ProjectBrowserSnapshot{}, false, errors.New("project browser journal unavailable")
	}
	sessionID, err := m.newID("br_")
	if err != nil {
		return ProjectBrowserSnapshot{}, false, err
	}
	profilePath := filepath.Join(m.profileRoot, sessionID)
	if err := ensurePrivateBrowserDirectory(profilePath); err != nil {
		return ProjectBrowserSnapshot{}, false, err
	}
	if err := ensurePrivateBrowserDirectory(filepath.Join(profilePath, "downloads")); err != nil {
		return ProjectBrowserSnapshot{}, false, err
	}
	now := m.now().UTC()
	safe := safeBrowserURL(request.InitialURL)
	record := browserSessionRecord{SessionID: sessionID, IdempotencyKey: request.IdempotencyKey, RequestDigest: digestText, WorkspaceID: request.Resolution.Workspace.ID, Alias: request.Resolution.Project.Alias, Target: request.Resolution.TargetAlias, NetworkScope: request.NetworkScope, InitialOrigin: origin, CurrentURL: request.InitialURL, SafeURL: safe, State: "ready", ProfilePath: profilePath, ViewportWidth: request.ViewportWidth, ViewportHeight: request.ViewportHeight, IgnoreHTTPSErrors: request.IgnoreHTTPSErrors, Revision: 1, CreatedAt: now, UpdatedAt: now}
	_, err = m.db.Exec(`INSERT INTO browser_sessions(session_id,idempotency_key,request_digest,workspace_id,project_alias,target_alias,network_scope,initial_origin,current_url,safe_url,title,state,profile_path,viewport_width,viewport_height,ignore_https_errors,cookies_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.SessionID, record.IdempotencyKey, record.RequestDigest, record.WorkspaceID, record.Alias, record.Target, record.NetworkScope, record.InitialOrigin, record.CurrentURL, record.SafeURL, record.Title, record.State, record.ProfilePath, record.ViewportWidth, record.ViewportHeight, record.IgnoreHTTPSErrors, []byte("[]"), record.Revision, record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano())
	if err != nil {
		return ProjectBrowserSnapshot{}, false, errors.New("project browser journal unavailable")
	}
	return snapshotBrowser(record), false, nil
}

func (m *ProjectBrowserManager) Status(request ProjectBrowserReadRequest) (ProjectBrowserSnapshot, error) {
	record, err := m.boundRecord(request.Resolution, request.SessionID)
	if err != nil {
		return ProjectBrowserSnapshot{}, err
	}
	return snapshotBrowser(record), nil
}
func (m *ProjectBrowserManager) List(request ProjectBrowserListRequest) ([]ProjectBrowserSnapshot, error) {
	if err := validateBrowserResolution(request.Resolution); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit < 1 || limit > edge.MaxBrowserSessions {
		limit = edge.MaxBrowserSessions
	}
	rows, err := m.db.Query(browserSessionSelect+` WHERE workspace_id=? AND project_alias=? AND target_alias=? ORDER BY updated_at DESC LIMIT ?`, request.Resolution.Workspace.ID, request.Resolution.Project.Alias, request.Resolution.TargetAlias, limit)
	if err != nil {
		return nil, errors.New("project browser journal unavailable")
	}
	defer rows.Close()
	out := []ProjectBrowserSnapshot{}
	for rows.Next() {
		r, err := scanBrowserSession(rows)
		if err != nil {
			return nil, errors.New("project browser journal unavailable")
		}
		out = append(out, snapshotBrowser(r))
	}
	return out, rows.Err()
}

func (m *ProjectBrowserManager) Run(ctx context.Context, request ProjectBrowserRunRequest) (ProjectBrowserSnapshot, error) {
	if !browserOperationIDPattern.MatchString(request.OperationID) || request.IdempotencyKey == "" {
		return ProjectBrowserSnapshot{}, errors.New("project browser run identity is invalid")
	}
	digestBody, _ := json.Marshal(struct {
		SessionID string
		Steps     []BrowserStep
		Capture   string
		FullPage  bool
		Timeout   int
	}{request.SessionID, request.Steps, request.Capture, request.FullPage, request.TimeoutSeconds})
	digestSum := sha256.Sum256(digestBody)
	digest := hex.EncodeToString(digestSum[:])

	m.mu.Lock()
	var receiptState, receiptDigest string
	var receiptBody []byte
	receiptErr := m.db.QueryRow(`SELECT state,request_digest,result_json FROM browser_receipts WHERE operation_id=?`, request.OperationID).Scan(&receiptState, &receiptDigest, &receiptBody)
	if receiptErr == nil {
		m.mu.Unlock()
		if receiptDigest != digest {
			return ProjectBrowserSnapshot{}, errors.New("project browser run receipt conflict")
		}
		if receiptState == "succeeded" {
			var result ProjectBrowserSnapshot
			if json.Unmarshal(receiptBody, &result) != nil {
				return ProjectBrowserSnapshot{}, errors.New("project browser run receipt is invalid")
			}
			return result, nil
		}
		return ProjectBrowserSnapshot{}, errors.New("project browser run outcome is indeterminate")
	}
	if !errors.Is(receiptErr, sql.ErrNoRows) {
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser receipt unavailable")
	}
	record, err := m.boundRecord(request.Resolution, request.SessionID)
	if err != nil {
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, err
	}
	if record.State != "ready" {
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser session is not ready")
	}
	now := m.now().UTC()
	tx, err := m.db.Begin()
	if err != nil {
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	if _, err = tx.Exec(`INSERT INTO browser_receipts(operation_id,workspace_id,session_id,request_digest,state,result_json,created_at,updated_at) VALUES(?,?,?,?, 'running',NULL,?,?)`, request.OperationID, request.Resolution.Workspace.ID, record.SessionID, digest, now.UnixNano(), now.UnixNano()); err != nil {
		_ = tx.Rollback()
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser receipt unavailable")
	}
	changed, err := tx.Exec(`UPDATE browser_sessions SET state='busy',revision=revision+1,updated_at=? WHERE session_id=? AND state='ready'`, now.UnixNano(), record.SessionID)
	if err != nil {
		_ = tx.Rollback()
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	count, _ := changed.RowsAffected()
	if count != 1 {
		_ = tx.Rollback()
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser session is not ready")
	}
	if err := tx.Commit(); err != nil {
		m.mu.Unlock()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	record.State = "busy"
	record.Revision++
	record.UpdatedAt = now
	m.mu.Unlock()

	page, runErr := m.runner.Run(ctx, BrowserPageRequest{ProfilePath: record.ProfilePath, NetworkScope: record.NetworkScope, InitialOrigin: record.InitialOrigin, CurrentURL: record.CurrentURL, Steps: request.Steps, Capture: request.Capture, FullPage: request.FullPage, ViewportWidth: record.ViewportWidth, ViewportHeight: record.ViewportHeight, IgnoreHTTPSErrors: record.IgnoreHTTPSErrors, TimeoutSeconds: request.TimeoutSeconds, Cookies: record.Cookies})

	m.mu.Lock()
	defer m.mu.Unlock()
	markIndeterminate := func() {
		stamp := m.now().UTC().UnixNano()
		_, _ = m.db.Exec(`UPDATE browser_sessions SET state='ready',revision=revision+1,updated_at=? WHERE session_id=?`, stamp, record.SessionID)
		_, _ = m.db.Exec(`UPDATE browser_receipts SET state='indeterminate',updated_at=? WHERE operation_id=? AND state='running'`, stamp, request.OperationID)
	}
	if runErr != nil {
		markIndeterminate()
		return ProjectBrowserSnapshot{}, runErr
	}
	if page.URL != "" {
		if err := ValidateBrowserURL(ctx, record.NetworkScope, record.InitialOrigin, page.URL, nil); err != nil {
			markIndeterminate()
			return ProjectBrowserSnapshot{}, err
		}
		record.CurrentURL = page.URL
		record.SafeURL = safeBrowserURL(page.URL)
	}
	record.Title = boundedBrowserText(page.Title, 512)
	record.Cookies = page.Cookies
	text, truncated := boundedBrowserTextWithFlag(page.Text, edge.MaxBrowserTextBytes)
	record.State = "ready"
	record.Revision++
	record.UpdatedAt = m.now().UTC()
	result := snapshotBrowser(record)
	result.Text = text
	result.TextTruncated = truncated
	if len(page.Screenshot) > 0 {
		artifact, err := m.storeArtifact(record.SessionID, page.Screenshot)
		if err != nil {
			markIndeterminate()
			return ProjectBrowserSnapshot{}, err
		}
		result.ArtifactID = artifact.ArtifactID
		result.ArtifactMediaType = artifact.MediaType
		result.ArtifactBytes = artifact.Bytes
		result.ArtifactSHA256 = artifact.SHA256
	}
	cookiesJSON, err := json.Marshal(record.Cookies)
	if err != nil {
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser cookie persistence failed")
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser receipt unavailable")
	}
	tx, err = m.db.Begin()
	if err != nil {
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	if _, err = tx.Exec(`UPDATE browser_sessions SET current_url=?,safe_url=?,title=?,cookies_json=?,state='ready',revision=?,updated_at=? WHERE session_id=?`, record.CurrentURL, record.SafeURL, record.Title, cookiesJSON, record.Revision, record.UpdatedAt.UnixNano(), record.SessionID); err != nil {
		_ = tx.Rollback()
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	if _, err = tx.Exec(`UPDATE browser_receipts SET state='succeeded',result_json=?,updated_at=? WHERE operation_id=? AND state='running'`, resultJSON, record.UpdatedAt.UnixNano(), request.OperationID); err != nil {
		_ = tx.Rollback()
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser receipt unavailable")
	}
	if err = tx.Commit(); err != nil {
		markIndeterminate()
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	return result, nil
}

func (m *ProjectBrowserManager) ReadArtifact(request ProjectBrowserArtifactReadRequest) (ProjectBrowserArtifactChunk, error) {
	if _, err := m.boundRecord(request.Resolution, request.SessionID); err != nil {
		return ProjectBrowserArtifactChunk{}, err
	}
	var a browserArtifactRecord
	var created int64
	err := m.db.QueryRow(`SELECT artifact_id,session_id,media_type,bytes,sha256,path,created_at FROM browser_artifacts WHERE artifact_id=? AND session_id=?`, request.ArtifactID, request.SessionID).Scan(&a.ArtifactID, &a.SessionID, &a.MediaType, &a.Bytes, &a.SHA256, &a.Path, &created)
	if err != nil {
		return ProjectBrowserArtifactChunk{}, errors.New("project browser artifact not found")
	}
	a.CreatedAt = time.Unix(0, created).UTC()
	if request.Offset < 0 || request.Offset > a.Bytes || request.Limit < 1 || request.Limit > edge.MaxBrowserArtifactChunk {
		return ProjectBrowserArtifactChunk{}, errors.New("project browser artifact range invalid")
	}
	file, err := openPrivateBrowserArtifact(a.Path)
	if err != nil {
		return ProjectBrowserArtifactChunk{}, err
	}
	defer file.Close()
	if _, err = file.Seek(request.Offset, io.SeekStart); err != nil {
		return ProjectBrowserArtifactChunk{}, errors.New("project browser artifact unavailable")
	}
	remaining := a.Bytes - request.Offset
	size := int64(request.Limit)
	if remaining < size {
		size = remaining
	}
	buffer := make([]byte, size)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ProjectBrowserArtifactChunk{}, errors.New("project browser artifact unavailable")
	}
	buffer = buffer[:n]
	next := request.Offset + int64(n)
	return ProjectBrowserArtifactChunk{SessionID: a.SessionID, ArtifactID: a.ArtifactID, MediaType: a.MediaType, Bytes: a.Bytes, SHA256: a.SHA256, Offset: request.Offset, Next: next, EOF: next == a.Bytes, DataBase64: base64.StdEncoding.EncodeToString(buffer)}, nil
}

func (m *ProjectBrowserManager) CloseSession(request ProjectBrowserCloseRequest) (ProjectBrowserSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.boundRecord(request.Resolution, request.SessionID)
	if err != nil {
		return ProjectBrowserSnapshot{}, err
	}
	if record.State == "busy" {
		return ProjectBrowserSnapshot{}, errors.New("project browser session is busy")
	}
	if record.State == "closed" {
		return snapshotBrowser(record), nil
	}
	record.State = "closed"
	record.Revision++
	record.UpdatedAt = m.now().UTC()
	_, err = m.db.Exec(`UPDATE browser_sessions SET state='closed',revision=?,updated_at=? WHERE session_id=?`, record.Revision, record.UpdatedAt.UnixNano(), record.SessionID)
	if err != nil {
		return ProjectBrowserSnapshot{}, errors.New("project browser journal unavailable")
	}
	return snapshotBrowser(record), nil
}

func (m *ProjectBrowserManager) Cleanup(request ProjectBrowserCleanupRequest) (ProjectBrowserCleanupResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := validateBrowserResolution(request.Resolution); err != nil {
		return ProjectBrowserCleanupResult{}, err
	}
	query := browserSessionSelect + ` WHERE workspace_id=? AND project_alias=? AND target_alias=? AND state='closed'`
	args := []any{request.Resolution.Workspace.ID, request.Resolution.Project.Alias, request.Resolution.TargetAlias}
	if request.SessionID != "" {
		query += ` AND session_id=?`
		args = append(args, request.SessionID)
	}
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return ProjectBrowserCleanupResult{}, errors.New("project browser journal unavailable")
	}
	records := []browserSessionRecord{}
	for rows.Next() {
		r, e := scanBrowserSession(rows)
		if e != nil {
			rows.Close()
			return ProjectBrowserCleanupResult{}, e
		}
		records = append(records, r)
	}
	rows.Close()
	result := ProjectBrowserCleanupResult{}
	for _, r := range records {
		artifacts, err := m.artifactsForSession(r.SessionID)
		if err != nil {
			return result, err
		}
		for _, a := range artifacts {
			if err := removePrivateBrowserArtifact(a.Path, m.artifactRoot); err != nil {
				return result, err
			}
			result.Artifacts++
		}
		if err := removePrivateBrowserProfile(r.ProfilePath, m.profileRoot, r.SessionID); err != nil {
			return result, err
		}
		if _, err := m.db.Exec(`DELETE FROM browser_sessions WHERE session_id=? AND state='closed'`, r.SessionID); err != nil {
			return result, errors.New("project browser journal unavailable")
		}
		result.Removed++
	}
	return result, nil
}

func ValidateBrowserURL(_ context.Context, scope, _ string, raw string, _ any) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return errors.New("browser URL is invalid")
	}
	if scope != "general" {
		return errors.New("browser network scope is invalid")
	}
	return nil
}

func browserOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}
func safeBrowserURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
func boundedBrowserText(value string, limit int) string {
	v, _ := boundedBrowserTextWithFlag(value, limit)
	return v
}
func boundedBrowserTextWithFlag(value string, limit int) (string, bool) {
	redacted, _ := policy.Redact(value)
	if len(redacted) <= limit {
		return redacted, false
	}
	cut := limit
	for cut > 0 && (redacted[cut]&0xC0) == 0x80 {
		cut--
	}
	return redacted[:cut], true
}

func validateBrowserResolution(r ProjectResolution) error {
	if r.Project.Alias == "" || r.Project.Owner == "" || r.Project.Repository == "" || r.TargetAlias == "" || r.Workspace.ID == "" || !filepath.IsAbs(r.Workspace.Path) || r.Workspace.Profile != WorkspaceProfileLinuxWorkcell || r.Workspace.Mode != WorkspaceModeDev {
		return errors.New("project browser resolution is invalid")
	}
	return nil
}
func ensurePrivateBrowserDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return errors.New("project browser state unavailable")
		}
		return os.Chmod(path, 0o700)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project browser state is unsafe")
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(path, 0o700); err != nil {
			return errors.New("project browser state is unsafe")
		}
	}
	return nil
}
func newBrowserOpaqueID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("project browser identity unavailable")
	}
	return prefix + hex.EncodeToString(b), nil
}

const browserSessionSelect = `SELECT session_id,idempotency_key,request_digest,workspace_id,project_alias,target_alias,network_scope,initial_origin,current_url,safe_url,title,state,profile_path,viewport_width,viewport_height,ignore_https_errors,cookies_json,revision,created_at,updated_at FROM browser_sessions`

type browserSessionScanner interface{ Scan(...any) error }

func scanBrowserSession(row browserSessionScanner) (browserSessionRecord, error) {
	var r browserSessionRecord
	var ignore bool
	var cookies []byte
	var created, updated int64
	err := row.Scan(&r.SessionID, &r.IdempotencyKey, &r.RequestDigest, &r.WorkspaceID, &r.Alias, &r.Target, &r.NetworkScope, &r.InitialOrigin, &r.CurrentURL, &r.SafeURL, &r.Title, &r.State, &r.ProfilePath, &r.ViewportWidth, &r.ViewportHeight, &ignore, &cookies, &r.Revision, &created, &updated)
	if err == nil && json.Unmarshal(cookies, &r.Cookies) != nil {
		return browserSessionRecord{}, errors.New("project browser cookie journal is invalid")
	}
	r.IgnoreHTTPSErrors = ignore
	r.CreatedAt = time.Unix(0, created).UTC()
	r.UpdatedAt = time.Unix(0, updated).UTC()
	return r, err
}
func (m *ProjectBrowserManager) recordByIdempotency(res ProjectResolution, key string) (browserSessionRecord, error) {
	return scanBrowserSession(m.db.QueryRow(browserSessionSelect+` WHERE workspace_id=? AND project_alias=? AND target_alias=? AND idempotency_key=?`, res.Workspace.ID, res.Project.Alias, res.TargetAlias, key))
}
func (m *ProjectBrowserManager) boundRecord(res ProjectResolution, id string) (browserSessionRecord, error) {
	if err := validateBrowserResolution(res); err != nil {
		return browserSessionRecord{}, err
	}
	r, err := scanBrowserSession(m.db.QueryRow(browserSessionSelect+` WHERE session_id=? AND workspace_id=? AND project_alias=? AND target_alias=?`, id, res.Workspace.ID, res.Project.Alias, res.TargetAlias))
	if err != nil {
		return browserSessionRecord{}, errors.New("project browser session not found")
	}
	return r, nil
}
func snapshotBrowser(r browserSessionRecord) ProjectBrowserSnapshot {
	return ProjectBrowserSnapshot{SessionID: r.SessionID, State: r.State, NetworkScope: r.NetworkScope, SafeURL: r.SafeURL, Title: r.Title, Revision: r.Revision, CreatedAt: r.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: r.UpdatedAt.Format(time.RFC3339Nano)}
}
func (m *ProjectBrowserManager) storeArtifact(sessionID string, body []byte) (browserArtifactRecord, error) {
	if len(body) < 1 || len(body) > edge.MaxBrowserArtifactBytes {
		return browserArtifactRecord{}, errors.New("project browser artifact is oversized")
	}
	id, err := m.newID("ba_")
	if err != nil {
		return browserArtifactRecord{}, err
	}
	path := filepath.Join(m.artifactRoot, id+".jpg")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return browserArtifactRecord{}, errors.New("project browser artifact unavailable")
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return browserArtifactRecord{}, errors.New("project browser artifact unavailable")
	}
	sum := sha256.Sum256(body)
	a := browserArtifactRecord{ArtifactID: id, SessionID: sessionID, MediaType: "image/jpeg", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:]), Path: path, CreatedAt: m.now().UTC()}
	_, err = m.db.Exec(`INSERT INTO browser_artifacts(artifact_id,session_id,media_type,bytes,sha256,path,created_at) VALUES(?,?,?,?,?,?,?)`, a.ArtifactID, a.SessionID, a.MediaType, a.Bytes, a.SHA256, a.Path, a.CreatedAt.UnixNano())
	if err != nil {
		_ = os.Remove(path)
		return browserArtifactRecord{}, errors.New("project browser journal unavailable")
	}
	return a, nil
}
func openPrivateBrowserArtifact(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return nil, errors.New("project browser artifact is unsafe")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("project browser artifact unavailable")
	}
	return f, nil
}
func removePrivateBrowserArtifact(path, root string) error {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanRoot) || !filepath.IsAbs(cleanPath) || filepath.Dir(cleanPath) != cleanRoot || !browserManagedArtifactIDPattern.MatchString(strings.TrimSuffix(filepath.Base(cleanPath), ".jpg")) || filepath.Ext(cleanPath) != ".jpg" {
		return errors.New("project browser artifact path is unsafe")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project browser artifact is unsafe")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("project browser artifact cleanup failed")
	}
	return nil
}
func removePrivateBrowserProfile(path, root, sessionID string) error {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanRoot) || !filepath.IsAbs(cleanPath) || !browserManagedSessionIDPattern.MatchString(sessionID) || cleanPath != filepath.Join(cleanRoot, sessionID) || filepath.Dir(cleanPath) != cleanRoot {
		return errors.New("project browser profile path is unsafe")
	}
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("project browser profile is unsafe")
	}
	if err := os.RemoveAll(cleanPath); err != nil {
		return errors.New("project browser profile cleanup failed")
	}
	return nil
}

func (m *ProjectBrowserManager) artifactsForSession(id string) ([]browserArtifactRecord, error) {
	rows, err := m.db.Query(`SELECT artifact_id,session_id,media_type,bytes,sha256,path,created_at FROM browser_artifacts WHERE session_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []browserArtifactRecord{}
	for rows.Next() {
		var a browserArtifactRecord
		var created int64
		if err := rows.Scan(&a.ArtifactID, &a.SessionID, &a.MediaType, &a.Bytes, &a.SHA256, &a.Path, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(0, created).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
