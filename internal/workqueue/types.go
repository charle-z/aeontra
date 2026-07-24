package workqueue

import (
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

type State string
type Reason string

const (
	StateBlocked   State = "blocked"
	StateQueued    State = "queued"
	StateLeased    State = "leased"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

const (
	ReasonNone              Reason = ""
	ReasonDependencyPending Reason = "dependency_pending"
	ReasonDependencyFailed  Reason = "dependency_failed"
	ReasonLeaseExpired      Reason = "lease_expired"
	ReasonCancelled         Reason = "cancelled"
	ReasonCancelRequested   Reason = "cancel_requested"
)

var ErrNoJobAvailable = errors.New("workqueue: no job available")

const (
	DefaultMaxJobs                   = 1024
	DefaultMaxJobsPerWorkspace       = 64
	MaxDependencies                  = 16
	MaxListResults                   = 100
	MinLeaseTTL                      = 15 * time.Second
	MaxLeaseTTL                      = 10 * time.Minute
	TargetMaxBytes             int64 = 64 << 20
)

type Config struct {
	Root                string
	ControllerID        string
	Writers             int
	MaxJobs             int
	MaxJobsPerWorkspace int
}

type Spec struct {
	IdempotencyKey string
	Workspace      string
	Pool           string
	Profile        string
	PayloadHash    string
	Dependencies   []string
}

type Job struct {
	ID              string
	IdempotencyKey  string
	Workspace       string
	Pool            string
	Profile         string
	PayloadHash     string
	State           State
	Reason          Reason
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CancelRequested bool
	Attempt         int
	Fence           uint64
	LeaseID         string
	LeaseHolder     string
	LeaseExpiresAt  time.Time
	Outcome         State
	Summary         string
	ResultRef       string
}

type Lease struct {
	Job       Job
	ID        string
	Fence     uint64
	Attempt   int
	ExpiresAt time.Time
}

type HeartbeatStatus struct {
	ExpiresAt       time.Time
	CancelRequested bool
}

type Result struct {
	Outcome   State
	Summary   string
	ResultRef string
}

var (
	controllerPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	workspacePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	poolPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	profilePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	jobIDPattern       = regexp.MustCompile(`^wj_[a-f0-9]{32}$`)
	leaseIDPattern     = regexp.MustCompile(`^wl_[a-f0-9]{32}$`)
	resultRefPattern   = regexp.MustCompile(`^rs_[a-f0-9]{32,64}$`)
	payloadHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	holderPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`)
)

func terminal(state State) bool {
	return state == StateSucceeded || state == StateFailed || state == StateCancelled
}

func legalTransition(from, to State) bool {
	switch from {
	case StateBlocked:
		return to == StateQueued || to == StateFailed || to == StateCancelled
	case StateQueued:
		return to == StateLeased || to == StateCancelled
	case StateLeased:
		return to == StateQueued || to == StateSucceeded || to == StateFailed || to == StateCancelled
	default:
		return false
	}
}

func validateConfig(config Config) (Config, error) {
	if config.Writers == 0 {
		config.Writers = 1
	}
	if config.MaxJobs == 0 {
		config.MaxJobs = DefaultMaxJobs
	}
	if config.MaxJobsPerWorkspace == 0 {
		config.MaxJobsPerWorkspace = DefaultMaxJobsPerWorkspace
	}
	if !controllerPattern.MatchString(config.ControllerID) || config.Writers != 1 || config.MaxJobs < 1 || config.MaxJobs > 4096 || config.MaxJobsPerWorkspace < 1 || config.MaxJobsPerWorkspace > config.MaxJobs {
		return Config{}, errors.New("workqueue: configuration is invalid")
	}
	return config, nil
}

func validateSpec(spec Spec) error {
	if !idempotencyPattern.MatchString(spec.IdempotencyKey) || !workspacePattern.MatchString(spec.Workspace) || !poolPattern.MatchString(spec.Pool) || !profilePattern.MatchString(spec.Profile) || !payloadHashPattern.MatchString(spec.PayloadHash) || len(spec.Dependencies) > MaxDependencies {
		return errors.New("workqueue: job specification is invalid")
	}
	seen := make(map[string]struct{}, len(spec.Dependencies))
	for _, dependency := range spec.Dependencies {
		if !jobIDPattern.MatchString(dependency) {
			return errors.New("workqueue: dependency is invalid")
		}
		if _, exists := seen[dependency]; exists {
			return errors.New("workqueue: duplicate dependency")
		}
		seen[dependency] = struct{}{}
	}
	return nil
}

func canonicalSpec(spec Spec) Spec {
	if len(spec.Dependencies) == 0 {
		return spec
	}
	spec.Dependencies = append([]string(nil), spec.Dependencies...)
	sort.Strings(spec.Dependencies)
	return spec
}

func validateResult(result Result) error {
	if result.Outcome != StateSucceeded && result.Outcome != StateFailed && result.Outcome != StateCancelled {
		return errors.New("workqueue: result outcome is invalid")
	}
	if len(result.Summary) < 1 || len(result.Summary) > 2048 || (result.ResultRef != "" && !resultRefPattern.MatchString(result.ResultRef)) {
		return errors.New("workqueue: result is invalid")
	}
	if redacted, changed := policy.Redact(result.Summary); changed || redacted != result.Summary {
		return errors.New("workqueue: result contains secret material")
	}
	return nil
}
