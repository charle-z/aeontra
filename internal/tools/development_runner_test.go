package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if _, err := f.runner.ConfiguredTemplateAttestation(); err != nil {
		t.Fatal(err)
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
			for i, name := range []string{"Validate immutable request", "Prepare disposable VM", "Fetch exact public Git objects", "Probe kernel and CI contracts", "Execute exact profile", "Bind trusted receipt"} {
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
	f.failedStep = "Probe kernel and CI contracts"
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
	}{{"Execute exact profile", development.FailureCode}, {"Probe kernel and CI contracts", development.FailureCapabilityMissing}, {"Prepare disposable VM", development.FailureDependencyMissing}} {
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

func TestDevelopmentRunnerRejectsMutableSourceBeforeDispatch(t *testing.T) {
	runner := &DevelopmentRunner{}
	if _, err := runner.Start(context.Background(), DevelopmentRunnerRequest{SourceSHA: "main"}); err == nil {
		t.Fatal("mutable source accepted")
	}
}
