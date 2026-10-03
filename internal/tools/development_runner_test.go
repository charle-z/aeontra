package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
	"go.yaml.in/yaml/v3"
)

func TestDevelopmentRunnerRequiresExplicitPublicProfile(t *testing.T) {
	if _, err := (&SourceCapability{}).NewDevelopmentRunner(DevelopmentRunnerConfig{}, nil); err == nil {
		t.Fatal("disabled runner enabled")
	}
}

func TestDevelopmentRunnerAutomaticCalibrationIsDurableAndNoWorkload(t *testing.T) {
	f := newRunnerFixture(t)
	f.req.EffectID = f.runner.calibrationEffectID()
	f.req.PlanDigest = f.runner.templateDigest()
	result, err := f.runner.EnsureCalibration(context.Background())
	if err != nil || !result.Pending || f.posts != 1 {
		t.Fatalf("probe=%+v posts=%d err=%v", result, f.posts, err)
	}
	if _, err := f.runner.ConfiguredTemplateAttestation(); err == nil {
		t.Fatal("queued automatic calibration gave capability")
	}
	if _, err := f.runner.EnsureCalibration(context.Background()); err != nil || f.posts != 1 {
		t.Fatalf("probe redispatch posts=%d err=%v", f.posts, err)
	}
	f.mu.Lock()
	f.visible = true
	f.status = "completed"
	f.conclusion = "success"
	f.mu.Unlock()
	result, err = f.runner.EnsureCalibration(context.Background())
	if err != nil || result.State != "succeeded" || f.posts != 1 {
		t.Fatalf("calibration=%+v err=%v", result, err)
	}
	attestation, err := f.runner.ConfiguredTemplateAttestation()
	if err != nil {
		t.Fatal(err)
	}
	required, _ := development.Requirements("toolchain.go.v1-26-8")
	if len(attestation.Capabilities.Missing(required)) != 0 {
		t.Fatal("calibrated runner does not attest pinned Go 1.26.8")
	}
	effect, found, err := f.runner.queue.DevelopmentRunnerEffect(f.req.EffectID)
	if err != nil || !found || effect.CommandProfile != "probe-only" {
		t.Fatal("automatic calibration ran workload")
	}
	job, found, err := f.runner.queue.Get(effect.JobID)
	if err != nil || !found || job.State != workqueue.StateSucceeded {
		t.Fatal("calibration lease did not settle")
	}
}

type runnerFixture struct {
	mu                                sync.Mutex
	posts, cancels, lists             int
	visible, duplicate, privateSource bool
	ack                               bool
	status, conclusion, failedStep    string
	probeName                         string
	headSHA                           string
	runRequest                        *DevelopmentRunnerRequest
	runner                            *DevelopmentRunner
	req                               DevelopmentRunnerRequest
	root                              string
}

func newRunnerFixture(t *testing.T) *runnerFixture {
	t.Helper()
	f := &runnerFixture{status: "queued", headSHA: strings.Repeat("a", 40), root: filepath.Join(t.TempDir(), "queue")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer dummy-runner-token" {
			t.Error("source broker authorization missing")
		}
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && (path == "/repos/acme/aeontra" || path == "/repos/upstream/project"):
			name := "acme/aeontra"
			private := f.privateSource
			if path == "/repos/upstream/project" {
				name = "upstream/project"
				private = f.privateSource
			}
			visibility := "public"
			if private {
				visibility = "private"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"full_name": name, "private": private, "visibility": visibility})
		case r.Method == http.MethodGet && strings.Contains(path, "/git/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": filepath.Base(path)})
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/git/ref/heads/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": strings.Repeat("a", 40)}})
		case r.Method == http.MethodGet && path == "/repos/acme/aeontra/actions/workflows/development-runner.yml":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": int64(31), "path": ".github/workflows/development-runner.yml", "state": "active"})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/dispatches"):
			f.posts++
			var body struct {
				Ref    string            `json:"ref"`
				Inputs map[string]string `json:"inputs"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Ref != "main" || len(body.Inputs) != 9 || body.Inputs["effect_id"] != f.req.EffectID || body.Inputs["command_profile"] != f.req.CommandProfile || body.Inputs["execution_digest"] != developmentRunnerExecutionDigest(f.req, f.runner.config.WorkflowSHA, 31) {
				t.Error("private fixed dispatch binding changed")
			}
			if f.ack {
				_ = json.NewEncoder(w).Encode(map[string]int64{"workflow_run_id": 91})
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/workflows/31/runs"):
			f.lists++
			if r.URL.Query().Get("head_sha") != strings.Repeat("a", 40) || r.URL.Query().Get("event") != "workflow_dispatch" || r.URL.Query().Get("per_page") != "100" {
				t.Error("run lookup was not exact bounded")
			}
			runs := []developmentRunnerGitHubRun{}
			if f.visible {
				runs = append(runs, f.run())
				if f.duplicate {
					run := f.run()
					run.ID++
					runs = append(runs, run)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case r.Method == http.MethodGet && path == "/repos/acme/aeontra/actions/runs/91":
			_ = json.NewEncoder(w).Encode(f.run())
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/runs/91/jobs"):
			steps := []githubActionsStep{}
			for i, name := range []string{"Validate immutable request", "Prepare disposable VM", "Fetch exact public Git objects", "Probe kernel and CI contracts (Go 1.26.8)", "Execute exact profile", "Bind trusted receipt"} {
				if name == "Probe kernel and CI contracts (Go 1.26.8)" && f.probeName != "" {
					name = f.probeName
				}
				conclusion := "success"
				if f.failedStep == name {
					conclusion = "failure"
				}
				steps = append(steps, githubActionsStep{Number: i + 1, Name: name, Status: "completed", Conclusion: conclusion})
			}
			conclusion := "success"
			if f.failedStep != "" {
				conclusion = "failure"
			}
			_ = json.NewEncoder(w).Encode(githubActionsJobsResponse{TotalCount: 1, Jobs: []githubActionsJob{{ID: 301, Name: "Isolated development execution", Status: "completed", Conclusion: conclusion, Steps: steps}}})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/runs/91/cancel"):
			f.cancels++
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Errorf("unexpected runner route %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	queue, err := workqueue.Open(workqueue.Config{Root: f.root, ControllerID: "runner-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	_, _, err = queue.Enqueue(workqueue.Spec{IdempotencyKey: "runner-effect-001", Workspace: "runner", Pool: "vps.build", Profile: "development-runner", PayloadHash: "sha256:" + strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := queue.LeaseNext("vps.build", "runner-holder-001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "dummy-runner-token", "acme", "org", "public"))
	f.runner, err = svc.SourceCapability.NewDevelopmentRunner(DevelopmentRunnerConfig{Profile: DevelopmentRunnerProfile, Repository: "aeontra", WorkflowRef: "main", WorkflowSHA: strings.Repeat("a", 40), Generation: 1}, queue)
	if err != nil {
		t.Fatal(err)
	}
	f.req = DevelopmentRunnerRequest{EffectID: strings.Repeat("e", 64), PlanDigest: "sha256:" + strings.Repeat("f", 64), SourceOwner: "acme", SourceRepo: "aeontra", SourceSHA: strings.Repeat("a", 40), SourceDigest: "sha256:" + strings.Repeat("d", 64), CommandProfile: "probe-only", Lease: lease}
	return f
}

func (f *runnerFixture) run() developmentRunnerGitHubRun {
	request := f.req
	if f.runRequest != nil {
		request = *f.runRequest
	}
	run := developmentRunnerGitHubRun{ID: 91, WorkflowID: 31, RunAttempt: 1, HeadSHA: f.headSHA, Path: ".github/workflows/development-runner.yml@main", Event: "workflow_dispatch", Title: "aeontra-development-" + request.EffectID + "-" + strings.TrimPrefix(request.PlanDigest, "sha256:") + "-" + developmentRunnerExecutionDigest(request, f.runner.config.WorkflowSHA, 31), Status: f.status, Conclusion: f.conclusion, CreatedAt: time.Now().UTC()}
	run.Repository.FullName = "acme/aeontra"
	return run
}

func TestDevelopmentRunnerRunLookupRejectsDifferentExecutedInputs(t *testing.T) {
	for name, mutate := range map[string]func(*DevelopmentRunnerRequest){
		"source owner":    func(request *DevelopmentRunnerRequest) { request.SourceOwner = "upstream" },
		"source repo":     func(request *DevelopmentRunnerRequest) { request.SourceRepo = "project" },
		"source SHA":      func(request *DevelopmentRunnerRequest) { request.SourceSHA = strings.Repeat("9", 40) },
		"command profile": func(request *DevelopmentRunnerRequest) { request.CommandProfile = "probe-only" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newRunnerFixture(t)
			f.req.CommandProfile = "go-test-all"
			actual := f.req
			mutate(&actual)
			f.runRequest = &actual
			f.visible = true
			f.status, f.conclusion = "completed", "success"
			effect := workqueue.DevelopmentRunnerEffect{StartedAt: time.Now().UTC()}
			_, found, err := f.runner.github.developmentRunnerFindRun(context.Background(), "aeontra", f.runner.config.WorkflowSHA, 31, f.req, effect)
			if err != nil || found {
				t.Fatalf("different executed %s matched original request: found=%v err=%v", name, found, err)
			}
		})
	}
}

func TestDevelopmentRunnerExecutionDigestFixture(t *testing.T) {
	request := DevelopmentRunnerRequest{EffectID: strings.Repeat("e", 64), PlanDigest: "sha256:" + strings.Repeat("f", 64), SourceOwner: "acme", SourceRepo: "aeontra", SourceSHA: strings.Repeat("a", 40), CommandProfile: "probe-only"}
	workflowSHA := strings.Repeat("a", 40)
	// Shared vector for the Go broker and the workflow's Python validator.
	const expected = "495a6fcd8c6b602f70bb8f845c37d22f8d6be02a4814bf0e00095172b14488d2"
	if actual := developmentRunnerExecutionDigest(request, workflowSHA, 31); actual != expected {
		t.Fatalf("execution digest=%s want=%s", actual, expected)
	}
	if developmentRunnerExecutionDigest(request, strings.Repeat("9", 40), 31) == expected || developmentRunnerExecutionDigest(request, workflowSHA, 32) == expected {
		t.Fatal("execution digest omitted template/workflow identity")
	}
	left, right := request, request
	left.SourceOwner, left.SourceRepo = "ab", "c"
	right.SourceOwner, right.SourceRepo = "a", "bc"
	if developmentRunnerExecutionDigest(left, workflowSHA, 31) == developmentRunnerExecutionDigest(right, workflowSHA, 31) {
		t.Fatal("execution digest did not delimit source fields")
	}
}

func TestDevelopmentRunnerLostAcknowledgementRestartNeverRedispatches(t *testing.T) {
	f := newRunnerFixture(t)
	ctx := context.Background()
	first, err := f.runner.Start(ctx, f.req)
	if err != nil || !first.Pending || first.RunID != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	effect, found, err := f.runner.queue.DevelopmentRunnerEffect(f.req.EffectID)
	if err != nil || !found || effect.State != "dispatch_intent" {
		t.Fatal("intent missing before acknowledgement")
	}
	if _, err = f.runner.Start(ctx, f.req); err != nil {
		t.Fatal(err)
	}
	if f.posts != 1 {
		t.Fatal("204 acknowledgement caused redispatch")
	}
	if err = f.runner.queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workqueue.Open(workqueue.Config{Root: f.root, ControllerID: "runner-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	f.runner.queue = reopened
	f.mu.Lock()
	f.visible = true
	f.mu.Unlock()
	recovered, err := f.runner.Reconcile(ctx, f.req)
	if err != nil || recovered.RunID != 91 || !recovered.Pending || f.posts != 1 {
		t.Fatalf("recovered=%+v err=%v posts=%d", recovered, err, f.posts)
	}
}

func TestDevelopmentRunnerUncertainPOSTNeverRedispatches(t *testing.T) {
	f := newRunnerFixture(t)
	realDo := f.runner.github.do
	f.runner.github.do = func(req *http.Request) (*http.Response, error) {
		response, err := realDo(req)
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/dispatches") {
			if response != nil {
				response.Body.Close()
			}
			return nil, fmt.Errorf("lost acknowledgement")
		}
		return response, err
	}
	result, err := f.runner.Start(context.Background(), f.req)
	if err != nil || result.State != "reconciliation_required" || !result.Pending {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for i := 0; i < 3; i++ {
		if _, err = f.runner.Start(context.Background(), f.req); err != nil {
			t.Fatal(err)
		}
	}
	if f.posts != 1 {
		t.Fatal("uncertain POST was replayed")
	}
}

func TestDevelopmentRunnerPrivateSourceAndStaleLeaseFailClosed(t *testing.T) {
	f := newRunnerFixture(t)
	f.privateSource = true
	if _, err := f.runner.Start(context.Background(), f.req); err == nil || f.posts != 0 {
		t.Fatal("private source transferred")
	}
	f.privateSource = false
	f.req.Lease.Fence++
	if _, err := f.runner.Start(context.Background(), f.req); err == nil || f.posts != 0 {
		t.Fatal("stale lease dispatched")
	}
}

func TestDevelopmentRunnerWorkloadRequiresPriorProbeOnlyCalibration(t *testing.T) {
	f := newRunnerFixture(t)
	f.req.CommandProfile = "go-test-all"
	if _, err := f.runner.Start(context.Background(), f.req); err == nil || f.posts != 0 {
		t.Fatal("workload executed before calibration")
	}
}

func TestDevelopmentRunnerReceiptNeedsExactTrustedRunAndCalibrationProfile(t *testing.T) {
	f := newRunnerFixture(t)
	f.ack = true
	if _, err := f.runner.Start(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.TemplateAttestation(f.req.EffectID); err == nil {
		t.Fatal("queued run granted capabilities")
	}
	f.mu.Lock()
	f.status = "completed"
	f.conclusion = "success"
	f.headSHA = strings.Repeat("9", 40)
	f.mu.Unlock()
	if result, err := f.runner.Reconcile(context.Background(), f.req); err == nil || result.ReceiptDigest != "" {
		t.Fatal("other head granted receipt")
	}
	f.mu.Lock()
	f.headSHA = strings.Repeat("a", 40)
	f.failedStep = "Probe kernel and CI contracts (Go 1.26.8)"
	f.mu.Unlock()
	if result, err := f.runner.Reconcile(context.Background(), f.req); err == nil || result.ReceiptDigest != "" {
		t.Fatal("failed trusted gate granted receipt")
	}
	f.mu.Lock()
	f.failedStep = ""
	f.mu.Unlock()
	result, err := f.runner.Reconcile(context.Background(), f.req)
	if err != nil || result.State != "succeeded" || result.Pending || result.ReceiptDigest == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	calibration, err := f.runner.TemplateAttestation(f.req.EffectID)
	if err != nil || !calibration.Valid() {
		t.Fatalf("calibration=%+v err=%v", calibration, err)
	}
	f.runner.config.Generation++
	if _, err := f.runner.TemplateAttestation(f.req.EffectID); err == nil {
		t.Fatal("changed template inherited calibration")
	}
}

func TestDevelopmentRunnerOldPersistedCalibrationCannotAttestNewGoPin(t *testing.T) {
	f := newRunnerFixture(t)
	identity := f.runner.config
	identity.CalibrationEffectID = ""
	body, _ := json.Marshal(identity)
	legacyHash := sha256.Sum256(append([]byte("aeontra-development-runner-template-v1\x00"), body...))
	legacyTemplate := "sha256:" + hex.EncodeToString(legacyHash[:])
	body, _ = json.Marshal(struct {
		TemplateDigest                                                                                string
		EffectID, PlanDigest, SourceOwner, SourceRepo, SourceSHA, SourceDigest, CommandProfile, JobID string
	}{legacyTemplate, f.req.EffectID, f.req.PlanDigest, f.req.SourceOwner, f.req.SourceRepo, f.req.SourceSHA, f.req.SourceDigest, f.req.CommandProfile, f.req.Lease.Job.ID})
	legacyHash = sha256.Sum256(append([]byte("aeontra-development-runner-binding-v1\x00"), body...))
	effect := workqueue.DevelopmentRunnerEffect{EffectID: f.req.EffectID, Revision: 1, BindingDigest: "sha256:" + hex.EncodeToString(legacyHash[:]), TemplateDigest: legacyTemplate, WorkflowID: 31, CommandProfile: "probe-only", JobID: f.req.Lease.Job.ID, Fence: f.req.Lease.Fence, State: "dispatch_intent", StartedAt: time.Now().UTC()}
	if _, err := f.runner.queue.SaveDevelopmentRunnerEffect(effect, f.req.Lease); err != nil {
		t.Fatal(err)
	}
	// Model an intact succeeded journal entry created by the prior binary.
	effect.Revision++
	effect.State, effect.RunID, effect.ReceiptDigest = "succeeded", 91, "sha256:"+strings.Repeat("3", 64)
	if _, err := f.runner.queue.SaveDevelopmentRunnerEffect(effect, f.req.Lease); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.TemplateAttestation(effect.EffectID); err == nil {
		t.Fatal("old successful calibration granted new pinned Go capability")
	}
	if _, err := f.runner.Start(context.Background(), f.req); err == nil {
		t.Fatal("old template binding was silently adopted")
	}
	if _, err := f.runner.Cancel(context.Background(), f.req); err == nil {
		t.Fatal("old template cancellation was silently retargeted")
	}
	preserved, found, err := f.runner.queue.DevelopmentRunnerEffect(effect.EffectID)
	if err != nil || !found || preserved.Revision != effect.Revision || preserved.TemplateDigest != legacyTemplate || preserved.ReceiptDigest != effect.ReceiptDigest || f.posts != 0 || f.cancels != 0 {
		t.Fatal("incompatible template rewrote journal or repeated an external effect", err)
	}
}

func TestDevelopmentRunnerOldUnversionedProbeCannotYieldReceipt(t *testing.T) {
	f := newRunnerFixture(t)
	f.ack = true
	f.probeName = "Probe kernel and CI contracts"
	if _, err := f.runner.Start(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.status = "completed"
	f.conclusion = "success"
	f.mu.Unlock()
	result, err := f.runner.Reconcile(context.Background(), f.req)
	if err == nil || result.ReceiptDigest != "" {
		t.Fatal("old unversioned probe granted new pinned-version receipt")
	}
	if _, err := f.runner.TemplateAttestation(f.req.EffectID); err == nil {
		t.Fatal("old unversioned probe granted calibrated capabilities")
	}
}

func TestDevelopmentRunnerCancellationReconcilesAndNeverReportsSuccess(t *testing.T) {
	f := newRunnerFixture(t)
	f.ack = true
	if _, err := f.runner.Start(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	result, err := f.runner.Cancel(context.Background(), f.req)
	if err != nil || !result.Pending || result.State != "cancel_intent" || f.cancels != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	f.mu.Lock()
	f.status = "completed"
	f.conclusion = "cancelled"
	f.mu.Unlock()
	result, err = f.runner.Reconcile(context.Background(), f.req)
	if err != nil || result.State != "cancelled" || result.Pending || result.ReceiptDigest != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDevelopmentRunnerAmbiguousRunAndChangedBindingAreNotReplayed(t *testing.T) {
	f := newRunnerFixture(t)
	if _, err := f.runner.Start(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.visible = true
	f.duplicate = true
	f.mu.Unlock()
	if result, err := f.runner.Reconcile(context.Background(), f.req); err == nil || !result.Pending {
		t.Fatal("ambiguous effect resolved")
	}
	f.req.SourceSHA = strings.Repeat("9", 40)
	if _, err := f.runner.Start(context.Background(), f.req); err == nil || f.posts != 1 {
		t.Fatal("effect source was retargeted")
	}
}

func TestDevelopmentRunnerFailureClassificationDoesNotEscalateCodeFailure(t *testing.T) {
	for _, item := range []struct {
		step string
		want development.FailureClass
	}{{"Execute exact profile", development.FailureCode}, {"Probe kernel and CI contracts (Go 1.26.8)", development.FailureCapabilityMissing}, {"Prepare disposable VM", development.FailureDependencyMissing}} {
		t.Run(item.step, func(t *testing.T) {
			f := newRunnerFixture(t)
			f.ack = true
			if _, err := f.runner.Start(context.Background(), f.req); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.status = "completed"
			f.conclusion = "failure"
			f.failedStep = item.step
			f.mu.Unlock()
			result, err := f.runner.Reconcile(context.Background(), f.req)
			if err != nil || result.State != "failed" || result.Failure != item.want || result.ReceiptDigest != "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestDevelopmentRunnerGoPinMatchesAttestedCapability(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{"go-version: '1.26.8'", "name: Probe kernel and CI contracts (Go 1.26.8)", "grep -Fx 'go version go1.26.8 linux/amd64'"} {
		if !strings.Contains(string(body), pin) {
			t.Errorf("workflow missing exact Go pin/probe %q", pin)
		}
	}
	result := runnerEffectResult(workqueue.DevelopmentRunnerEffect{State: "succeeded"})
	found := false
	for _, id := range result.Capabilities {
		found = found || id == "toolchain.go.v1-26-8"
		if id == "toolchain.go.v1-26-6" {
			t.Error("runner advertises obsolete exact Go version")
		}
	}
	if !found {
		t.Error("runner receipt missing exact pinned Go capability")
	}
}

func TestDevelopmentRunnerWorkflowIsFixedAndContainmentGatesAreMandatory(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			RunsOn string           `yaml:"runs-on"`
			Steps  []map[string]any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if yaml.Unmarshal(body, &workflow) != nil || len(workflow.Jobs) != 1 || len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("workflow authority changed")
	}
	for _, job := range workflow.Jobs {
		if job.RunsOn != "ubuntu-24.04" {
			t.Fatal("paid or non-VM runner configured")
		}
		for _, step := range job.Steps {
			if value, ok := step["uses"].(string); ok && !strings.HasPrefix(value, "./") {
				parts := strings.Split(value, "@")
				if len(parts) != 2 || !developmentRunnerSHA.MatchString(parts[1]) {
					t.Fatal("action is not full-SHA pinned")
				}
			}
			if _, ok := step["continue-on-error"]; ok {
				t.Fatal("gate weakened")
			}
		}
	}
	for _, required := range []string{"sudo mount -o remount,hidepid=2 /proc", "User=aeontra-workload", "MemoryMax=10G", "TasksMax=4096", "--net=slirp4netns", "--disable-host-loopback", "--map-root-user --mount --pid --fork", "version=2,scope=$EFFECT_ID", "builder-import", "make validate-all", "go test ./... -count=1", "GOTOOLCHAIN", "fetch --no-tags origin \"$SOURCE_SHA\"", "probe-only)", "inputs.execution_digest", "execution input binding mismatch", "struct.pack('>I', len(value))"} {
		if !strings.Contains(string(body), required) {
			t.Errorf("missing immutable gate %s", required)
		}
	}
	if strings.Contains(string(body), "${{ secrets.") || strings.Contains(string(body), "pull_request:") || strings.Contains(string(body), "self-hosted") {
		t.Fatal("ambient secret or automatic untrusted workflow authority added")
	}
}

func TestDevelopmentRunnerRootlessNetworkUtilitiesRemainReachable(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Environment=PATH=/opt/aeontra-bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"'PATH': '/opt/aeontra-bin:/opt/aeontra-go/bin:/usr/bin:/bin:/usr/sbin:/sbin'",
	} {
		if !strings.Contains(string(body), required) {
			t.Errorf("rootless runtime cannot find system networking utilities: %s", required)
		}
	}
}

func TestDevelopmentRunnerUsesBoundedUserManager(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"/etc/systemd/user/aeontra-rootless.service",
		"/etc/systemd/system/user@${workload_uid}.service.d",
		"Delegate=cpu cpuset io memory pids",
		"sudo loginctl enable-linger aeontra-workload",
		"sudo systemctl start \"user@${workload_uid}.service\"",
		"Environment=XDG_RUNTIME_DIR=%t",
		"Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=%t/bus",
		"systemctl --user start aeontra-rootless.service",
		"systemctl --user stop aeontra-rootless.service",
		"/user.slice/user-${workload_uid}.slice/user@${workload_uid}.service/app.slice/aeontra-rootless.service",
		"test \"$(sudo cat \"$manager_cgroup/memory.max\")\" = 10737418240",
		"test \"$(sudo cat \"$manager_cgroup/pids.max\")\" = 4096",
		"^ Cgroup Driver: systemd$",
		"^ Cgroup Version: 2$",
		"--memory 256m --pids-limit 64",
		"cat /sys/fs/cgroup/memory.max",
		"cat /sys/fs/cgroup/pids.max",
		"_SYSTEMD_USER_UNIT=aeontra-rootless.service",
	} {
		if !strings.Contains(string(body), required) {
			t.Errorf("missing rootless user-manager invariant: %s", required)
		}
	}
	if strings.Contains(string(body), "/etc/systemd/system/aeontra-rootless.service") {
		t.Error("rootless daemon still uses unsupported system-wide User= service")
	}
}

func TestDevelopmentRunnerWorkflowValidatesPublicEventAndExecutionBinding(t *testing.T) {
	bash, bashErr := exec.LookPath("bash")
	python, pythonErr := exec.LookPath("python3")
	if bashErr != nil || pythonErr != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("hosted Linux validator requires bash and python3; native Windows availability: bash=%v python3=%v", bashErr, pythonErr)
		}
		t.Fatalf("hosted Linux validator tools unavailable: bash=%v python3=%v", bashErr, pythonErr)
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	if output, err := exec.CommandContext(probeCtx, python, "--version").CombinedOutput(); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("python3 is not executable on this native Windows host: %v: %s", err, output)
		}
		t.Fatalf("python3 is not executable: %v: %s", err, output)
	}
	if runtime.GOOS == "windows" {
		command := exec.CommandContext(probeCtx, bash, "--noprofile", "--norc", "-c", `python3 -c 'import os; print(os.name)'`)
		command.Env = []string{"PATH=" + os.Getenv("PATH")}
		if output, err := command.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "nt" {
			t.Skipf("native Windows bash/python3 pair cannot use Windows fixture paths; run this Linux validator in WSL: %v: %s", err, output)
		}
	}
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name, Shell, Run string
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	steps := workflow.Jobs["execution"].Steps
	if len(steps) == 0 || steps[0].Name != "Validate immutable request" || steps[0].Shell != "bash" || steps[0].Run == "" {
		t.Fatal("trusted immutable-request validator is not the first execution step")
	}
	const publicEvent = `{"repository":{"private":false,"full_name":"acme/aeontra"}}`
	cases := []struct {
		name, event, environmentOverride string
		missingFile                      bool
		pass                             bool
	}{
		{name: "public", event: publicEvent, pass: true},
		{name: "private", event: `{"repository":{"private":true,"full_name":"acme/aeontra"}}`},
		{name: "missing repository", event: `{}`},
		{name: "missing private", event: `{"repository":{"full_name":"acme/aeontra"}}`},
		{name: "null private", event: `{"repository":{"private":null,"full_name":"acme/aeontra"}}`},
		{name: "string private", event: `{"repository":{"private":"false","full_name":"acme/aeontra"}}`},
		{name: "numeric private", event: `{"repository":{"private":0,"full_name":"acme/aeontra"}}`},
		{name: "wrong identity", event: `{"repository":{"private":false,"full_name":"upstream/project"}}`},
		{name: "missing identity", event: `{"repository":{"private":false}}`},
		{name: "null identity", event: `{"repository":{"private":false,"full_name":null}}`},
		{name: "malformed repository", event: `{"repository":[]}`},
		{name: "malformed event", event: `[]`},
		{name: "invalid JSON", event: `{`},
		{name: "missing event file", missingFile: true},
		{name: "asserted digest mismatch", event: publicEvent, environmentOverride: "EXECUTION_DIGEST=" + strings.Repeat("9", 64)},
		{name: "effect binding mismatch", event: publicEvent, environmentOverride: "EFFECT_ID=" + strings.Repeat("9", 64)},
		{name: "plan binding mismatch", event: publicEvent, environmentOverride: "PLAN_DIGEST=" + strings.Repeat("9", 64)},
		{name: "owner binding mismatch", event: publicEvent, environmentOverride: "SOURCE_OWNER=upstream"},
		{name: "repo binding mismatch", event: publicEvent, environmentOverride: "SOURCE_REPO=project"},
		{name: "source binding mismatch", event: publicEvent, environmentOverride: "SOURCE_SHA=" + strings.Repeat("9", 40)},
		{name: "profile binding mismatch", event: publicEvent, environmentOverride: "COMMAND_PROFILE=go-test-all"},
		{name: "template binding mismatch", event: publicEvent, environmentOverride: "WORKFLOW_SHA=" + strings.Repeat("9", 40)},
		{name: "workflow binding mismatch", event: publicEvent, environmentOverride: "WORKFLOW_ID=32"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			eventPath := filepath.Join(t.TempDir(), "event.json")
			if !test.missingFile {
				if err := os.WriteFile(eventPath, []byte(test.event), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c", steps[0].Run)
			command.Env = []string{
				"PATH=" + os.Getenv("PATH"), "GITHUB_EVENT_NAME=workflow_dispatch", "GITHUB_RUN_ATTEMPT=1",
				"GITHUB_SHA=" + strings.Repeat("a", 40), "GITHUB_REPOSITORY=acme/aeontra", "GITHUB_EVENT_PATH=" + eventPath,
				"EFFECT_ID=" + strings.Repeat("e", 64), "PLAN_DIGEST=" + strings.Repeat("f", 64),
				"SOURCE_OWNER=acme", "SOURCE_REPO=aeontra", "SOURCE_SHA=" + strings.Repeat("a", 40),
				"COMMAND_PROFILE=probe-only", "WORKFLOW_SHA=" + strings.Repeat("a", 40), "WORKFLOW_ID=31",
				"EXECUTION_DIGEST=495a6fcd8c6b602f70bb8f845c37d22f8d6be02a4814bf0e00095172b14488d2",
			}
			if test.environmentOverride != "" {
				name, _, _ := strings.Cut(test.environmentOverride, "=")
				for index, value := range command.Env {
					if strings.HasPrefix(value, name+"=") {
						command.Env[index] = test.environmentOverride
					}
				}
			}
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("validator exceeded deadline: %v", ctx.Err())
			}
			if (err == nil) != test.pass {
				t.Fatalf("validator pass=%v want=%v err=%v output=%s", err == nil, test.pass, err, output)
			}
		})
	}
}

func TestDevelopmentRunnerRejectsMutableSourceBeforeDispatch(t *testing.T) {
	runner := &DevelopmentRunner{}
	if _, err := runner.Start(context.Background(), DevelopmentRunnerRequest{SourceSHA: "main"}); err == nil {
		t.Fatal("mutable source accepted")
	}
}
