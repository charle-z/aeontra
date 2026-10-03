package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func (r *DevelopmentRunner) calibrationEffectID() string {
	if r.config.CalibrationEffectID != "" {
		return r.config.CalibrationEffectID
	}
	digest := sha256.Sum256([]byte("aeontra-development-runner-calibration-v1\x00" + r.templateDigest()))
	return hex.EncodeToString(digest[:])
}

// EnsureCalibration is a private explicit-opt-in effect, not an attestation
// read. It shares the existing queue and sole-POST intent protocol. A pending
// probe never plans or executes repository workload argv.
func (r *DevelopmentRunner) EnsureCalibration(ctx context.Context) (DevelopmentRunnerResult, error) {
	if r == nil || r.github == nil || r.queue == nil || ctx == nil || ctx.Err() != nil {
		return DevelopmentRunnerResult{}, errors.New("development runner calibration unavailable")
	}
	if r.config.CalibrationEffectID != "" {
		effect, found, err := r.queue.DevelopmentRunnerEffect(r.config.CalibrationEffectID)
		if err != nil || !found {
			return DevelopmentRunnerResult{}, errors.New("administrator calibration reference unavailable")
		}
		if _, err := r.ConfiguredTemplateAttestation(); err != nil {
			return DevelopmentRunnerResult{}, err
		}
		return runnerEffectResult(effect), nil
	}
	effectID := r.calibrationEffectID()
	suffix := effectID[:24]
	pool := "development.probe." + suffix
	if err := r.queue.PruneDevelopmentRunnerEffects([]string{effectID}); err != nil {
		return DevelopmentRunnerResult{}, err
	}
	job, _, err := r.queue.Enqueue(workqueue.Spec{IdempotencyKey: "development:probe:" + suffix, Workspace: "development.probe." + suffix, Pool: pool, Profile: r.config.Profile, PayloadHash: r.templateDigest()})
	if err != nil {
		return DevelopmentRunnerResult{}, err
	}
	if job.State == workqueue.StateLeased && !job.LeaseExpiresAt.After(time.Now().UTC()) {
		if err := r.queue.RecoverExpired(); err != nil {
			return DevelopmentRunnerResult{}, err
		}
		job, _, err = r.queue.Get(job.ID)
		if err != nil {
			return DevelopmentRunnerResult{}, err
		}
	}
	lease := workqueue.Lease{Job: job, ID: job.LeaseID, Fence: job.Fence, Attempt: job.Attempt, ExpiresAt: job.LeaseExpiresAt}
	if job.State == workqueue.StateQueued {
		lease, err = r.queue.LeaseNext(pool, "development-probe-holder", 2*time.Minute)
		if errors.Is(err, workqueue.ErrNoJobAvailable) {
			current, found, readErr := r.queue.Get(job.ID)
			if readErr != nil || !found {
				return DevelopmentRunnerResult{}, errors.New("calibration lease unavailable")
			}
			lease = workqueue.Lease{Job: current, ID: current.LeaseID, Fence: current.Fence, Attempt: current.Attempt, ExpiresAt: current.LeaseExpiresAt}
			err = nil
		}
		if err != nil {
			return DevelopmentRunnerResult{}, err
		}
	}
	if lease.Job.ID != job.ID || lease.ID == "" || lease.Fence == 0 {
		return DevelopmentRunnerResult{}, errors.New("calibration lease identity invalid")
	}
	if lease.Job.State == workqueue.StateLeased {
		if _, err := r.queue.Heartbeat(job.ID, lease.ID, lease.Fence, 2*time.Minute); err != nil {
			return DevelopmentRunnerResult{}, err
		}
	}
	sourceHash := sha256.Sum256([]byte("aeontra-development-calibration-git-identity-v1\x00" + r.github.owner + "/" + r.config.Repository + "\x00" + r.config.WorkflowSHA))
	request := DevelopmentRunnerRequest{EffectID: effectID, PlanDigest: r.templateDigest(), SourceOwner: r.github.owner, SourceRepo: r.config.Repository, SourceSHA: r.config.WorkflowSHA, SourceDigest: "sha256:" + hex.EncodeToString(sourceHash[:]), CommandProfile: "probe-only", Lease: lease}
	result, err := r.Start(ctx, request)
	if err != nil {
		return result, err
	}
	if result.Pending || lease.Job.State != workqueue.StateLeased {
		return result, nil
	}
	outcome := workqueue.StateFailed
	if result.State == "succeeded" {
		outcome = workqueue.StateSucceeded
	} else if result.State == "cancelled" {
		outcome = workqueue.StateCancelled
	}
	_, err = r.queue.Complete(job.ID, lease.ID, lease.Fence, workqueue.Result{Outcome: outcome, Summary: "isolated runner calibration " + strings.ReplaceAll(result.State, "_", " ")})
	return result, err
}
