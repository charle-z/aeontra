package modelturn

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPinnedDevelopmentBodyRequiresExactOwnerReferenceAndDigest(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	goal := []byte("private development goal body")
	body, err := store.StageRuntimeGoal(context.Background(), goal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	owner := IdempotencyDigest("development-request-owner")
	ref := TaskGoalReference{BodyRef: body.BodyRef, ContentDigest: body.ContentDigest}

	if _, err := store.PinnedDevelopmentBody(context.Background(), owner, ref); !errors.Is(err, ErrRequestRefConflict) {
		t.Fatalf("unpinned body read error=%v", err)
	}
	if err := store.PinTaskGoalReferences(context.Background(), owner, []TaskGoalReference{ref}); err != nil {
		t.Fatal(err)
	}

	content, err := store.PinnedDevelopmentBody(context.Background(), owner, ref)
	if err != nil || !bytes.Equal(content, goal) {
		t.Fatalf("content=%q err=%v", content, err)
	}
	content[0] = 'X'
	content, err = store.PinnedDevelopmentBody(context.Background(), owner, ref)
	if err != nil || !bytes.Equal(content, goal) {
		t.Fatalf("returned content was not isolated: content=%q err=%v", content, err)
	}

	wrongOwner := IdempotencyDigest("another-development-request-owner")
	wrongDigest := ref
	wrongDigest.ContentDigest = "sha256:" + repeatDigestCharacter("0")
	wrongReference := ref
	wrongReference.BodyRef = "mb_11111111111111111111111111111111"
	for label, test := range map[string]struct {
		owner string
		ref   TaskGoalReference
	}{
		"wrong owner":     {owner: wrongOwner, ref: ref},
		"wrong digest":    {owner: owner, ref: wrongDigest},
		"wrong reference": {owner: owner, ref: wrongReference},
	} {
		t.Run(label, func(t *testing.T) {
			if content, err := store.PinnedDevelopmentBody(context.Background(), test.owner, test.ref); !errors.Is(err, ErrRequestRefConflict) || content != nil {
				t.Fatalf("content=%q err=%v", content, err)
			}
		})
	}
	var missingContext context.Context
	if content, err := store.PinnedDevelopmentBody(missingContext, owner, ref); !errors.Is(err, ErrRequestRefConflict) || content != nil {
		t.Fatalf("nil context content=%q err=%v", content, err)
	}
}

func TestPinnedDevelopmentBodyRemainsAvailableAfterExpiry(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	root := filepath.Join(t.TempDir(), "model-turns")
	store, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	goal := []byte("pinned request remains recoverable after ttl")
	body, err := store.StageRuntimeGoal(context.Background(), goal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	owner := IdempotencyDigest("expired-but-pinned-development-request")
	ref := TaskGoalReference{BodyRef: body.BodyRef, ContentDigest: body.ContentDigest}
	if err := store.PinTaskGoalReferences(context.Background(), owner, []TaskGoalReference{ref}); err != nil {
		t.Fatal(err)
	}
	clock.Add(2 * time.Minute)
	if err := store.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := store.PinnedDevelopmentBody(context.Background(), owner, ref)
	if err != nil || !bytes.Equal(content, goal) {
		t.Fatalf("expired pinned body content=%q err=%v", content, err)
	}
}

func TestPinnedDevelopmentBodyRejectsStoredContentDigestMismatch(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	body, err := store.StageRuntimeGoal(context.Background(), []byte("original immutable body"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	owner := IdempotencyDigest("tampered-development-request-owner")
	ref := TaskGoalReference{BodyRef: body.BodyRef, ContentDigest: body.ContentDigest}
	if err := store.PinTaskGoalReferences(context.Background(), owner, []TaskGoalReference{ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TRIGGER runtime_bodies_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE runtime_bodies SET content=? WHERE body_ref=?`, []byte("tampered contents"), body.BodyRef); err != nil {
		t.Fatal(err)
	}
	if content, err := store.PinnedDevelopmentBody(context.Background(), owner, ref); !errors.Is(err, ErrRequestRefConflict) || content != nil {
		t.Fatalf("tampered content=%q err=%v", content, err)
	}
}

func repeatDigestCharacter(character string) string {
	return strings.Repeat(character, 64)
}
