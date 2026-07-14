package oauth

import (
	"crypto/subtle"
	"errors"
	"time"
)

func (s *tokenStore) ensureFixedClient(clientID, redirectURI string) error {
	if s == nil || clientID == "" || redirectURI == "" {
		return errors.New("oauth: console client configuration is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.clients[clientID]; ok {
		if len(existing.redirectURIs) != 1 || subtle.ConstantTimeCompare([]byte(existing.redirectURIs[0]), []byte(redirectURI)) != 1 {
			return errors.New("oauth: console client registration conflicts with persisted state")
		}
		return nil
	}
	if len(s.clients) >= maxClients {
		return errors.New("oauth: client registration limit reached")
	}
	s.clients[clientID] = clientReg{redirectURIs: []string{redirectURI}, createdAt: nowUTC()}
	if err := s.persistClientsLocked(); err != nil {
		delete(s.clients, clientID)
		return errors.New("oauth: could not persist console client registration")
	}
	return nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func (s *tokenStore) revokeAccess(token string) {
	if s == nil || token == "" {
		return
	}
	s.mu.Lock()
	delete(s.access, token)
	s.mu.Unlock()
}

func (s *tokenStore) revokeRefresh(token string) {
	if s == nil || token == "" {
		return
	}
	s.mu.Lock()
	delete(s.refresh, token)
	_ = s.persistRefreshLocked()
	s.mu.Unlock()
}
