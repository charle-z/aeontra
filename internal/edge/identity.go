package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	PairingTTL      = 10 * time.Minute
	SignatureWindow = 2 * time.Minute
	NonceTTL        = 10 * time.Minute
)

type State string

const (
	StateActive  State = "active"
	StateRevoked State = "revoked"
)

type Config struct {
	Root string
	Now  func() time.Time
}

type Device struct {
	ID       string    `json:"device_id"`
	Name     string    `json:"name"`
	State    State     `json:"state"`
	PairedAt time.Time `json:"paired_at"`
}
type SignedRequest struct {
	DeviceID            string
	Timestamp           int64
	Nonce, Method, Path string
	Body, Signature     []byte
}

func (r SignedRequest) Canonical() []byte {
	sum := sha256.Sum256(r.Body)
	return []byte(fmt.Sprintf("edge-v1\n%s\n%d\n%s\n%s\n%s\n%s", r.DeviceID, r.Timestamp, r.Nonce, strings.ToUpper(r.Method), r.Path, hex.EncodeToString(sum[:])))
}

type Store struct {
	mu  sync.Mutex
	db  *sql.DB
	now func() time.Time
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var idPattern = regexp.MustCompile(`^ed_[a-f0-9]{32}$`)
var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)

func Open(cfg Config) (*Store, error) {
	root := filepath.Clean(strings.TrimSpace(cfg.Root))
	if !filepath.IsAbs(root) || root == "." {
		return nil, errors.New("edge root must be absolute")
	}
	if err := rejectSymlinkAncestors(root); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("edge root is unsafe")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return nil, errors.New("edge root unavailable")
		}
	} else {
		return nil, errors.New("edge root unavailable")
	}
	path := filepath.Join(root, "edge.db")
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, errors.New("edge database is unsafe")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("edge database unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, now: cfg.Now}
	if store.now == nil {
		store.now = time.Now
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`, `PRAGMA max_page_count=8192`,
		`CREATE TABLE IF NOT EXISTS pairings(code_hash BLOB PRIMARY KEY, expires_at INTEGER NOT NULL, consumed_at INTEGER) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS devices(device_id TEXT PRIMARY KEY, name TEXT NOT NULL, public_key BLOB NOT NULL UNIQUE, state TEXT NOT NULL, paired_at INTEGER NOT NULL, revoked_at INTEGER) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS nonces(device_id TEXT NOT NULL, nonce_hash BLOB NOT NULL, expires_at INTEGER NOT NULL, PRIMARY KEY(device_id,nonce_hash)) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("edge database initialization failed")
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("edge database permissions failed")
	}
	return store, nil
}

func rejectSymlinkAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("edge root is unsafe")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("edge root unavailable")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func randomOpaque(prefix string, bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}
func digest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func (s *Store) CreatePairing(ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > PairingTTL {
		return "", errors.New("pairing ttl is invalid")
	}
	code, err := randomOpaque("ep_", 24)
	if err != nil {
		return "", errors.New("pairing generation failed")
	}
	_, err = s.db.Exec(`INSERT INTO pairings(code_hash,expires_at) VALUES(?,?)`, digest(code), s.now().UTC().Add(ttl).Unix())
	if err != nil {
		return "", errors.New("pairing persistence failed")
	}
	return code, nil
}

func (s *Store) Pair(code, name string, publicKey ed25519.PublicKey) (Device, error) {
	if !namePattern.MatchString(name) || len(publicKey) != ed25519.PublicKeySize || !strings.HasPrefix(code, "ep_") {
		return Device{}, errors.New("pairing request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Device{}, errors.New("pairing unavailable")
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE pairings SET consumed_at=? WHERE code_hash=? AND consumed_at IS NULL AND expires_at>?`, now.Unix(), digest(code), now.Unix())
	if err != nil {
		return Device{}, errors.New("pairing unavailable")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Device{}, errors.New("pairing code is invalid or expired")
	}
	id, err := randomOpaque("ed_", 16)
	if err != nil {
		return Device{}, errors.New("device identity generation failed")
	}
	if _, err := tx.Exec(`INSERT INTO devices(device_id,name,public_key,state,paired_at) VALUES(?,?,?,?,?)`, id, name, []byte(publicKey), StateActive, now.Unix()); err != nil {
		return Device{}, errors.New("device registration failed")
	}
	if err := tx.Commit(); err != nil {
		return Device{}, errors.New("pairing unavailable")
	}
	return Device{ID: id, Name: name, State: StateActive, PairedAt: now}, nil
}

func (s *Store) Authenticate(request SignedRequest) (Device, error) {
	if !idPattern.MatchString(request.DeviceID) || !noncePattern.MatchString(request.Nonce) || len(request.Signature) != ed25519.SignatureSize {
		return Device{}, errors.New("edge authentication failed")
	}
	now := s.now().UTC()
	signedAt := time.Unix(request.Timestamp, 0).UTC()
	if signedAt.Before(now.Add(-SignatureWindow)) || signedAt.After(now.Add(SignatureWindow)) {
		return Device{}, errors.New("edge authentication failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Device{}, errors.New("edge authentication failed")
	}
	defer tx.Rollback()
	var device Device
	var pub []byte
	var paired int64
	if err := tx.QueryRow(`SELECT name,state,public_key,paired_at FROM devices WHERE device_id=?`, request.DeviceID).Scan(&device.Name, &device.State, &pub, &paired); err != nil || device.State != StateActive || !ed25519.Verify(ed25519.PublicKey(pub), request.Canonical(), request.Signature) {
		return Device{}, errors.New("edge authentication failed")
	}
	device.ID = request.DeviceID
	device.PairedAt = time.Unix(paired, 0).UTC()
	_, _ = tx.Exec(`DELETE FROM nonces WHERE expires_at<=?`, now.Unix())
	if _, err := tx.Exec(`INSERT INTO nonces(device_id,nonce_hash,expires_at) VALUES(?,?,?)`, request.DeviceID, digest(request.Nonce), now.Add(NonceTTL).Unix()); err != nil {
		return Device{}, errors.New("edge authentication failed")
	}
	if err := tx.Commit(); err != nil {
		return Device{}, errors.New("edge authentication failed")
	}
	return device, nil
}

func (s *Store) Revoke(deviceID string) error {
	if !idPattern.MatchString(deviceID) {
		return errors.New("device id is invalid")
	}
	result, err := s.db.Exec(`UPDATE devices SET state=?,revoked_at=? WHERE device_id=? AND state=?`, StateRevoked, s.now().UTC().Unix(), deviceID, StateActive)
	if err != nil {
		return errors.New("device revocation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("active device not found")
	}
	return nil
}
func (s *Store) ActiveCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM devices WHERE state=?`, StateActive).Scan(&count)
	return count, err
}
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func EncodePublicKey(key ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(key)
}
