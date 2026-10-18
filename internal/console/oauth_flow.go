package console

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	consoleOAuthFlowTTL  = 5 * time.Minute
	maxConsoleOAuthFlows = 64
	oauthRandomBytes     = 32
)

type oauthFlow struct {
	verifier  string
	expiresAt time.Time
}

type oauthFlowStore struct {
	mu    sync.Mutex
	flows map[[sha256.Size]byte]oauthFlow
	now   func() time.Time
	rand  io.Reader
}

func newOAuthFlowStore() *oauthFlowStore {
	return &oauthFlowStore{
		flows: make(map[[sha256.Size]byte]oauthFlow),
		now:   time.Now,
		rand:  rand.Reader,
	}
}

func (s *oauthFlowStore) create() (state, verifier, challenge string, err error) {
	if s == nil {
		return "", "", "", errors.New("console OAuth flow store is unavailable")
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if len(s.flows) >= maxConsoleOAuthFlows {
		return "", "", "", errors.New("console OAuth flow limit reached")
	}
	for attempt := 0; attempt < 4; attempt++ {
		state, err = randomURLToken(s.rand)
		if err != nil {
			return "", "", "", err
		}
		verifier, err = randomURLToken(s.rand)
		if err != nil {
			return "", "", "", err
		}
		digest := sha256.Sum256([]byte(state))
		if _, exists := s.flows[digest]; exists {
			continue
		}
		s.flows[digest] = oauthFlow{verifier: verifier, expiresAt: now.Add(consoleOAuthFlowTTL)}
		challengeDigest := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(challengeDigest[:])
		return state, verifier, challenge, nil
	}
	return "", "", "", errors.New("console OAuth state collision limit reached")
}

func (s *oauthFlowStore) consume(state string) (string, bool) {
	if s == nil || state == "" {
		return "", false
	}
	now := s.now()
	digest := sha256.Sum256([]byte(state))
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.flows[digest]
	delete(s.flows, digest)
	if !ok || now.After(flow.expiresAt) {
		return "", false
	}
	return flow.verifier, true
}

func (s *oauthFlowStore) pruneLocked(now time.Time) {
	for digest, flow := range s.flows {
		if now.After(flow.expiresAt) {
			delete(s.flows, digest)
		}
	}
}

func randomURLToken(source io.Reader) (string, error) {
	buffer := make([]byte, oauthRandomBytes)
	if _, err := io.ReadFull(source, buffer); err != nil {
		return "", errors.New("secure random generation failed")
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
