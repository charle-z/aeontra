package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestCreateOperationReportsInsertPersistenceStageWithoutDatabaseDetail(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_operation_insert BEFORE INSERT ON edge_operations BEGIN SELECT RAISE(FAIL, 'private sqlite detail'); END`); err != nil {
		t.Fatal(err)
	}

	_, _, err = store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err == nil || err.Error() != "edge operation persistence failed: insert" {
		t.Fatalf("err=%v", err)
	}
}
