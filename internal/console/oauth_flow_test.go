package console

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }

func TestOAuthFlowStoreCreatesConsumesExpiresAndPrunes(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store := newOAuthFlowStore()
	store.now = func() time.Time { return now }
	store.rand = bytes.NewReader(append(bytes.Repeat([]byte{0x31}, oauthRandomBytes), bytes.Repeat([]byte{0x32}, oauthRandomBytes)...))
	state, verifier, challenge, err := store.create()
	if err != nil || state == "" || verifier == "" || challenge == "" || state == verifier {
		t.Fatalf("state=%q verifier=%q challenge=%q err=%v", state, verifier, challenge, err)
	}
	if got, ok := store.consume(state); !ok || got != verifier {
		t.Fatalf("consume=%q ok=%v", got, ok)
	}
	if _, ok := store.consume(state); ok {
		t.Fatal("OAuth state replay accepted")
	}

	store.rand = bytes.NewReader(bytes.Repeat([]byte{0x42}, oauthRandomBytes*4))
	expiredState, _, _, err := store.create()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(consoleOAuthFlowTTL + time.Second)
	if _, ok := store.consume(expiredState); ok {
		t.Fatal("expired state accepted")
	}
	store.pruneLocked(now)
}

func TestOAuthFlowStoreFailsClosedOnLimitsAndRandomFailure(t *testing.T) {
	store := newOAuthFlowStore()
	store.rand = failingReader{}
	if _, _, _, err := store.create(); err == nil || !strings.Contains(err.Error(), "random") {
		t.Fatalf("random failure err=%v", err)
	}
	if _, ok := store.consume(""); ok {
		t.Fatal("empty state accepted")
	}

	store = newOAuthFlowStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	for index := 0; index < maxConsoleOAuthFlows; index++ {
		var digest [32]byte
		digest[0] = byte(index)
		store.flows[digest] = oauthFlow{verifier: "v", expiresAt: now.Add(time.Hour)}
	}
	if _, _, _, err := store.create(); err == nil {
		t.Fatal("flow cap was not enforced")
	}
}
