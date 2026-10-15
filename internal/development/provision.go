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
	ProvisionPlanVersion             = 1
	MaxProvisioningAttemptsPerStep   = 16
	provisionPlanDigestDomain        = "aeontra-development-provision-plan-v1\x00"
)

var (
	provisionIdentityPattern = regexp.MustCompile("^[a-z][a-z0-9._-]{0,63}$")
	provisionJobIDPattern    = regexp.MustCompile("^wj_[a-f0-9]{32}$")
	digestPattern            = regexp.MustCompile("^sha256:[a-f0-9]{64}$")
)

type ProvisionPlan struct {
	Version               int
	Provider              string
	Pool                  string
	Profile               string
	OutputClass           ExecutionClass
	BaseEnvironmentDigest string
	Capabilities          []CapabilityID
	Digest                string
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
	ProvisioningPlanned ProvisioningState = "planned"
	ProvisioningQueued  ProvisioningState = "queued"
	ProvisioningSucceeded ProvisioningState = "succeeded"
	ProvisioningFailed    ProvisioningState = "failed"
	ProvisioningCancelled ProvisioningState = "cancelled"
)

type ProvisioningAttempt struct {
	ProvisionID       string
	Plan              ProvisionPlan
	State             ProvisioningState
	JobID             string
	ReceiptDigest     string
	Failure           FailureClass
}

func NewProvisioningAttempt(provisionID string, plan ProvisionPlan) (ProvisioningAttempt, error) {
	provisionID = strings.TrimSpace(provisionID)
	if !identityPattern.MatchString(provisionID) || !plan.Valid() {
		return ProvisioningAttempt{}, errors.New("development provisioning attempt is invalid")
	}
	return ProvisioningAttempt{
		ProvisionID: provisionID,
		Plan:        plan,
		State:       ProvisioningPlanned,
	}, nil
}

func (attempt ProvisioningAttempt) Valid() bool {
	if !identityPattern.MatchString(attempt.ProvisionID) || !attempt.Plan.Valid() {
		return false
	}
	switch attempt.State {
	case ProvisioningPlanned:
		return attempt.JobID == "" && attempt.ReceiptDigest == "" && attempt.Failure == ""
	case ProvisioningQueued:
		return provisionJobIDPattern.MatchString(attempt.JobID) && attempt.ReceiptDigest == "" && attempt.Failure == ""
	case ProvisioningSucceeded:
		return provisionJobIDPattern.MatchString(attempt.JobID) && digestPattern.MatchString(attempt.ReceiptDigest) && attempt.Failure == ""
	case ProvisioningFailed:
		if !provisionJobIDPattern.MatchString(attempt.JobID) || attempt.ReceiptDigest != "" {
			return false
		}
		_, ok := ContinuationForFailure(attempt.Failure)
		return ok
	case ProvisioningCancelled:
		return attempt.ReceiptDigest == "" && attempt.Failure == "" &&
			(attempt.JobID == "" || provisionJobIDPattern.MatchString(attempt.JobID))
	default:
		return false
	}
}

func (attempt ProvisioningAttempt) BindJob(jobID string) (ProvisioningAttempt, error) {
	jobID = strings.TrimSpace(jobID)
	if attempt.State != ProvisioningPlanned || !provisionJobIDPattern.MatchString(jobID) {
		return ProvisioningAttempt{}, errors.New("development provisioning job binding is invalid")
	}
	next := attempt
	next.State = ProvisioningQueued
	next.JobID = jobID
	return next, nil
}

func (attempt ProvisioningAttempt) Succeed(receiptDigest string) (ProvisioningAttempt, error) {
	receiptDigest = strings.ToLower(strings.TrimSpace(receiptDigest))
	if attempt.State != ProvisioningQueued || !digestPattern.MatchString(receiptDigest) {
		return ProvisioningAttempt{}, errors.New("development provisioning success is invalid")
	}
	next := attempt
	next.State = ProvisioningSucceeded
	next.ReceiptDigest = receiptDigest
	return next, nil
}

func (attempt ProvisioningAttempt) Fail(class FailureClass) (ProvisioningAttempt, error) {
	if attempt.State != ProvisioningQueued {
		return ProvisioningAttempt{}, errors.New("development provisioning failure is invalid")
	}
	if _, ok := ContinuationForFailure(class); !ok {
		return ProvisioningAttempt{}, errors.New("development provisioning failure is invalid")
	}
	next := attempt
	next.State = ProvisioningFailed
	next.Failure = class
	return next, nil
}


func (attempt ProvisioningAttempt) Cancel() (ProvisioningAttempt, error) {
	if attempt.State != ProvisioningPlanned && attempt.State != ProvisioningQueued {
		return ProvisioningAttempt{}, errors.New("development provisioning cancellation is invalid")
	}
	next := attempt
	next.State = ProvisioningCancelled
	next.ReceiptDigest = ""
	next.Failure = ""
	return next, nil
}
