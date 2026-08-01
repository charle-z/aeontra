package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	maxAccessGrants    = 1024
	maxAccessStoreSize = 1 << 20
	maxAccessClientID  = 256
	maxAccessScope     = 256
	maxAccessResource  = 4096
)

type accessStoreFile struct {
	Version int                 `json:"version"`
	Access  []accessStoreRecord `json:"access"`
}

type accessStoreRecord struct {
	Digest    string    `json:"digest"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	Resource  string    `json:"resource"`
	ExpiresAt time.Time `json:"expires_at"`
}

func accessTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validAccessDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size
}

func (s *tokenStore) enableAccessPersistence(path string) error {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessStorePath = path
	return withAccessStoreFileLock(path, s.loadAccessLocked)
}

func (s *tokenStore) loadAccessLocked() error {
	info, err := os.Stat(s.accessStorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() > maxAccessStoreSize {
		return fmt.Errorf("access store is too large")
	}
	body, err := os.ReadFile(s.accessStorePath)
	if err != nil {
		return err
	}
	var doc accessStoreFile
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode %s: %w", s.accessStorePath, err)
	}
	if doc.Version != 1 {
		return fmt.Errorf("unsupported access store version %d", doc.Version)
	}
	if len(doc.Access) > maxAccessGrants {
		return fmt.Errorf("access store has %d grants, max %d", len(doc.Access), maxAccessGrants)
	}
	now := time.Now()
	loaded := make(map[string]accessGrant, len(doc.Access))
	for _, record := range doc.Access {
		if now.After(record.ExpiresAt) {
			continue
		}
		if !validAccessDigest(record.Digest) || record.ClientID == "" || len(record.ClientID) > maxAccessClientID ||
			len(record.Scope) > maxAccessScope || record.Resource == "" || len(record.Resource) > maxAccessResource || record.ExpiresAt.IsZero() {
			return fmt.Errorf("access store contains an invalid grant")
		}
		if _, exists := loaded[record.Digest]; exists {
			return fmt.Errorf("access store contains a duplicate digest")
		}
		loaded[record.Digest] = accessGrant{
			clientID: record.ClientID, scope: record.Scope, resource: record.Resource, expiresAt: record.ExpiresAt,
		}
	}
	s.access = loaded
	return nil
}

func (s *tokenStore) putAccess(token string, grant accessGrant) error {
	if token == "" {
		return fmt.Errorf("access token is empty")
	}
	digest := accessTokenDigest(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	return withAccessStoreFileLock(s.accessStorePath, func() error {
		if s.accessStorePath != "" {
			if err := s.loadAccessLocked(); err != nil {
				return err
			}
		}
		s.pruneAccessLocked(time.Now())
		if _, exists := s.access[digest]; !exists && len(s.access) >= maxAccessGrants {
			return fmt.Errorf("access grant limit reached")
		}
		s.access[digest] = grant
		if err := s.persistAccessLocked(); err != nil {
			delete(s.access, digest)
			return err
		}
		return nil
	})
}

func (s *tokenStore) getAccess(token string) (accessGrant, bool) {
	if token == "" {
		return accessGrant{}, false
	}
	digest := accessTokenDigest(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	var grant accessGrant
	var ok bool
	err := withAccessStoreFileLock(s.accessStorePath, func() error {
		grant, ok = s.access[digest]
		if !ok && s.accessStorePath != "" {
			if err := s.loadAccessLocked(); err != nil {
				return err
			}
			grant, ok = s.access[digest]
		}
		if ok && time.Now().After(grant.expiresAt) {
			delete(s.access, digest)
			_ = s.persistAccessLocked()
			ok = false
		}
		return nil
	})
	if err != nil || !ok {
		return accessGrant{}, false
	}
	return grant, true
}

func (s *tokenStore) pruneAccessLocked(now time.Time) {
	for digest, grant := range s.access {
		if now.After(grant.expiresAt) {
			delete(s.access, digest)
		}
	}
}

func (s *tokenStore) persistAccessLocked() error {
	if s.accessStorePath == "" {
		return nil
	}
	s.pruneAccessLocked(time.Now())
	records := make([]accessStoreRecord, 0, len(s.access))
	for digest, grant := range s.access {
		records = append(records, accessStoreRecord{
			Digest: digest, ClientID: grant.clientID, Scope: grant.scope, Resource: grant.resource, ExpiresAt: grant.expiresAt,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Digest < records[j].Digest })
	body, err := json.MarshalIndent(accessStoreFile{Version: 1, Access: records}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic0600(s.accessStorePath, body)
}
