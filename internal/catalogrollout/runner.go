package catalogrollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/catalogidentity"
)

var ErrInterrupted = errors.New("catalog rollout interrupted")
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var requestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

const JournalFilename = "backend-catalog-rollout.json"

type Identity struct {
	Commit          string `json:"commit"`
	ProtocolVersion string `json:"protocol_version"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

func (i Identity) Validate() error {
	if !commitPattern.MatchString(i.Commit) {
		return errors.New("rollout commit is invalid")
	}
	return (catalogidentity.Identity{ProtocolVersion: i.ProtocolVersion, ToolCount: i.ToolCount, CatalogHash: i.CatalogHash}).Validate()
}

type Request struct {
	RequestID string   `json:"request_id"`
	Previous  Identity `json:"previous"`
	Candidate Identity `json:"candidate"`
}

func (r Request) Validate() error {
	if !requestPattern.MatchString(r.RequestID) {
		return errors.New("rollout request id is invalid")
	}
	if err := r.Previous.Validate(); err != nil {
		return err
	}
	if err := r.Candidate.Validate(); err != nil {
		return err
	}
	if r.Previous.ProtocolVersion != r.Candidate.ProtocolVersion {
		return errors.New("candidate protocol does not match current protocol")
	}
	return nil
}

type FrontContract struct {
	Primary    string `json:"primary"`
	Transition string `json:"transition,omitempty"`
}

type Observation struct {
	Backend Identity      `json:"backend"`
	Front   FrontContract `json:"front"`
}

type State string

const (
	StateIdle         State = "idle"
	StateQueued       State = "queued"
	StateRunning      State = "running"
	StateCompensating State = "compensating"
	StateSucceeded    State = "succeeded"
	StateFailed       State = "failed"
)

type Phase string

const (
	PhaseNone            Phase = ""
	PhasePrepareFront    Phase = "prepare-front"
	PhaseDeployBackend   Phase = "deploy-backend"
	PhaseVerifyBackend   Phase = "verify-backend"
	PhaseFinalizeFront   Phase = "finalize-front"
	PhaseRollbackBackend Phase = "rollback-backend"
	PhaseRollbackFront   Phase = "rollback-front"
	PhaseComplete        Phase = "complete"
)

type Status struct {
	SchemaVersion int       `json:"schema_version"`
	Revision      uint64    `json:"revision"`
	Request       Request   `json:"request"`
	State         State     `json:"state"`
	Phase         Phase     `json:"phase,omitempty"`
	DeploymentID  string    `json:"deployment_id,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Platform interface {
	Observe(context.Context) (Observation, error)
	PrepareFront(context.Context, Identity, Identity) (string, error)
	DeployBackend(context.Context, Identity) (string, error)
	VerifyBackend(context.Context, Identity) error
	FinalizeFront(context.Context, Identity) (string, error)
	RollbackBackend(context.Context, Identity) (string, error)
	RollbackFront(context.Context, Identity) (string, error)
	PublishStatus(context.Context, Status) error
}

type Journal struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

func OpenJournal(root string) (*Journal, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("rollout state root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	journal := &Journal{path: filepath.Join(root, JournalFilename), now: time.Now}
	if _, err := os.Stat(journal.path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(journal.path, []byte(`{"schema_version":1,"state":"idle"}`+"\n"), 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if _, err := journal.Read(); err != nil {
		return nil, err
	}
	return journal, nil
}

func (j *Journal) Read() (Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.read()
}

func (j *Journal) read() (Status, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	if status.SchemaVersion != 1 {
		return Status{}, errors.New("unsupported rollout journal schema")
	}
	if status.State != StateIdle {
		if err := status.Request.Validate(); err != nil {
			return Status{}, err
		}
	}
	return status, nil
}

func (j *Journal) Advance(status Status) (Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	current, err := j.read()
	if err != nil {
		return Status{}, err
	}
	status.SchemaVersion = 1
	status.Revision = current.Revision + 1
	status.UpdatedAt = j.now().UTC()
	data, err := json.Marshal(status)
	if err != nil {
		return Status{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(j.path), ".catalog-rollout-*.tmp")
	if err != nil {
		return Status{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(append(data, '\n'))
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, j.path)
	}
	return status, err
}

type Runner struct {
	Platform Platform
	Journal  *Journal
}

func (r Runner) persist(ctx context.Context, status Status) (Status, error) {
	next, err := r.Journal.Advance(status)
	if err != nil {
		return Status{}, err
	}
	if err := r.Platform.PublishStatus(ctx, next); err != nil {
		return next, err
	}
	return next, nil
}

func same(a, b Identity) bool { return a == b }
func frontOld(f FrontContract, q Request) bool {
	return f.Primary == q.Previous.CatalogHash && f.Transition == ""
}
func frontBoth(f FrontContract, q Request) bool {
	return f.Primary == q.Candidate.CatalogHash && f.Transition == q.Previous.CatalogHash
}
func frontNew(f FrontContract, q Request) bool {
	return f.Primary == q.Candidate.CatalogHash && f.Transition == ""
}

func validateObservation(o Observation, q Request) error {
	if err := o.Backend.Validate(); err != nil {
		return err
	}
	for _, hash := range []string{o.Front.Primary, o.Front.Transition} {
		if hash != "" && hash != q.Previous.CatalogHash && hash != q.Candidate.CatalogHash {
			return errors.New("front door contains a third catalog")
		}
	}
	if o.Front.Primary == "" || o.Front.Primary == o.Front.Transition {
		return errors.New("front door catalog contract is invalid")
	}
	return nil
}

func interrupted(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (r Runner) Run(ctx context.Context, request Request) (Status, error) {
	if r.Platform == nil || r.Journal == nil {
		return Status{}, errors.New("catalog rollout runner is not configured")
	}
	if err := request.Validate(); err != nil {
		return Status{}, err
	}
	status, err := r.Journal.Read()
	if err != nil {
		return Status{}, err
	}
	active := status.State == StateQueued || status.State == StateRunning || status.State == StateCompensating
	if active && status.Request.RequestID != request.RequestID {
		return status, errors.New("a different rollout is active")
	}
	if status.Request.RequestID == request.RequestID && (status.State == StateSucceeded || status.State == StateFailed) {
		if err := r.Platform.PublishStatus(ctx, status); err != nil {
			return status, err
		}
		if status.State == StateFailed {
			return status, errors.New(status.Reason)
		}
		return status, nil
	}
	if status.Request.RequestID != request.RequestID {
		status, err = r.persist(ctx, Status{Request: request, State: StateQueued})
		if err != nil {
			return status, err
		}
	}
	for attempts := 0; attempts < 24; attempts++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return status, errors.Join(ErrInterrupted, ctxErr)
		}
		observation, observeErr := r.Platform.Observe(ctx)
		if observeErr != nil {
			if interrupted(ctx, observeErr) {
				return status, errors.Join(ErrInterrupted, observeErr)
			}
			return r.compensate(ctx, status, request, "observe_failed", observeErr)
		}
		if err := validateObservation(observation, request); err != nil {
			return r.compensate(ctx, status, request, "contract_invalid", err)
		}
		if same(observation.Backend, request.Candidate) {
			if status.Phase != PhaseFinalizeFront && status.Phase != PhaseComplete {
				status, _ = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseVerifyBackend})
				if err := r.Platform.VerifyBackend(ctx, request.Candidate); err != nil {
					if interrupted(ctx, err) {
						return status, errors.Join(ErrInterrupted, err)
					}
					return r.compensate(ctx, status, request, "candidate_verification_failed", err)
				}
				status, err = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseFinalizeFront})
				if err != nil {
					return status, err
				}
				continue
			}
			if request.Candidate.CatalogHash == request.Previous.CatalogHash || frontNew(observation.Front, request) {
				return r.persist(ctx, Status{Request: request, State: StateSucceeded, Phase: PhaseComplete})
			}
			if !frontBoth(observation.Front, request) {
				return r.compensate(ctx, status, request, "front_contract_lost", errors.New("candidate backend is not admitted by front door"))
			}
			status, _ = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseFinalizeFront})
			deploymentID, actionErr := r.Platform.FinalizeFront(ctx, request.Candidate)
			if actionErr != nil {
				if interrupted(ctx, actionErr) {
					return status, errors.Join(ErrInterrupted, actionErr)
				}
				return r.compensate(ctx, status, request, "front_finalize_failed", actionErr)
			}
			status, err = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseComplete, DeploymentID: deploymentID})
			if err != nil {
				return status, err
			}
			continue
		}
		if !same(observation.Backend, request.Previous) {
			return r.compensate(ctx, status, request, "backend_identity_unknown", errors.New("backend is neither previous nor candidate identity"))
		}
		if request.Candidate.CatalogHash != request.Previous.CatalogHash && !frontBoth(observation.Front, request) {
			if !frontOld(observation.Front, request) {
				return r.compensate(ctx, status, request, "front_contract_invalid", errors.New("front door is not on the previous compatible catalog"))
			}
			status, _ = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhasePrepareFront})
			deploymentID, actionErr := r.Platform.PrepareFront(ctx, request.Previous, request.Candidate)
			if actionErr != nil {
				if interrupted(ctx, actionErr) {
					return status, errors.Join(ErrInterrupted, actionErr)
				}
				return r.compensate(ctx, status, request, "front_prepare_failed", actionErr)
			}
			status, err = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseDeployBackend, DeploymentID: deploymentID})
			if err != nil {
				return status, err
			}
			continue
		}
		status, _ = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseDeployBackend})
		deploymentID, actionErr := r.Platform.DeployBackend(ctx, request.Candidate)
		if actionErr != nil {
			if interrupted(ctx, actionErr) {
				return status, errors.Join(ErrInterrupted, actionErr)
			}
			status, err = r.persist(ctx, Status{
				Request: request, State: StateRunning, Phase: PhaseDeployBackend,
				DeploymentID: deploymentID, Reason: "backend_deploy_failed",
			})
			if err != nil {
				return status, errors.Join(actionErr, err)
			}
			return r.compensate(ctx, status, request, "backend_deploy_failed", actionErr)
		}
		status, err = r.persist(ctx, Status{Request: request, State: StateRunning, Phase: PhaseVerifyBackend, DeploymentID: deploymentID})
		if err != nil {
			return status, err
		}
	}
	return r.compensate(ctx, status, request, "phase_budget_exhausted", errors.New("catalog rollout exceeded phase budget"))
}

func (r Runner) compensate(ctx context.Context, status Status, request Request, reason string, cause error) (Status, error) {
	failureDeploymentID := status.DeploymentID
	status, _ = r.persist(ctx, Status{
		Request: request, State: StateCompensating, Phase: status.Phase,
		DeploymentID: failureDeploymentID, Reason: reason,
	})

	var observation Observation
	rollbackBackend := reason == "backend_deploy_failed"
	if !rollbackBackend {
		var err error
		observation, err = r.Platform.Observe(ctx)
		if err != nil {
			return r.fail(ctx, status, reason+": rollback observation failed", errors.Join(cause, err))
		}
		rollbackBackend = same(observation.Backend, request.Candidate)
	}
	if rollbackBackend {
		status, _ = r.persist(ctx, Status{
			Request: request, State: StateCompensating, Phase: PhaseRollbackBackend,
			DeploymentID: failureDeploymentID, Reason: reason,
		})
		rollbackID, rollbackErr := r.Platform.RollbackBackend(ctx, request.Previous)
		if rollbackErr != nil {
			if rollbackID != "" {
				status.DeploymentID = rollbackID
			}
			return r.fail(ctx, status, reason+": backend rollback failed", errors.Join(cause, rollbackErr))
		}
		status, _ = r.persist(ctx, Status{
			Request: request, State: StateCompensating, Phase: PhaseRollbackBackend,
			DeploymentID: rollbackID, Reason: reason,
		})
		var observeErr error
		observation, observeErr = r.Platform.Observe(ctx)
		if observeErr != nil {
			return r.fail(ctx, status, reason+": rollback observation failed", errors.Join(cause, observeErr))
		}
	}
	if !same(observation.Backend, request.Previous) {
		return r.fail(ctx, status, reason+": backend rollback verification failed", errors.Join(cause, errors.New("previous backend identity was not restored")))
	}
	if !frontOld(observation.Front, request) {
		status, _ = r.persist(ctx, Status{
			Request: request, State: StateCompensating, Phase: PhaseRollbackFront,
			DeploymentID: status.DeploymentID, Reason: reason,
		})
		rollbackID, rollbackErr := r.Platform.RollbackFront(ctx, request.Previous)
		if rollbackErr != nil {
			if rollbackID != "" {
				status.DeploymentID = rollbackID
			}
			return r.fail(ctx, status, reason+": front rollback failed", errors.Join(cause, rollbackErr))
		}
		status, _ = r.persist(ctx, Status{
			Request: request, State: StateCompensating, Phase: PhaseRollbackFront,
			DeploymentID: rollbackID, Reason: reason,
		})
		finalObservation, observeErr := r.Platform.Observe(ctx)
		if observeErr != nil {
			return r.fail(ctx, status, reason+": rollback observation failed", errors.Join(cause, observeErr))
		}
		if !same(finalObservation.Backend, request.Previous) || !frontOld(finalObservation.Front, request) {
			return r.fail(ctx, status, reason+": rollback verification failed", errors.Join(cause, errors.New("previous backend and front-door contract were not restored")))
		}
	}
	if failureDeploymentID != "" {
		status.DeploymentID = failureDeploymentID
	}
	return r.fail(ctx, status, reason, cause)
}

func (r Runner) fail(ctx context.Context, status Status, reason string, cause error) (Status, error) {
	status.State = StateFailed
	status.Phase = PhaseComplete
	status.Reason = reason
	status, persistErr := r.persist(ctx, status)
	if persistErr != nil {
		return status, errors.Join(cause, persistErr)
	}
	return status, fmt.Errorf("%s: %w", reason, cause)
}
