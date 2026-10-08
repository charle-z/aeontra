package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCreateOperationReclaimsOldTerminalRowsAtDatabaseLimit(t *testing.T) {
	store := openHTTPTestStore(t)
	code, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.Pair(code, "windows-trusted", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	active, fresh, err := store.CreateOperation(device.ID, OperationProjectStatus, OperationRequest{
		Alias: "active-project", TargetAlias: "windows-trusted", Profile: "linux-workcell",
	})
	if err != nil || !fresh {
		t.Fatalf("active=%+v fresh=%t err=%v", active, fresh, err)
	}

	var pageCount int64
	if err := store.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		t.Fatal(err)
	}
	var maxPageCount int64
	if err := store.db.QueryRow(fmt.Sprintf(`PRAGMA max_page_count=%d`, pageCount+64)).Scan(&maxPageCount); err != nil {
		t.Fatal(err)
	}
	if maxPageCount != pageCount+64 {
		t.Fatalf("max_page_count=%d page_count=%d", maxPageCount, pageCount)
	}

	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "filler", ProjectOwner: "charle-z", ProjectRepository: "aeontra",
		ProjectTarget: "windows-trusted", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		ExecCompleted: true, ExecStdout: strings.Repeat("x", MaxProjectExecStreamBytes),
	}
	if !validOperationCompletionForKind(OperationProjectExec, result, "") {
		t.Fatal("filler result is invalid")
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	inserted := 0
	for i := 0; i < 1024; i++ {
		request, err := validateOperationRequestWithProjectExec(OperationProjectExec, OperationRequest{
			Alias: "filler", TargetAlias: "windows-trusted", Profile: "linux-workcell",
			IdempotencyKey: fmt.Sprintf("storage-filler-%d", i), Argv: []string{"true"}, TimeoutSeconds: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		requestJSON, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.db.Exec(`INSERT INTO edge_operations(operation_id,device_id,kind,request_json,request_digest,state,result_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("eo_%032x", i+1), device.ID, OperationProjectExec, requestJSON, fmt.Sprintf("%064x", i+1), OperationSucceeded, resultJSON, int64(i+1), int64(i+1))
		if err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "full") {
				t.Fatalf("fill err=%v", err)
			}
			break
		}
		inserted++
	}
	if inserted == 0 || inserted == 1024 {
		t.Fatalf("database did not reach its page limit: inserted=%d", inserted)
	}

	created, fresh, err := store.CreateOperation(device.ID, OperationProjectStatus, OperationRequest{
		Alias: "hire-me-landing", TargetAlias: "windows-trusted", Profile: "linux-workcell",
	})
	if err != nil || !fresh || created.State != OperationQueued {
		t.Fatalf("created=%+v fresh=%t err=%v", created, fresh, err)
	}
	activeAfter, err := store.OperationLifecycleStatus(active.ID)
	if err != nil || activeAfter.State != OperationQueued {
		t.Fatalf("active operation was pruned or changed: operation=%+v err=%v", activeAfter, err)
	}
	var remaining int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM edge_operations WHERE kind=? AND state=?`, OperationProjectExec, OperationSucceeded).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining >= inserted {
		t.Fatalf("terminal rows were not reclaimed: inserted=%d remaining=%d", inserted, remaining)
	}
	available, reserve, err := store.operationStorageCapacityLocked()
	if err != nil || available < reserve {
		t.Fatalf("storage reserve was not restored: available=%d reserve=%d err=%v", available, reserve, err)
	}
}
