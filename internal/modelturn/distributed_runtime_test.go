package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestBoundRuntimePersistsOnlyContentFreeMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "turns")
	store, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	goal := []byte("Fix the private repository bug without exposing the prompt")
	body, err := store.StageRuntimeGoal(context.Background(), goal, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request := BoundRuntimeRequest{
		DeviceID:             "ed_0123456789abcdef0123456789abcdef",
		WorkspaceID:          "ws_0123456789abcdef0123456789abcdef",
		Controller:           ControllerRemoteEdge,
		GoalSummary:          GoalSummary(goal),
		GoalRef:              body.BodyRef,
		GoalDigest:           body.ContentDigest,
		IdempotencyKeyDigest: IdempotencyDigest("runtime-request-0001"),
		TTL:                  10 * time.Minute,
	}
	runtime, created, err := store.StartBoundRuntime(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !created || runtime.State != RuntimeStateAwaitingEdge || runtime.Controller != ControllerRemoteEdge {
		t.Fatalf("unexpected runtime: created=%t runtime=%+v", created, runtime)
	}
	if runtime.DeviceID != request.DeviceID || runtime.WorkspaceID != request.WorkspaceID || runtime.GoalSummary != GoalSummary(goal) {
		t.Fatalf("runtime binding mismatch: %+v", runtime)
	}
	if strings.Contains(runtime.GoalSummary, "private") || strings.Contains(runtime.GoalSummary, "repository") {
		t.Fatalf("goal summary leaked goal content: %q", runtime.GoalSummary)
	}
	content, digest, err := store.RuntimeGoal(context.Background(), runtime.RuntimeID, request.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(goal) || digest != body.ContentDigest {
		t.Fatalf("goal body mismatch: content=%q digest=%q", content, digest)
	}
	if _, _, err := store.RuntimeGoal(context.Background(), runtime.RuntimeID, "ed_ffffffffffffffffffffffffffffffff"); !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("wrong device read error=%v", err)
	}

	replayed, replayCreated, err := store.StartBoundRuntime(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if replayCreated || replayed.RuntimeID != runtime.RuntimeID {
		t.Fatalf("idempotent replay created duplicate: first=%+v replay=%+v", runtime, replayed)
	}
	conflict := request
	conflict.WorkspaceID = "ws_ffffffffffffffffffffffffffffffff"
	if _, _, err := store.StartBoundRuntime(context.Background(), conflict); !errors.Is(err, ErrTurnConflict) {
		t.Fatalf("idempotency conflict error=%v", err)
	}
}

func TestBoundRuntimeLeaseHeartbeatAndStateMachine(t *testing.T) {
	store, err := OpenStore(StoreConfig{Root: filepath.Join(t.TempDir(), "turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	deviceID := "ed_0123456789abcdef0123456789abcdef"
	goal := []byte("run the bounded OpenCode workflow")
	body, err := store.StageRuntimeGoal(context.Background(), goal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _, err := store.StartBoundRuntime(context.Background(), BoundRuntimeRequest{
		DeviceID: deviceID, WorkspaceID: "ws_abcdefabcdefabcdefabcdefabcdefab", Controller: ControllerRemoteEdge,
		GoalSummary: GoalSummary(goal), GoalRef: body.BodyRef, GoalDigest: body.ContentDigest,
		IdempotencyKeyDigest: IdempotencyDigest("runtime-request-0002"), TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lease, found, err := store.LeaseNextRuntime(context.Background(), "ed_ffffffffffffffffffffffffffffffff"); err != nil || found || lease.RuntimeID != "" {
		t.Fatalf("wrong device lease=%+v found=%t err=%v", lease, found, err)
	}
	leased, found, err := store.LeaseNextRuntime(context.Background(), deviceID)
	if err != nil || !found || leased.RuntimeID != runtime.RuntimeID || leased.State != RuntimeStateStarting {
		t.Fatalf("lease=%+v found=%t err=%v", leased, found, err)
	}
	heartbeat, err := store.HeartbeatRuntime(context.Background(), runtime.RuntimeID, deviceID)
	if err != nil || heartbeat.LastHeartbeat.IsZero() {
		t.Fatalf("heartbeat=%+v err=%v", heartbeat, err)
	}
	if err := store.SetRuntimeState(context.Background(), runtime.RuntimeID, RuntimeStateAwaitingModel, RuntimeStateStarting); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRuntimeState(context.Background(), runtime.RuntimeID, RuntimeStateExecutingTools, RuntimeStateAwaitingModel); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRuntimeResult(context.Background(), runtime.RuntimeID, deviceID, "rs_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteRuntime(context.Background(), runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	completed, err := store.RuntimeForDevice(context.Background(), runtime.RuntimeID, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != RuntimeStateCompleted || completed.Status != RuntimeCompleted || completed.ResultRef == "" {
		t.Fatalf("completed runtime mismatch: %+v", completed)
	}
	if _, err := store.HeartbeatRuntime(context.Background(), runtime.RuntimeID, deviceID); !errors.Is(err, ErrTurnConflict) {
		t.Fatalf("terminal heartbeat error=%v", err)
	}
}

func TestOpenStoreMigratesLegacyRuntimeSchema(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, turnDatabaseFilename)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE model_runtimes (
		runtime_id TEXT PRIMARY KEY,
		status TEXT NOT NULL CHECK(status IN ('ready','running','completed','cancelled','failed')),
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) WITHOUT ROWID`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO model_runtimes(runtime_id,status,created_at,updated_at) VALUES('mr_legacy','running',?,?)`, now.UnixNano(), now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := store.Runtime(context.Background(), "mr_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Controller != ControllerPullRendezvous || runtime.State != RuntimeStateAwaitingModel || runtime.ExpiresAt.IsZero() {
		t.Fatalf("legacy runtime not migrated: %+v", runtime)
	}
}

func TestRuntimeMetadataDoesNotPersistGoalOrForbiddenFields(t *testing.T) {
	store, err := OpenStore(StoreConfig{Root: filepath.Join(t.TempDir(), "turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	goal := []byte("secret prompt with /home/user and bash arguments")
	body, err := store.StageRuntimeGoal(context.Background(), goal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.StartBoundRuntime(context.Background(), BoundRuntimeRequest{
		DeviceID: "ed_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_abcdefabcdefabcdefabcdefabcdefab",
		Controller: ControllerRemoteEdge, GoalSummary: GoalSummary(goal), GoalRef: body.BodyRef, GoalDigest: body.ContentDigest,
		IdempotencyKeyDigest: IdempotencyDigest("runtime-request-0003"), TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	var metadata string
	if err := store.db.QueryRow(`SELECT device_id||workspace_id||controller||state||goal_summary||result_ref FROM model_runtimes LIMIT 1`).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret prompt", "/home/user", "bash arguments"} {
		if strings.Contains(metadata, forbidden) {
			t.Fatalf("runtime metadata contains forbidden value %q", forbidden)
		}
	}
}
