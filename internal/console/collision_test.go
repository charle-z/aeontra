package console

import (
	"bytes"
	"testing"
	"time"
)

func TestSessionStoreRetriesCollision(t *testing.T) {
	firstBytes := bytes.Repeat([]byte{0x41}, sessionBytes)
	secondBytes := bytes.Repeat([]byte{0x52}, sessionBytes)
	random := append(append(append([]byte{}, firstBytes...), firstBytes...), secondBytes...)
	store, err := NewSessionStore(SessionConfig{
		TTL:         time.Hour,
		MaxSessions: 4,
		Rand:        bytes.NewReader(random),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("collision was not retried")
	}
	if !store.Valid(first) || !store.Valid(second) {
		t.Fatal("sessions are not independently valid")
	}
}
