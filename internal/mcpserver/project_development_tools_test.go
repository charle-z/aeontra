package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type developmentTestEdge struct {
	*projectTaskEdgeStore
	lostStartACK       bool
	forgedResult       bool
	exitCode           int
	sourceChanged      bool
	missingCapability  bool
	interruptedStart   bool
	recoveryMissing    bool
	bootstrapEnabled   bool
	bootstrapInstalled bool
	bootstrapStarts    int
	starts             int
}

func (store *developmentTestEdge) LatestDevelopmentProcessOperation(device string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var latest edge.Operation
	found := false
	for _, op := range store.operations {
		if op.DeviceID == device && op.Kind == kind && reflect.DeepEqual(op.Request, request) && (!found || op.ID > latest.ID) {
			latest = op
			found = true
		}
	}
	return latest, found, nil
}

func (store *developmentTestEdge) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	op, created, err := store.projectTaskEdgeStore.CreateOperation(deviceID, kind, request)
	if err != nil || !created {
		return op, created, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	op.State = edge.OperationSucceeded
	op.Result = edge.OperationResult{WorkspaceID: "ws_" + strings.Repeat("1", 32), ProjectAlias: request.Alias, ProjectTarget: request.TargetAlias, ProjectOwner: "acme", ProjectRepository: "project", ProjectState: "ready", ProjectMode: "dev", ProjectProfile: "linux-workcell", ProjectClaimGeneration: 1}
	switch kind {
	case edge.OperationProjectDevelopmentInspect:
		caps := []string{"toolchain.go"}
		if store.missingCapability {
			caps = nil
		}
		if store.bootstrapInstalled {
			caps = []string{"toolchain.go", "toolchain.go.v1", "toolchain.go.v1-26", "toolchain.go.v1-26-6"}
		}
		set, _ := development.NewCapabilitySet(caps...)
		attestation, _ := development.NewEnvironmentAttestation("workcell:"+op.Result.WorkspaceID, development.ClassWorkcell, 1, set)
		record, _ := attestation.Record()
		source := "sha256:" + strings.Repeat("b", 64)
		if store.sourceChanged && strings.HasSuffix(request.IdempotencyKey, ":accept-inspect") {
			source = "sha256:" + strings.Repeat("c", 64)
		}
		op.Result.DevelopmentInspection = &edge.ProjectDevelopmentInspection{Version: 1, ProjectGeneration: 1, SourceDigest: source, SourceHead: strings.Repeat("a", 40), SourceClean: true, SourceEvidenceKnown: true, Requirements: []development.CapabilityID{"toolchain.go"}, Environments: []development.EnvironmentRecord{record}}
		if store.bootstrapEnabled {
			op.Result.DevelopmentInspection.Requirements = []development.CapabilityID{"toolchain.go.v1-26-6"}
		}
	case edge.OperationProjectDevelopmentBootstrapResolve:
		binding := *request.DevelopmentBootstrap
		resolution := development.BootstrapResolution{CapabilityID: binding.CapabilityID, Toolchain: "go", Version: "1.26.6", Platform: "amd64", ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("e", 64), ArtifactSize: 12345}
		binding.Resolution = &resolution
		binding.ResolutionDigest, _ = development.BootstrapResolutionDigest(resolution)
		op.Result.DevelopmentBootstrap = &binding
	case edge.OperationProjectDevelopmentBootstrapStart:
		store.bootstrapStarts++
		op.Result.DevelopmentBootstrap = request.DevelopmentBootstrap
		op.Result.BackgroundProcessID = "pr_" + strings.Repeat("b", 32)
		op.Result.BackgroundProcessState = "running"
	case edge.OperationProjectDevelopmentCommandStart:
		if request.DevelopmentRecoveryOperationID == "" {
			store.starts++
		}
		op.Result.DevelopmentCommand = request.DevelopmentCommand
		op.Result.BackgroundProcessID = "pr_" + strings.TrimPrefix(op.ID, "eo_")
		op.Result.BackgroundProcessState = "running"
		if request.DevelopmentRecoveryOperationID != "" {
			op.Result.BackgroundProcessID = store.operations[request.DevelopmentRecoveryOperationID].Result.BackgroundProcessID
			if store.recoveryMissing {
				op.State = edge.OperationFailed
				op.Result = edge.OperationResult{}
				op.SafeCode = "project_development_reconciliation_required"
			}
		} else if store.interruptedStart {
			op.State = edge.OperationFailed
			op.SafeCode = "operation_execution_interrupted"
		}
		if store.forgedResult {
			binding := *request.DevelopmentCommand
			binding.PrivateBodyDigest = "sha256:" + strings.Repeat("f", 64)
			op.Result.DevelopmentCommand = &binding
		}
	case edge.OperationProjectProcessStatus, edge.OperationProjectProcessStop:
		op.Result.BackgroundProcessID = request.BackgroundProcessID
		op.Result.BackgroundProcessState = "exited"
		op.Result.BackgroundExitKnown = true
		op.Result.BackgroundExitCode = store.exitCode
		if kind == edge.OperationProjectProcessStop {
			op.Result.BackgroundProcessState = "stopped"
		}
		if request.BackgroundProcessID == "pr_"+strings.Repeat("b", 32) {
			store.bootstrapInstalled = true
			op.Result.BackgroundStdout = "go version go1.26.6 linux/amd64\nmcp-devbox-bootstrap-verified=go:1.26.6\n"
			op.Result.BackgroundStdoutEOF = true
			op.Result.BackgroundStderrEOF = true
		}
	}
	store.operations[op.ID] = op
	if kind == edge.OperationProjectDevelopmentCommandStart && store.lostStartACK {
		store.lostStartACK = false
		return edge.Operation{}, false, errors.New("lost command ACK")
	}
	return op, true, nil
}

func TestProjectDevelopmentBootstrapReattestsBeforeCommandPlan(t *testing.T) {
	server, edges, _ := developmentServer(t)
	edges.bootstrapEnabled = true
	edges.missingCapability = true
	view := developmentStart(t, server, "development-bootstrap-001")
	developmentRounds(t, server, 1)
	debugRequest, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	debugObjective, _, _ := server.workQueue.DevelopmentObjective(debugRequest.ObjectiveID)
	debugInspection, _ := server.edgeOperations.OperationStatus(debugRequest.InspectionOperationID)
	_, debugEnvs, _ := developmentInspection(debugRequest, debugInspection)
	_, debugProvider, debugErr := server.developmentBootstrapSupervisor(debugObjective, debugEnvs)
	if debugErr != nil {
		t.Fatal("bootstrap constructor", debugErr)
	}
	debugCatalog, _ := development.NewEnvironmentCatalog(debugEnvs...)
	if _, err := debugProvider.Plans(context.Background(), debugObjective, debugObjective.Steps[0], debugCatalog); err != nil {
		t.Fatal("bootstrap plans", err, debugObjective, debugCatalog)
	}
	developmentRounds(t, server, 6)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if request.State != workqueue.DevelopmentRequestCompleted || edges.bootstrapStarts != 1 || edges.starts != 1 || len(objective.Steps[0].Provisioning) != 1 || objective.Steps[0].Provisioning[0].State != development.ProvisioningSucceeded {
		t.Fatalf("request=%+v bootstrap=%d starts=%d objective=%+v", request, edges.bootstrapStarts, edges.starts, objective)
	}
	if len(objective.Steps[0].Attempts) != 1 || objective.Steps[0].Attempts[0].EnvironmentDigest == objective.Steps[0].Provisioning[0].Plan.BaseEnvironmentDigest {
		t.Fatal("installer completion reused missing-capability baseline")
	}
}

func TestProjectDevelopmentInterruptedStartUsesRecoveryOnly(t *testing.T) {
	server, edges, _ := developmentServer(t)
	edges.interruptedStart = true
	view := developmentStart(t, server, "development-interrupt-001")
	developmentRounds(t, server, 5)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestCompleted || edges.starts != 1 {
		t.Fatalf("request=%+v starts=%d", request, edges.starts)
	}
	var recovered bool
	for _, op := range edges.operations {
		if op.Request.DevelopmentRecoveryOperationID != "" {
			recovered = true
			original := edges.operations[op.Request.DevelopmentRecoveryOperationID]
			if op.Request.DevelopmentRecoveryIdempotencyKey != original.Request.IdempotencyKey || op.Result.BackgroundProcessID != original.Result.BackgroundProcessID {
				t.Fatal("recovery retargeted captured process")
			}
		}
	}
	if !recovered {
		t.Fatal("interruption did not use recovery-only path")
	}
}

func TestProjectDevelopmentInterruptedMissingProcessNeverRestartsOrTerminatesBlind(t *testing.T) {
	server, edges, _ := developmentServer(t)
	edges.interruptedStart = true
	edges.recoveryMissing = true
	view := developmentStart(t, server, "development-missing-process-001")
	developmentRounds(t, server, 5)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestActive || request.Reason != workqueue.DevelopmentRequestReasonReconciliationRequired || edges.starts != 1 {
		t.Fatalf("request=%+v starts=%d", request, edges.starts)
	}
}

func TestProjectDevelopmentMissingBodyQuarantinesOnlyRequestAndCapturedStopSurvives(t *testing.T) {
	server, edges, _ := developmentServer(t)
	view := developmentStart(t, server, "development-body-lost-001")
	developmentRounds(t, server, 2)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.ProcessID == "" {
		t.Fatal("test lacks captured process")
	}
	refs := []modelturn.TaskGoalReference{{BodyRef: request.BodyRef, ContentDigest: request.BodyDigest}}
	if err := server.modelTurns.UnpinTaskGoalReferences(context.Background(), request.KeyDigest, refs); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileDevelopmentRequestsOnce(context.Background()); err == nil {
		t.Fatal("missing body not classified")
	}
	request, _, _ = server.workQueue.DevelopmentRequest(request.ID)
	if request.Reason != workqueue.DevelopmentRequestReasonReconciliationRequired || edges.starts != 1 {
		t.Fatal("missing body lost process identity")
	}
	if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + request.ID + `"}`)); err != nil {
		t.Fatal(err)
	}
	developmentRounds(t, server, 1)
	request, _, _ = server.workQueue.DevelopmentRequest(request.ID)
	if request.State != workqueue.DevelopmentRequestCancelled {
		t.Fatal("captured stop depended on unavailable body")
	}
}

func (*developmentTestEdge) WaitOperation(context.Context, string, time.Duration) (edge.Operation, error) {
	return edge.Operation{}, errors.New("development coordinator must not wait")
}

func developmentServer(t *testing.T) (*Server, *developmentTestEdge, string) {
	t.Helper()
	server, _ := modelTurnServer(t)
	root := filepath.Join(t.TempDir(), "queue")
	queue, err := workqueue.Open(workqueue.Config{Root: root, ControllerID: "development-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.workQueue.Close() })
	edges := &developmentTestEdge{projectTaskEdgeStore: newProjectTaskEdgeStore()}
	server.WithWorkQueue(queue).WithEdgeStore(edges)
	return server, edges, root
}

func developmentStart(t *testing.T, server *Server, key string) projectDevelopmentView {
	t.Helper()
	output, err := server.handleProjectDevelopmentStart(json.RawMessage(fmt.Sprintf(`{"alias":"project","target":"parrot","idempotency_key":%q,"argv":["go","test","./..."],"timeout_seconds":600}`, key)))
	if err != nil {
		t.Fatal(err)
	}
	var view projectDevelopmentView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func developmentRounds(t *testing.T, server *Server, count int) {
	t.Helper()
	for range count {
		if err := server.reconcileDevelopmentRequestsOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectDevelopmentStartDurableBeforeEffectsAndReadOnlyStatus(t *testing.T) {
	server, edges, _ := developmentServer(t)
	view := developmentStart(t, server, "development-key-001")
	if view.State != workqueue.DevelopmentRequestPreparing || len(edges.operations) != 0 {
		t.Fatal("start performed foreground effects")
	}
	request, found, err := server.workQueue.DevelopmentRequest(view.RequestID)
	if err != nil || !found {
		t.Fatal(err)
	}
	if _, err := server.developmentBody(context.Background(), request); err != nil {
		t.Fatal("body not pinned before journal commit", err)
	}
	before := request.Revision
	output, err := server.handleProjectDevelopmentStatus(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	request, _, _ = server.workQueue.DevelopmentRequest(view.RequestID)
	if request.Revision != before || len(edges.operations) != 0 || strings.Contains(output, "argv") || strings.Contains(output, "go test") {
		t.Fatal("status mutated state or exposed command", output)
	}
	if _, err := server.handleProjectDevelopmentStart(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"development-key-001","argv":["go","test","./..."],"cwd":"changed","timeout_seconds":600}`)); err == nil {
		t.Fatal("cwd retarget accepted")
	}
	if _, err := server.handleProjectDevelopmentStart(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"development-key-001","argv":["go","test","./..."],"environment":{"CGO_ENABLED":"0"},"timeout_seconds":600}`)); err == nil {
		t.Fatal("environment retarget accepted")
	}
}

func TestProjectDevelopmentExactCommandAcceptanceSurvivesRestart(t *testing.T) {
	server, edges, root := developmentServer(t)
	view := developmentStart(t, server, "development-restart-001")
	developmentRounds(t, server, 2)
	if edges.starts != 1 {
		t.Fatalf("starts=%d", edges.starts)
	}
	if err := server.workQueue.Close(); err != nil {
		t.Fatal(err)
	}
	queue, err := workqueue.Open(workqueue.Config{Root: root, ControllerID: "development-test"})
	if err != nil {
		t.Fatal(err)
	}
	server.WithWorkQueue(queue)
	developmentRounds(t, server, 4)
	output, err := server.handleProjectDevelopmentStatus(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"state":"completed"`) || !strings.Contains(output, `"acceptance_state":"command_verified"`) || edges.starts != 1 {
		t.Fatalf("output=%s starts=%d", output, edges.starts)
	}
	request, _, _ := queue.DevelopmentRequest(view.RequestID)
	objective, _, _ := queue.DevelopmentObjective(request.ObjectiveID)
	if objective.Steps[0].AcceptanceReceipt == nil || objective.Steps[0].AcceptanceReceipt.PrivateBodyDigest != request.BodyDigest {
		t.Fatal("receipt did not bind private body")
	}
}

func TestProjectDevelopmentLostStartACKRecoveredWithoutSecondEffect(t *testing.T) {
	server, edges, _ := developmentServer(t)
	view := developmentStart(t, server, "development-lost-ack-001")
	developmentRounds(t, server, 1)
	edges.lostStartACK = true
	if err := server.reconcileDevelopmentRequestsOnce(context.Background()); err == nil {
		t.Fatal("expected lost acknowledgment")
	}
	developmentRounds(t, server, 4)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if edges.starts != 1 || request.State != workqueue.DevelopmentRequestCompleted {
		t.Fatalf("starts=%d state=%s", edges.starts, request.State)
	}
}

func TestProjectDevelopmentForgedReceiptAndChangedSourceNeverAccept(t *testing.T) {
	for _, mode := range []string{"forged", "source", "code"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			view := developmentStart(t, server, "development-invalid-001")
			edges.forgedResult = mode == "forged"
			edges.sourceChanged = mode == "source"
			if mode == "code" {
				edges.exitCode = 1
			}
			var gotErr error
			for range 5 {
				gotErr = server.reconcileDevelopmentRequestsOnce(context.Background())
				if gotErr != nil {
					break
				}
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if objective.State == development.ObjectiveAccepted || request.State == workqueue.DevelopmentRequestCompleted {
				t.Fatal("untrusted evidence accepted")
			}
			if mode == "forged" && gotErr == nil {
				t.Fatal("forged binding not rejected")
			}
			if mode != "forged" && request.State != workqueue.DevelopmentRequestAwaitingReasoning {
				t.Fatalf("state=%s err=%v", request.State, gotErr)
			}
		})
	}
}

func TestProjectDevelopmentCancellationRecoversLostACKProcessIdentity(t *testing.T) {
	server, edges, _ := developmentServer(t)
	view := developmentStart(t, server, "development-cancel-001")
	developmentRounds(t, server, 1)
	edges.lostStartACK = true
	_ = server.reconcileDevelopmentRequestsOnce(context.Background())
	if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
		t.Fatal(err)
	}
	developmentRounds(t, server, 1)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestCancelled || request.ProcessID == "" || edges.starts != 1 {
		t.Fatalf("request=%+v starts=%d", request, edges.starts)
	}
	for _, op := range edges.operations {
		if op.Kind == edge.OperationProjectProcessStop && op.Request.BackgroundProcessID != request.ProcessID {
			t.Fatal("stop retargeted")
		}
	}
}

func TestProjectDevelopmentParallelDistinctAndRepeatedStarts(t *testing.T) {
	server, edges, _ := developmentServer(t)
	var wg sync.WaitGroup
	results := make(chan string, 4)
	for index := range 4 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := fmt.Sprintf("development-parallel-%d", index%2)
			output, err := server.handleProjectDevelopmentStart(json.RawMessage(fmt.Sprintf(`{"alias":"project","target":"parrot","idempotency_key":%q,"argv":["go","test","./..."],"timeout_seconds":600}`, key)))
			if err != nil {
				results <- "error:" + err.Error()
				return
			}
			var view projectDevelopmentView
			_ = json.Unmarshal([]byte(output), &view)
			results <- view.RequestID
		}(index)
	}
	wg.Wait()
	close(results)
	ids := map[string]bool{}
	for id := range results {
		if !developmentRequestPattern.MatchString(id) {
			t.Fatal(id)
		}
		ids[id] = true
	}
	if len(ids) != 2 {
		t.Fatal("retries did not converge", ids)
	}
	developmentRounds(t, server, 5)
	if edges.starts != 2 {
		t.Fatalf("starts=%d", edges.starts)
	}
}

func TestProjectDevelopmentUnsupportedCapabilityAwaitsReasoningAndRemainsPinned(t *testing.T) {
	server, edges, _ := developmentServer(t)
	edges.missingCapability = true
	view := developmentStart(t, server, "development-missing-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestAwaitingReasoning || request.Reason != workqueue.DevelopmentRequestReasonNewRequirement || edges.starts != 0 {
		t.Fatalf("request=%+v", request)
	}
	if err := server.reconcileProjectTaskGoalPins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := server.developmentBody(context.Background(), request); err != nil {
		t.Fatal("development pin treated as orphan", err)
	}
}
