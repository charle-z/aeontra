package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/tools"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type developmentRunnerTestProvider struct {
	mu         sync.Mutex
	queue      *workqueue.Store
	calibrated bool
	pending    bool
	posts      int
	cancelled  int
	requests   []tools.DevelopmentRunnerRequest
}

func (*developmentRunnerTestProvider) Profile() string { return tools.DevelopmentRunnerProfile }
func (runner *developmentRunnerTestProvider) EnsureCalibration(context.Context) (tools.DevelopmentRunnerResult, error) {
	return tools.DevelopmentRunnerResult{Pending: true, State: "pending"}, nil
}
func (runner *developmentRunnerTestProvider) ConfiguredTemplateAttestation() (development.EnvironmentAttestation, error) {
	if !runner.calibrated {
		return development.EnvironmentAttestation{}, errors.New("calibration missing")
	}
	caps, _ := development.NewCapabilitySet("toolchain.go")
	return development.NewEnvironmentAttestation("github-hosted-template-test", development.ClassIsolatedRunner, 1, caps)
}
func (runner *developmentRunnerTestProvider) Start(_ context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.requests = append(runner.requests, request)
	effect, found, err := runner.queue.DevelopmentRunnerEffect(request.EffectID)
	if err != nil {
		return tools.DevelopmentRunnerResult{}, err
	}
	if !found {
		effect = workqueue.DevelopmentRunnerEffect{EffectID: request.EffectID, BindingDigest: request.PlanDigest, TemplateDigest: "sha256:" + strings.Repeat("e", 64), Revision: 1, WorkflowID: 31, CommandProfile: request.CommandProfile, JobID: request.Lease.Job.ID, Fence: request.Lease.Fence, State: "dispatch_intent", StartedAt: time.Now().UTC()}
		if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
		runner.posts++
	}
	if runner.pending {
		return tools.DevelopmentRunnerResult{State: "pending", Pending: true}, nil
	}
	if effect.State != "succeeded" {
		effect.Revision++
		effect.RunID = 91
		effect.State = "succeeded"
		effect.ReceiptDigest = "sha256:" + strings.Repeat("f", 64)
		if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
			return tools.DevelopmentRunnerResult{}, err
		}
	}
	return tools.DevelopmentRunnerResult{State: "succeeded", RunID: 91, ReceiptDigest: effect.ReceiptDigest, Capabilities: []development.CapabilityID{"toolchain.go"}}, nil
}
func (runner *developmentRunnerTestProvider) Reconcile(ctx context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	return runner.Start(ctx, request)
}
func (runner *developmentRunnerTestProvider) Cancel(_ context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.cancelled++
	effect, found, err := runner.queue.DevelopmentRunnerEffect(request.EffectID)
	if err != nil || !found {
		return tools.DevelopmentRunnerResult{}, errors.New("effect missing")
	}
	effect.Revision++
	effect.State = "cancel_intent"
	if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
		return tools.DevelopmentRunnerResult{}, err
	}
	effect.Revision++
	effect.State = "cancelled"
	if _, err := runner.queue.SaveDevelopmentRunnerEffect(effect, request.Lease); err != nil {
		return tools.DevelopmentRunnerResult{}, err
	}
	return tools.DevelopmentRunnerResult{State: "cancelled"}, nil
}

func runnerDevelopmentStart(t *testing.T, server *Server, key string) projectDevelopmentView {
	t.Helper()
	output, err := server.handleProjectDevelopmentStart(json.RawMessage(fmt.Sprintf(`{"alias":"project","target":"parrot","idempotency_key":%q,"argv":["go","test","./...","-count=1"],"timeout_seconds":4200,"runner_profile":"github-hosted-ubuntu24-v1"}`, key)))
	if err != nil {
		t.Fatal(err)
	}
	var view projectDevelopmentView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestProjectDevelopmentRunnerExactProviderReceiptNoForgedEdgeID(t *testing.T) {
	server, edges, _ := developmentServer(t)
	runner := &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-development-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if request.State != workqueue.DevelopmentRequestCompleted || objective.State != development.ObjectiveAccepted || runner.posts != 1 || edges.starts != 0 {
		t.Fatalf("request=%+v posts=%d localstarts=%d", request, runner.posts, edges.starts)
	}
	receipt := objective.Steps[0].AcceptanceReceipt
	if receipt == nil || receipt.OperationID != "" || receipt.EvidenceProvider != "github-run" || receipt.RunnerRunID != 91 || receipt.RunnerEffectID != runner.requests[0].EffectID || receipt.PrivateBodyDigest != request.BodyDigest {
		t.Fatalf("receipt=%+v", receipt)
	}
	if runner.requests[0].SourceOwner != "acme" || runner.requests[0].SourceRepo != "project" || runner.requests[0].SourceSHA != strings.Repeat("a", 40) || runner.requests[0].CommandProfile != "go-test-all" {
		t.Fatalf("source transport=%+v", runner.requests[0])
	}
}

func TestProjectDevelopmentRunnerMustCalibrateBeforeAnyPlanAttempt(t *testing.T) {
	server, _, _ := developmentServer(t)
	runner := &developmentRunnerTestProvider{queue: server.workQueue}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-calibration-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if len(objective.Steps[0].Attempts) != 0 || runner.posts != 0 || request.Reason != workqueue.DevelopmentRequestReasonCapabilityMissing {
		t.Fatal("uncalibrated runner became a planned attempt")
	}
}

func TestProjectDevelopmentRunnerRejectsPrivateOptionsBeforeStage(t *testing.T) {
	for _, extra := range []string{`,"cwd":"subdir"`, `,"stdin":"private input"`, `,"environment":{"CGO_ENABLED":"0"}`} {
		t.Run(extra, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			server.developmentRunner = &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}
			body := `{"alias":"project","target":"parrot","idempotency_key":"runner-options-001","argv":["make","validate-all"],"timeout_seconds":4200,"runner_profile":"github-hosted-ubuntu24-v1"` + extra + `}`
			if _, err := server.handleProjectDevelopmentStart(json.RawMessage(body)); err == nil {
				t.Fatal("private option silently dropped")
			}
			requests, err := server.workQueue.DevelopmentRequests(4)
			if err != nil || len(requests) != 0 || len(edges.operations) != 0 {
				t.Fatal("unsupported options staged or affected Edge")
			}
		})
	}
	if _, err := developmentRunnerCommandProfile([]string{"make", "validate-all"}, "", "", nil, 600); err == nil {
		t.Fatal("timeout silently replaced")
	}
	if _, err := developmentRunnerCommandProfile([]string{"go", "test", "./..."}, "", "", nil, 4200); err == nil {
		t.Fatal("argv silently replaced")
	}
}

func TestProjectDevelopmentRunnerCancelReconcilesCapturedEffect(t *testing.T) {
	server, edges, _ := developmentServer(t)
	runner := &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true, pending: true}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-cancel-001")
	developmentRounds(t, server, 2)
	if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
		t.Fatal(err)
	}
	developmentRounds(t, server, 1)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestCancelled || runner.cancelled != 1 || runner.posts != 1 || edges.starts != 0 {
		t.Fatalf("request=%+v posts=%d cancels=%d", request, runner.posts, runner.cancelled)
	}
}

func TestProjectDevelopmentRunnerSourceDriftCannotAccept(t *testing.T) {
	server, edges, _ := developmentServer(t)
	edges.sourceChanged = true
	runner := &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-drift-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if request.State != workqueue.DevelopmentRequestAwaitingReasoning || objective.State == development.ObjectiveAccepted {
		t.Fatal("source drift became acceptance")
	}
	effect, _, _ := server.workQueue.DevelopmentRunnerEffect(developmentRunnerEffect(request, objective))
	job, _, _ := server.workQueue.Get(effect.JobID)
	if job.State != workqueue.StateFailed {
		t.Fatal("source drift left runner lease active")
	}
}

func TestProjectDevelopmentRunnerMissingBodyCancelsOnlyCapturedEffect(t *testing.T) {
	for _, captured := range []bool{false, true} {
		t.Run(fmt.Sprint(captured), func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			runner := &developmentRunnerTestProvider{queue: server.workQueue, calibrated: true, pending: true}
			server.developmentRunner = runner
			view := runnerDevelopmentStart(t, server, "runner-body-lost-001")
			if captured {
				developmentRounds(t, server, 2)
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			refs := []modelturn.TaskGoalReference{{BodyRef: request.BodyRef, ContentDigest: request.BodyDigest}}
			if err := server.modelTurns.UnpinTaskGoalReferences(context.Background(), request.KeyDigest, refs); err != nil {
				t.Fatal(err)
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + request.ID + `"}`)); err != nil {
				t.Fatal(err)
			}
			err := server.reconcileDevelopmentRequestsOnce(context.Background())
			request, _, _ = server.workQueue.DevelopmentRequest(request.ID)
			if captured {
				if err != nil || request.State != workqueue.DevelopmentRequestCancelled || runner.cancelled != 1 || runner.posts != 1 {
					t.Fatalf("request=%+v error=%v", request, err)
				}
			} else if err == nil || request.State != workqueue.DevelopmentRequestCancelling || runner.cancelled != 0 || runner.posts != 0 {
				t.Fatal("missing body created or inferred an uncaptured effect")
			}
			if edges.starts != 0 {
				t.Fatal("runner cancellation started local command")
			}
		})
	}
}
