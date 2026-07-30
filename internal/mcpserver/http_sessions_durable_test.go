package mcpserver

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableHTTPSessionSurvivesStoreReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)

	first, err := openHTTPSessionStoreWithClock(root, 60*365*24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := first.CreateBound("oauth-client:chatgpt", "catalog-a", ClientCapabilities{
		ClientName:      "chatgpt",
		ClientVersion:   "1",
		ProtocolVersion: "2024-11-05",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := openHTTPSessionStoreWithClock(root, 60*365*24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	record, validation, err := second.ValidateBound(sessionID, "oauth-client:chatgpt", "catalog-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if validation != httpSessionValid {
		t.Fatalf("replacement validation=%v", validation)
	}
	if record.Capabilities.ClientName != "chatgpt" || record.Capabilities.ProtocolVersion != "2024-11-05" {
		t.Fatalf("capabilities were not durable: %+v", record.Capabilities)
	}
	for _, path := range []string{
		filepath.Join(root, httpSessionDatabaseFilename),
		filepath.Join(root, httpSessionDatabaseFilename+"-wal"),
	} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range [][]byte{[]byte(sessionID), []byte("oauth-client:chatgpt")} {
			if bytes.Contains(data, forbidden) {
				t.Fatalf("durable session storage contains raw protected value %q", forbidden)
			}
		}
	}
}

func TestDurableHTTPSessionRejectsAnotherPrincipalAndRevokesGlobally(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	storeA, err := openHTTPSessionStoreWithClock(root, 60*365*24*time.Hour, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := openHTTPSessionStoreWithClock(root, 60*365*24*time.Hour, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()

	sessionID, err := storeA.CreateBound("oauth-client:owner", "catalog-a", ClientCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if _, validation, err := storeB.ValidateBound(sessionID, "oauth-client:other", "catalog-a", false); err != nil {
		t.Fatal(err)
	} else if validation != httpSessionUnknown {
		t.Fatalf("cross-principal validation=%v", validation)
	}

	deleted, err := storeB.DeleteBound(sessionID, "oauth-client:owner")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("durable delete did not revoke the session")
	}
	if _, validation, err := storeA.ValidateBound(sessionID, "oauth-client:owner", "catalog-a", false); err != nil {
		t.Fatal(err)
	} else if validation != httpSessionUnknown {
		t.Fatalf("revoked session validation=%v", validation)
	}
}

func TestLegacyHTTPSessionIsAdoptedOnceDuringMigrationWindow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	store, err := openHTTPSessionStoreWithClock(root, 60*365*24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	legacyID := "0123456789abcdef0123456789abcdef"
	_, validation, err := store.ValidateBound(legacyID, "oauth-client:owner", "catalog-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if validation != httpSessionValid {
		t.Fatalf("legacy adoption validation=%v", validation)
	}
	if _, validation, err := store.ValidateBound(legacyID, "oauth-client:other", "catalog-a", true); err != nil {
		t.Fatal(err)
	} else if validation != httpSessionUnknown {
		t.Fatalf("adopted legacy session changed principal: %v", validation)
	}

	now = now.Add(defaultLegacySessionAdoptionWindow + time.Nanosecond)
	unknownLegacy := "fedcba9876543210fedcba9876543210"
	if _, validation, err := store.ValidateBound(unknownLegacy, "oauth-client:owner", "catalog-a", true); err != nil {
		t.Fatal(err)
	} else if validation != httpSessionUnknown {
		t.Fatalf("legacy session adopted after migration window: %v", validation)
	}
}
