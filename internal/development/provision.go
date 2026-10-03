package development

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
)

const (
	ProvisionPlanVersion           = 1
	MaxProvisioningAttemptsPerStep = 16
	provisionPlanDigestDomain      = "aeontra-development-provision-plan-v1\x00"
)

var (
	provisionIdentityPattern = regexp.MustCompile("^[a-z][a-z0-9._-]{0,63}$")
	provisionJobIDPattern    = regexp.MustCompile("^wj_[a-f0-9]{32}$")
	digestPattern            = regexp.MustCompile("^sha256:[a-f0-9]{64}$")
)

type ProvisionPlan struct {
	Version               int            `json:"version"`
	Provider              string         `json:"provider"`
	Pool                  string         `json:"pool"`
	Profile               string         `json:"profile"`
	OutputClass           ExecutionClass `json:"output_class"`
	BaseEnvironmentDigest string         `json:"base_environment_digest,omitempty"`
	Capabilities          []CapabilityID `json:"capabilities"`
	Digest                string         `json:"digest"`
}

func NewProvisionPlan(provider, pool, profile string, outputClass ExecutionClass, baseEnvironmentDigest string, capabilities []CapabilityID) (ProvisionPlan, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	pool = strings.ToLower(strings.TrimSpace(pool))
	profile = strings.ToLower(strings.TrimSpace(profile))
	baseEnvironmentDigest = strings.ToLower(strings.TrimSpace(baseEnvironmentDigest))
	if !provisionIdentityPattern.MatchString(provider) ||
		!provisionIdentityPattern.MatchString(pool) ||
		!provisionIdentityPattern.MatchString(profile) {
		return ProvisionPlan{}, errors.New("development provision plan identity is invalid")
	}
	if _, ok := outputClass.Tier(); !ok {
		return ProvisionPlan{}, errors.New("development provision plan execution class is invalid")
	}
	if baseEnvironmentDigest != "" && !digestPattern.MatchString(baseEnvironmentDigest) {
		return ProvisionPlan{}, errors.New("development provision plan base environment is invalid")
	}
	canonical, err := canonicalCapabilityIDs(capabilities)
	if err != nil || len(canonical) == 0 || len(canonical) > MaxEnvironmentCatalogEntries {
		return ProvisionPlan{}, errors.New("development provision plan capabilities are invalid")
	}
	plan := ProvisionPlan{
		Version:               ProvisionPlanVersion,
		Provider:              provider,
		Pool:                  pool,
		Profile:               profile,
		OutputClass:           outputClass,
		BaseEnvironmentDigest: baseEnvironmentDigest,
		Capabilities:          canonical,
	}
	plan.Digest = provisionPlanDigest(plan)
	return plan, nil
}

func (plan ProvisionPlan) Valid() bool {
	if plan.Version != ProvisionPlanVersion ||
		!provisionIdentityPattern.MatchString(plan.Provider) ||
		!provisionIdentityPattern.MatchString(plan.Pool) ||
		!provisionIdentityPattern.MatchString(plan.Profile) ||
		(plan.BaseEnvironmentDigest != "" && !digestPattern.MatchString(plan.BaseEnvironmentDigest)) ||
		!digestPattern.MatchString(plan.Digest) {
		return false
	}
	if _, ok := plan.OutputClass.Tier(); !ok {
		return false
	}
	canonical, err := canonicalCapabilityIDs(plan.Capabilities)
	return err == nil && len(canonical) != 0 &&
		len(canonical) <= MaxEnvironmentCatalogEntries &&
		sameCapabilityIDs(canonical, plan.Capabilities) &&
		plan.Digest == provisionPlanDigest(plan)
}

func (plan ProvisionPlan) Covers(requirements []CapabilityID) bool {
	if !plan.Valid() {
		return false
	}
	for _, requirement := range requirements {
		index := sort.Search(len(plan.Capabilities), func(i int) bool { return plan.Capabilities[i] >= requirement })
		if index >= len(plan.Capabilities) || plan.Capabilities[index] != requirement {
			return false
		}
	}
	return true
}

func canonicalCapabilityIDs(ids []CapabilityID) ([]CapabilityID, error) {
	set, err := capabilitySetFromIDs(ids)
	if err != nil {
		return nil, err
	}
	return set.IDs(), nil
}

func sameCapabilityIDs(left, right []CapabilityID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func provisionPlanDigest(plan ProvisionPlan) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(provisionPlanDigestDomain))
	for _, value := range []string{
		plan.Provider,
		plan.Pool,
		plan.Profile,
		string(plan.OutputClass),
		plan.BaseEnvironmentDigest,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, capability := range plan.Capabilities {
		_, _ = hash.Write([]byte(capability))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

type ProvisioningState string

const (
	ProvisioningPlanned   ProvisioningState = "planned"
	ProvisioningQueued    ProvisioningState = "queued"
	ProvisioningSucceeded ProvisioningState = "succeeded"
	ProvisioningFailed    ProvisioningState = "failed"
	ProvisioningCancelled ProvisioningState = "cancelled"
)

type ProvisioningAttempt struct {
	ProvisionID   string            `json:"provision_id"`
	SourceDigest  string            `json:"source_digest"`
	Plan          ProvisionPlan     `json:"plan"`
	State         ProvisioningState `json:"state"`
	JobID         string            `json:"job_id,omitempty"`
	JobFence      uint64            `json:"job_fence,omitempty"`
	ReceiptDigest string            `json:"receipt_digest,omitempty"`
	Failure       FailureClass      `json:"failure_class,omitempty"`
}

func NewProvisioningAttempt(provisionID, sourceDigest string, plan ProvisionPlan) (ProvisioningAttempt, error) {
	provisionID = strings.TrimSpace(provisionID)
	if !identityPattern.MatchString(provisionID) || !digestPattern.MatchString(sourceDigest) || !plan.Valid() {
		return ProvisioningAttempt{}, errors.New("development provisioning attempt is invalid")
	}
	return ProvisioningAttempt{
		ProvisionID:  provisionID,
		SourceDigest: sourceDigest,
		Plan:         cloneProvisionPlan(plan),
		State:        ProvisioningPlanned,
	}, nil
}

func (attempt ProvisioningAttempt) Valid() bool {
	if !identityPattern.MatchString(attempt.ProvisionID) || !digestPattern.MatchString(attempt.SourceDigest) || !attempt.Plan.Valid() {
		return false
	}
	switch attempt.State {
	case ProvisioningPlanned:
		return attempt.JobID == "" && attempt.JobFence == 0 && attempt.ReceiptDigest == "" && attempt.Failure == ""
	case ProvisioningQueued:
		return provisionJobIDPattern.MatchString(attempt.JobID) && attempt.ReceiptDigest == "" && attempt.Failure == ""
	case ProvisioningSucceeded:
		return provisionJobIDPattern.MatchString(attempt.JobID) && attempt.JobFence > 0 && digestPattern.MatchString(attempt.ReceiptDigest) && attempt.Failure == ""
	case ProvisioningFailed:
		if !provisionJobIDPattern.MatchString(attempt.JobID) || attempt.ReceiptDigest != "" {
			return false
		}
		_, ok := ContinuationForFailure(attempt.Failure)
		return ok
	case ProvisioningCancelled:
		return attempt.ReceiptDigest == "" && attempt.Failure == "" &&
			(attempt.JobID == "" && attempt.JobFence == 0 || provisionJobIDPattern.MatchString(attempt.JobID))
	default:
		return false
	}
}

func (attempt ProvisioningAttempt) BindJob(jobID string) (ProvisioningAttempt, error) {
	jobID = strings.TrimSpace(jobID)
	if !attempt.Valid() || attempt.State != ProvisioningPlanned || !provisionJobIDPattern.MatchString(jobID) {
		return ProvisioningAttempt{}, errors.New("development provisioning job binding is invalid")
	}
	next := cloneProvisionAttempt(attempt)
	next.State = ProvisioningQueued
	next.JobID = jobID
	return next, nil
}

func (attempt ProvisioningAttempt) Succeed(receiptDigest string) (ProvisioningAttempt, error) {
	receiptDigest = strings.ToLower(strings.TrimSpace(receiptDigest))
	if !attempt.Valid() || attempt.State != ProvisioningQueued || attempt.JobFence == 0 || !digestPattern.MatchString(receiptDigest) {
		return ProvisioningAttempt{}, errors.New("development provisioning success is invalid")
	}
	next := cloneProvisionAttempt(attempt)
	next.State = ProvisioningSucceeded
	next.ReceiptDigest = receiptDigest
	return next, nil
}

func (attempt ProvisioningAttempt) Fail(class FailureClass) (ProvisioningAttempt, error) {
	if !attempt.Valid() || attempt.State != ProvisioningQueued {
		return ProvisioningAttempt{}, errors.New("development provisioning failure is invalid")
	}
	if _, ok := ContinuationForFailure(class); !ok {
		return ProvisioningAttempt{}, errors.New("development provisioning failure is invalid")
	}
	next := cloneProvisionAttempt(attempt)
	next.State = ProvisioningFailed
	next.Failure = class
	return next, nil
}

func (attempt ProvisioningAttempt) Cancel() (ProvisioningAttempt, error) {
	if !attempt.Valid() || attempt.State != ProvisioningPlanned && attempt.State != ProvisioningQueued {
		return ProvisioningAttempt{}, errors.New("development provisioning cancellation is invalid")
	}
	next := cloneProvisionAttempt(attempt)
	next.State = ProvisioningCancelled
	next.ReceiptDigest = ""
	next.Failure = ""
	return next, nil
}

func (attempt ProvisioningAttempt) BindFence(fence uint64) (ProvisioningAttempt, error) {
	if !attempt.Valid() || attempt.State != ProvisioningQueued || fence == 0 || fence < attempt.JobFence {
		return ProvisioningAttempt{}, errors.New("development provisioning fence binding is invalid")
	}
	next := cloneProvisionAttempt(attempt)
	next.JobFence = fence
	return next, nil
}

func cloneProvisionPlan(plan ProvisionPlan) ProvisionPlan {
	plan.Capabilities = append([]CapabilityID(nil), plan.Capabilities...)
	return plan
}

func cloneProvisionAttempt(attempt ProvisioningAttempt) ProvisioningAttempt {
	attempt.Plan = cloneProvisionPlan(attempt.Plan)
	return attempt
}

func sameProvisionAttempt(left, right ProvisioningAttempt) bool {
	return left.ProvisionID == right.ProvisionID && left.SourceDigest == right.SourceDigest &&
		left.Plan.Digest == right.Plan.Digest && left.State == right.State && left.JobID == right.JobID &&
		left.JobFence == right.JobFence && left.ReceiptDigest == right.ReceiptDigest && left.Failure == right.Failure
}

func validProvisionRevision(before, after ProvisioningAttempt) bool {
	if !before.Valid() || !after.Valid() || before.ProvisionID != after.ProvisionID ||
		before.SourceDigest != after.SourceDigest || before.Plan.Digest != after.Plan.Digest {
		return false
	}
	if sameProvisionAttempt(before, after) {
		return true
	}
	if before.State == ProvisioningPlanned {
		return after.State == ProvisioningQueued && after.JobFence == 0 ||
			after.State == ProvisioningCancelled && after.JobID == "" && after.JobFence == 0
	}
	if before.State != ProvisioningQueued || before.JobID != after.JobID || after.JobFence < before.JobFence {
		return false
	}
	if before.State == after.State {
		return after.JobFence > before.JobFence
	}
	// Persist the observed fence before recording a terminal receipt.
	return before.JobFence == after.JobFence &&
		(after.State == ProvisioningSucceeded || after.State == ProvisioningFailed || after.State == ProvisioningCancelled)
}
