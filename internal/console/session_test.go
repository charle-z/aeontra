package console

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestSessionStoreCreatesDigestOnlySession(t *testing.T) {
	now := time.Date(2026, 7, 13, 21, 0, 0, 0, time.UTC)
	store, err := NewSessionStore(SessionConfig{
		TTL:         8 * time.Hour,
		MaxSessions: 4,
		Now:         func() time.Time { return now },
		Rand:        bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != sessionBytes {
		t.Fatalf("invalid session id %q: len=%d err=%v", raw, len(decoded), err)
	}
	if !store.Valid(raw) {
		t.Fatal("created session is not valid")
	}
	if len(store.sessions) != 1 {
		t.Fatalf("sessions = %d", len(store.sessions))
	}
	for digest := range store.sessions {
		if strings.Contains(string(digest[:]), raw) {
			t.Fatal("raw session id stored in map")
		}
	}
}

func TestSessionStoreExpiryAndRevoke(t *testing.T) {
	now := time.Date(2026, 7, 13, 21, 0, 0, 0, time.UTC)
	store, err := NewSessionStore(SessionConfig{
		TTL:         time.Hour,
		MaxSessions: 4,
		Now:         func() time.Time { return now },
		Rand:        bytes.NewReader(bytes.Repeat([]byte{0x21}, 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	store.Revoke(raw)
	if store.Valid(raw) {
		t.Fatal("revoked session remains valid")
	}
	raw, err = store.Create()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	if store.Valid(raw) {
		t.Fatal("expired session remains valid")
	}
}

func TestSessionStoreCapsAndEvictsOldestExpiry(t *testing.T) {
	now := time.Date(2026, 7, 13, 21, 0, 0, 0, time.UTC)
	random := append(bytes.Repeat([]byte{0x01}, sessionBytes), bytes.Repeat([]byte{0x02}, sessionBytes)...)
	random = append(random, bytes.Repeat([]byte{0x03}, sessionBytes)...)
	store, err := NewSessionStore(SessionConfig{
		TTL:         time.Hour,
		MaxSessions: 2,
		Now:         func() time.Time { return now },
		Rand:        bytes.NewReader(random),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := store.Create()
	now = now.Add(time.Minute)
	second, _ := store.Create()
	now = now.Add(time.Minute)
	third, _ := store.Create()
	if len(store.sessions) != 2 {
		t.Fatalf("sessions = %d", len(store.sessions))
	}
	if store.Valid(first) {
		t.Fatal("oldest session was not evicted")
	}
	if !store.Valid(second) || !store.Valid(third) {
		t.Fatal("newer sessions were lost")
	}
}

func TestSessionStoreRejectsInvalidConfigAndRandomFailure(t *testing.T) {
	for _, cfg := range []SessionConfig{
		{TTL: -time.Second, MaxSessions: 1},
		{TTL: time.Hour, MaxSessions: -1},
		{TTL: maxSessionTTL + time.Second, MaxSessions: 1},
		{TTL: time.Hour, MaxSessions: maxSessionCount + 1},
	} {
		if _, err := NewSessionStore(cfg); err == nil {
			t.Fatalf("config should fail: %+v", cfg)
		}
	}
	store, err := NewSessionStore(SessionConfig{
		TTL:         time.Hour,
		MaxSessions: 1,
		Rand:        bytes.NewReader(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(); err == nil {
		t.Fatal("random failure should fail session creation")
	}
}
