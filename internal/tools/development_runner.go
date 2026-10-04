package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

const DevelopmentRunnerProfile = "github-hosted-ubuntu24-v1"
const DevelopmentRunnerGoVersion = "1.26.8"
const developmentRunnerWorkflow = "development-runner.yml"
const developmentRunnerProbeStep = "Probe kernel and CI contracts (Go " + DevelopmentRunnerGoVersion + ")"

var developmentRunnerSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var developmentRunnerEffectID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var developmentRunnerDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type DevelopmentRunnerConfig struct {
	Profile             string
	Repository          string
	WorkflowSHA         string
	WorkflowRef         string
	Generation          uint64
	CalibrationEffectID string
}

// DevelopmentRunnerRequest is private server-selected metadata, not an MCP
// schema. A caller never supplies raw argv, environment, credentials or source.
type DevelopmentRunnerRequest struct {
	EffectID       string
	PlanDigest     string
	SourceOwner    string
	SourceRepo     string
	SourceSHA      string
	SourceDigest   string
	CommandProfile string
	Lease          workqueue.Lease
}

type DevelopmentRunnerResult struct {
	Pending       bool
	State         string
	RunID         int64
	ReceiptDigest string
	Failure       development.FailureClass
	Capabilities  []development.CapabilityID
}

type DevelopmentRunner struct {
	config      DevelopmentRunnerConfig
	github      *GitHubClient
	queue       *workqueue.Store
	log         *audit.Logger
	effectLocks [64]sync.Mutex
}

// Profile exposes only the administrator-selected immutable profile name.
func (r *DevelopmentRunner) Profile() string {
	if r == nil {
		return ""
	}
	return r.config.Profile
}

// ValidateDevelopmentRunnerConfig checks the closed administrator profile
// before opening state stores. An empty profile is disabled, not a valid runner.
func ValidateDevelopmentRunnerConfig(config DevelopmentRunnerConfig) error {
	if config.Profile != DevelopmentRunnerProfile || !safeCloneDir(config.Repository) || !developmentRunnerSHA.MatchString(config.WorkflowSHA) || !safeGitHubRef(config.WorkflowRef) || strings.TrimSpace(config.WorkflowRef) != config.WorkflowRef || strings.HasPrefix(config.WorkflowRef, "refs/") || config.Generation == 0 || config.Generation > 1<<63-1 || config.CalibrationEffectID != "" && !developmentRunnerEffectID.MatchString(config.CalibrationEffectID) {
		return errors.New("development runner configuration is invalid")
	}
	return nil
}

func (r *DevelopmentRunner) effectLock(effectID string) *sync.Mutex {
	digest := sha256.Sum256([]byte(effectID))
	return &r.effectLocks[int(digest[0])%len(r.effectLocks)]
}

func (s *SourceCapability) NewDevelopmentRunner(config DevelopmentRunnerConfig, queue *workqueue.Store) (*DevelopmentRunner, error) {
	if s == nil || s.serviceCore == nil || s.log == nil || s.github == nil || !s.github.Configured() || queue == nil || ValidateDevelopmentRunnerConfig(config) != nil {
		return nil, errors.New("development runner is disabled or configuration is invalid")
	}
	// Only the configured owner-bound source broker is allowed to dispatch.
	return &DevelopmentRunner{config: config, github: s.github, queue: queue, log: s.log}, nil
}

func (r *DevelopmentRunner) validate(req DevelopmentRunnerRequest) error {
	if r == nil || r.github == nil || r.queue == nil || r.log == nil || r.config.Profile != DevelopmentRunnerProfile || !developmentRunnerEffectID.MatchString(req.EffectID) || !developmentRunnerDigest.MatchString(req.PlanDigest) || !developmentRunnerDigest.MatchString(req.SourceDigest) || !safeGitHubOwner(req.SourceOwner) || !safeCloneDir(req.SourceRepo) || !developmentRunnerSHA.MatchString(req.SourceSHA) || req.Lease.Fence == 0 || req.Lease.ID == "" || req.Lease.Job.ID == "" {
		return errors.New("development runner request is invalid")
	}
	if req.CommandProfile != "probe-only" && req.CommandProfile != "make-validate-all" && req.CommandProfile != "go-test-all" {
		return errors.New("development runner command profile is not registered")
	}
	if req.CommandProfile == "probe-only" && (req.SourceOwner != r.github.owner || req.SourceRepo != r.config.Repository || req.SourceSHA != r.config.WorkflowSHA) {
		return errors.New("development runner calibration must use its exact public template source")
	}
	return nil
}

// ConfiguredTemplateAttestation is the calibrated candidate selected by the
// administrator. Execution repeats its gates in a new run before any workload.
func (r *DevelopmentRunner) ConfiguredTemplateAttestation() (development.EnvironmentAttestation, error) {
	if r == nil {
		return development.EnvironmentAttestation{}, errors.New("development runner unavailable")
	}
	return r.TemplateAttestation(r.calibrationEffectID())
}

func (r *DevelopmentRunner) binding(req DevelopmentRunnerRequest) string {
	body, _ := json.Marshal(struct {
		TemplateDigest                                                                                string
		EffectID, PlanDigest, SourceOwner, SourceRepo, SourceSHA, SourceDigest, CommandProfile, JobID string
	}{r.templateDigest(), req.EffectID, req.PlanDigest, req.SourceOwner, req.SourceRepo, req.SourceSHA, req.SourceDigest, req.CommandProfile, req.Lease.Job.ID})
	digest := sha256.Sum256(append([]byte("aeontra-development-runner-binding-v1\x00"), body...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (r *DevelopmentRunner) templateDigest() string {
	identity := r.config
	identity.CalibrationEffectID = ""
	body, _ := json.Marshal(identity)
	// Bind compiled capability interpretation as well as administrator pins.
	// A prior successful calibration must not attest a new fixed Go version.
	digest := sha256.Sum256(append([]byte("aeontra-development-runner-template-v1\x00"+developmentRunnerProbeStep+"\x00"), body...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// TemplateAttestation requires a verified calibration of this exact immutable
// template/generation. This is a provisionable profile, not a living VM lease.
// Every subsequent execution still passes all fresh gates in the workflow.
func (r *DevelopmentRunner) TemplateAttestation(calibrationEffectID string) (development.EnvironmentAttestation, error) {
	if r == nil || r.queue == nil {
		return development.EnvironmentAttestation{}, errors.New("development runner calibration is unavailable")
	}
	effect, found, err := r.queue.DevelopmentRunnerEffect(calibrationEffectID)
	if err != nil || !found || effect.State != "succeeded" || effect.CommandProfile != "probe-only" || effect.TemplateDigest != r.templateDigest() {
		return development.EnvironmentAttestation{}, errors.New("development runner exact-template calibration is missing")
	}
	ids := runnerEffectResult(effect).Capabilities
	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = string(id)
	}
	caps, err := development.NewCapabilitySet(raw...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation("github-hosted-template-"+strings.TrimPrefix(r.templateDigest(), "sha256:")[:16], development.ClassIsolatedRunner, r.config.Generation, caps)
}

// Start persists dispatch intent before its sole POST. Repeated Start recovers
// existing intent; it does not repeat a POST, even when the API returns no run ID.
func (r *DevelopmentRunner) Start(ctx context.Context, req DevelopmentRunnerRequest) (result DevelopmentRunnerResult, err error) {
	if r != nil && r.log != nil {
		defer r.audit("development_runner_start", &err)
	}
	if err = r.validate(req); err != nil {
		return result, err
	}
	lock := r.effectLock(req.EffectID)
	lock.Lock()
	defer lock.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return result, errors.New("development runner context unavailable")
	}
	ctx, phaseCancel := context.WithTimeout(ctx, 15*time.Second)
	defer phaseCancel()
	old, found, err := r.queue.DevelopmentRunnerEffect(req.EffectID)
	if err != nil {
		return result, err
	}
	if found {
		return r.reconcile(ctx, req, old, false)
	}
	if req.CommandProfile != "probe-only" {
		if _, err = r.ConfiguredTemplateAttestation(); err != nil {
			return result, err
		}
	}
	if err = r.queue.PruneDevelopmentRunnerEffects([]string{r.calibrationEffectID()}); err != nil {
		return result, err
	}
	// Public source transfer is explicit. No private/dirty workspace is uploaded.
	if err = r.github.developmentRunnerPublicSource(ctx, r.github.owner, r.config.Repository, r.config.WorkflowSHA); err != nil {
		return result, err
	}
	if err = r.github.developmentRunnerPublicSource(ctx, req.SourceOwner, req.SourceRepo, req.SourceSHA); err != nil {
		return result, err
	}
	workflow, err := r.github.workflow(ctx, r.config.Repository, developmentRunnerWorkflow)
	if err != nil {
		return result, err
	}
	refSHA, err := r.github.branchSHA(ctx, r.config.Repository, r.config.WorkflowRef)
	if err != nil || refSHA != r.config.WorkflowSHA {
		return result, errors.New("development runner workflow ref changed from administrator pin")
	}
	old = workqueue.DevelopmentRunnerEffect{EffectID: req.EffectID, Revision: 1, BindingDigest: r.binding(req), TemplateDigest: r.templateDigest(), WorkflowID: workflow.ID, CommandProfile: req.CommandProfile, JobID: req.Lease.Job.ID, Fence: req.Lease.Fence, State: "dispatch_intent", StartedAt: time.Now().UTC()}
	created, err := r.queue.SaveDevelopmentRunnerEffect(old, req.Lease)
	if err != nil {
		return result, err
	}
	if !created {
		return result, errors.New("development runner dispatch intent already exists")
	}
	inputs := map[string]string{"effect_id": req.EffectID, "plan_digest": strings.TrimPrefix(req.PlanDigest, "sha256:"), "source_owner": req.SourceOwner, "source_repo": req.SourceRepo, "source_sha": req.SourceSHA, "command_profile": req.CommandProfile, "workflow_sha": r.config.WorkflowSHA, "workflow_id": fmt.Sprint(workflow.ID), "execution_digest": developmentRunnerExecutionDigest(req, r.config.WorkflowSHA, workflow.ID)}
	response, dispatchErr := r.github.dispatchWorkflow(ctx, r.config.Repository, developmentRunnerWorkflow, r.config.WorkflowRef, inputs)
	// Any dispatch error is uncertain after intent: retain it and reconcile. A
	// successful 204 also has no run ID on older GitHub API versions.
	if response.WorkflowRunID > 0 {
		old.Revision++
		old.RunID = response.WorkflowRunID
		old.State = "pending"
		if _, err = r.queue.SaveDevelopmentRunnerEffect(old, req.Lease); err != nil {
			return DevelopmentRunnerResult{Pending: true, State: "reconciliation_required"}, err
		}
	}
	if dispatchErr != nil {
		return DevelopmentRunnerResult{Pending: true, State: "reconciliation_required", RunID: old.RunID}, nil
	}
	return DevelopmentRunnerResult{Pending: true, State: old.State, RunID: old.RunID}, nil
}

func (r *DevelopmentRunner) Reconcile(ctx context.Context, req DevelopmentRunnerRequest) (result DevelopmentRunnerResult, err error) {
	return r.inspect(ctx, req, false)
}
func (r *DevelopmentRunner) Status(ctx context.Context, req DevelopmentRunnerRequest) (result DevelopmentRunnerResult, err error) {
	return r.inspect(ctx, req, false)
}
func (r *DevelopmentRunner) Cancel(ctx context.Context, req DevelopmentRunnerRequest) (result DevelopmentRunnerResult, err error) {
	return r.inspect(ctx, req, true)
}

func (r *DevelopmentRunner) inspect(ctx context.Context, req DevelopmentRunnerRequest, cancel bool) (result DevelopmentRunnerResult, err error) {
	if r != nil && r.log != nil {
		defer r.audit("development_runner_reconcile", &err)
	}
	if err = r.validate(req); err != nil {
		return result, err
	}
	lock := r.effectLock(req.EffectID)
	lock.Lock()
	defer lock.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return result, errors.New("development runner context unavailable")
	}
	ctx, phaseCancel := context.WithTimeout(ctx, 15*time.Second)
	defer phaseCancel()
	old, found, err := r.queue.DevelopmentRunnerEffect(req.EffectID)
	if err != nil || !found {
		return result, errors.New("development runner effect not found")
	}
	return r.reconcile(ctx, req, old, cancel)
}

func (r *DevelopmentRunner) reconcile(ctx context.Context, req DevelopmentRunnerRequest, old workqueue.DevelopmentRunnerEffect, cancel bool) (DevelopmentRunnerResult, error) {
	if old.BindingDigest != r.binding(req) || old.JobID != req.Lease.Job.ID || old.Fence > req.Lease.Fence {
		return DevelopmentRunnerResult{}, errors.New("development runner effect binding changed")
	}
	if old.State == "succeeded" || old.State == "failed" || old.State == "cancelled" {
		return runnerEffectResult(old), nil
	}
	// Persist the fresh fence before any remote read or cancellation.
	old.Revision++
	old.Fence = req.Lease.Fence
	if cancel {
		old.State = "cancel_intent"
	}
	if _, err := r.queue.SaveDevelopmentRunnerEffect(old, req.Lease); err != nil {
		return DevelopmentRunnerResult{}, err
	}
	workflow, err := r.github.workflow(ctx, r.config.Repository, developmentRunnerWorkflow)
	if err != nil {
		return DevelopmentRunnerResult{}, err
	}
	if workflow.ID != old.WorkflowID {
		return DevelopmentRunnerResult{}, errors.New("development runner workflow identity changed")
	}
	run, found, err := r.github.developmentRunnerFindRun(ctx, r.config.Repository, r.config.WorkflowSHA, workflow.ID, req, old)
	if err != nil {
		return DevelopmentRunnerResult{Pending: true, State: "reconciliation_required", RunID: old.RunID}, err
	}
	if !found {
		return DevelopmentRunnerResult{Pending: true, State: "reconciliation_required"}, nil
	}
	old.RunID = run.ID
	if old.State == "cancel_intent" && run.Status != "completed" {
		if err = r.github.developmentRunnerCancel(ctx, r.config.Repository, run.ID); err != nil {
			return DevelopmentRunnerResult{Pending: true, State: "cancel_intent", RunID: run.ID}, err
		}
	} else if run.Status == "completed" {
		switch run.Conclusion {
		case "success":
			if old.State == "cancel_intent" {
				old.State = "cancelled"
				break
			}
			if err = r.github.developmentRunnerVerifySteps(ctx, r.config.Repository, run.ID); err != nil {
				return DevelopmentRunnerResult{}, err
			}
			old.State = "succeeded"
			digest := sha256.Sum256([]byte(fmt.Sprintf("aeontra-development-runner-receipt-v1\x00%s\x00%d\x00%d\x00%s", old.BindingDigest, run.ID, run.RunAttempt, r.config.WorkflowSHA)))
			old.ReceiptDigest = "sha256:" + hex.EncodeToString(digest[:])
		case "cancelled":
			old.State = "cancelled"
		default:
			old.Failure, err = r.github.developmentRunnerFailure(ctx, r.config.Repository, run.ID)
			if err != nil {
				return DevelopmentRunnerResult{}, err
			}
			old.State = "failed"
		}
	} else if old.State != "cancel_intent" {
		old.State = "pending"
	}
	old.Revision++
	if _, err = r.queue.SaveDevelopmentRunnerEffect(old, req.Lease); err != nil {
		return DevelopmentRunnerResult{}, err
	}
	return runnerEffectResult(old), nil
}

func runnerEffectResult(effect workqueue.DevelopmentRunnerEffect) DevelopmentRunnerResult {
	result := DevelopmentRunnerResult{State: effect.State, RunID: effect.RunID, ReceiptDigest: effect.ReceiptDigest, Pending: effect.State == "dispatch_intent" || effect.State == "pending" || effect.State == "cancel_intent"}
	if effect.State == "failed" {
		result.Failure = effect.Failure
	}
	// Capabilities are measured in this completed VM. They do not advertise a
	// currently live VM and cannot authorize another attempt without a fresh gate.
	if effect.State == "succeeded" {
		result.Capabilities = []development.CapabilityID{"build.make", "cgroup.v2.delegated", "ci.github-actions.cache", "ci.github-actions.runtime", "container.docker.rootless", "filesystem.workspace-bind", "git.metadata.full", "idmap.subuid", "namespace.user.nested", "toolchain.go", "toolchain.go.v1", "toolchain.go.v1-26", "toolchain.go.v1-26-8"}
	}
	return result
}

func (r *DevelopmentRunner) audit(operation string, err *error) {
	decision := audit.Allow
	message := ""
	if *err != nil {
		decision = audit.Error
		message = "development runner operation failed"
	}
	_ = r.log.Log(audit.Entry{Tool: operation, Decision: decision, Args: "provider=" + DevelopmentRunnerProfile, Error: message})
}
