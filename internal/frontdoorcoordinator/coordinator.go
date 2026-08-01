package frontdoorcoordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

const (
	FrontPublicOrigin    = "https://mcp-devbox-charlez.duckdns.org"
	FrontTemporaryOrigin = "https://front.mcp-devbox-charlez.duckdns.org"
	BackendOrigin        = "https://backend.mcp-devbox-charlez.duckdns.org"
	JournalFilename      = "front-door-transition.json"
	ManagedRepository    = "charle-z/mcp-devbox"
)

type Target string

const (
	TargetIdle     Target = "idle"
	TargetCutover  Target = "cutover"
	TargetRollback Target = "rollback"
)

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
	PhaseNone                     Phase = ""
	PhaseAddBackendOrigin         Phase = "add-backend-origin"
	PhaseSwitchFrontBackend       Phase = "switch-front-backend"
	PhaseReleasePublicBackend     Phase = "release-public-backend"
	PhaseAssignPublicFront        Phase = "assign-public-front"
	PhaseMoveFrontTemporary       Phase = "move-front-temporary"
	PhaseRestorePublicBackend     Phase = "restore-public-backend"
	PhaseSwitchFrontPublicBackend Phase = "switch-front-public-backend"
	PhaseRemoveAlternateBackend   Phase = "remove-alternate-backend"
	PhaseComplete                 Phase = "complete"
)

type Topology struct {
	FrontDomain     string `json:"front_domain"`
	FrontBackendURL string `json:"front_backend_url"`
	BackendDomains  string `json:"backend_domains"`
}

type Status struct {
	SchemaVersion  int       `json:"schema_version"`
	Revision       uint64    `json:"revision"`
	RequestID      string    `json:"request_id,omitempty"`
	Target         Target    `json:"target"`
	RecoveryTarget Target    `json:"recovery_target,omitempty"`
	State          State     `json:"state"`
	Phase          Phase     `json:"phase,omitempty"`
	Topology       Topology  `json:"topology"`
	DeploymentID   string    `json:"deployment_id,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func ValidateRequestID(raw string) error {
	if !requestIDPattern.MatchString(strings.TrimSpace(raw)) {
		return errors.New("front-door coordinator request id is invalid")
	}
	return nil
}

func validateStatus(status Status) error {
	if status.SchemaVersion != 1 {
		return errors.New("unsupported front-door coordinator journal schema")
	}
	if status.State == StateIdle {
		if status.Revision != 0 || status.RequestID != "" || status.Target != TargetIdle || status.RecoveryTarget != "" || status.Phase != PhaseNone || status.Reason != "" {
			return errors.New("idle front-door coordinator status is invalid")
		}
		return nil
	}
	if status.Revision == 0 {
		return errors.New("front-door coordinator revision is invalid")
	}
	if err := ValidateRequestID(status.RequestID); err != nil {
		return err
	}
	if status.Target != TargetCutover && status.Target != TargetRollback {
		return errors.New("front-door coordinator target is invalid")
	}
	expectedRecovery, err := oppositeTarget(status.Target)
	if err != nil {
		return err
	}
	switch status.State {
	case StateQueued:
		if status.RecoveryTarget != "" || status.Phase != PhaseNone || status.Reason != "" {
			return errors.New("queued front-door coordinator status is invalid")
		}
	case StateRunning:
		if status.RecoveryTarget != "" || status.Phase == PhaseNone || status.Reason != "" {
			return errors.New("running front-door coordinator status is invalid")
		}
	case StateCompensating:
		if status.RecoveryTarget != expectedRecovery || status.Phase == PhaseNone || status.Reason == "" {
			return errors.New("compensating front-door coordinator status is invalid")
		}
	case StateSucceeded:
		if status.RecoveryTarget != "" || status.Phase != PhaseComplete || status.Reason != "" {
			return errors.New("succeeded front-door coordinator status is invalid")
		}
	case StateFailed:
		if status.Reason == "" {
			return errors.New("failed front-door coordinator status is invalid")
		}
		if status.RecoveryTarget != "" && status.Phase == PhaseNone {
			return errors.New("compensated front-door coordinator status requires a phase")
		}
		if status.RecoveryTarget != "" && status.RecoveryTarget != expectedRecovery {
			return errors.New("failed front-door coordinator recovery target is invalid")
		}
	default:
		return errors.New("front-door coordinator state is invalid")
	}
	return nil
}

func ParseTarget(raw string) (Target, error) {
	switch Target(strings.TrimSpace(raw)) {
	case TargetIdle:
		return TargetIdle, nil
	case TargetCutover:
		return TargetCutover, nil
	case TargetRollback:
		return TargetRollback, nil
	default:
		return "", errors.New("front-door coordinator target must be idle, cutover or rollback")
	}
}

func NextPhase(target Target, topology Topology) (Phase, bool, error) {
	front := strings.TrimSpace(topology.FrontDomain)
	frontBackend := strings.TrimSpace(topology.FrontBackendURL)
	backend := normalizeDomains(topology.BackendDomains)
	if backend == "" || (frontBackend != FrontPublicOrigin && frontBackend != BackendOrigin) {
		return PhaseNone, false, errors.New("managed front-door topology is invalid")
	}
	both := normalizeDomains(FrontPublicOrigin + "," + BackendOrigin)
	switch target {
	case TargetCutover:
		switch {
		case front == FrontPublicOrigin && frontBackend == BackendOrigin && backend == BackendOrigin:
			return PhaseComplete, true, nil
		case front == FrontTemporaryOrigin && frontBackend == FrontPublicOrigin && backend == FrontPublicOrigin:
			return PhaseAddBackendOrigin, false, nil
		case front == FrontTemporaryOrigin && frontBackend == FrontPublicOrigin && backend == both:
			return PhaseSwitchFrontBackend, false, nil
		case front == FrontTemporaryOrigin && frontBackend == BackendOrigin && backend == both:
			return PhaseReleasePublicBackend, false, nil
		case front == FrontTemporaryOrigin && frontBackend == BackendOrigin && backend == BackendOrigin:
			return PhaseAssignPublicFront, false, nil
		}
	case TargetRollback:
		switch {
		case front == FrontTemporaryOrigin && frontBackend == FrontPublicOrigin && backend == FrontPublicOrigin:
			return PhaseComplete, true, nil
		case front == FrontPublicOrigin && frontBackend == BackendOrigin && backend == BackendOrigin:
			return PhaseMoveFrontTemporary, false, nil
		case front == FrontTemporaryOrigin && frontBackend == BackendOrigin && backend == BackendOrigin:
			return PhaseRestorePublicBackend, false, nil
		case front == FrontTemporaryOrigin && frontBackend == BackendOrigin && backend == both:
			return PhaseSwitchFrontPublicBackend, false, nil
		case front == FrontTemporaryOrigin && frontBackend == FrontPublicOrigin && backend == both:
			return PhaseRemoveAlternateBackend, false, nil
		}
	default:
		return PhaseNone, false, errors.New("idle target has no transition phase")
	}
	return PhaseNone, false, fmt.Errorf("managed topology is not valid for target %s", target)
}

// ValidateTopology verifies that the current managed topology belongs to the
// closed cutover/rollback state machine without performing any mutation.
func ValidateTopology(target Target, topology Topology) error {
	switch target {
	case TargetCutover, TargetRollback:
		_, _, err := NextPhase(target, topology)
		return err
	case TargetIdle:
		if _, _, err := NextPhase(TargetCutover, topology); err == nil {
			return nil
		}
		if _, _, err := NextPhase(TargetRollback, topology); err == nil {
			return nil
		}
		return errors.New("managed front-door topology is invalid")
	default:
		return errors.New("front-door coordinator target is invalid")
	}
}

func normalizeDomains(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := map[string]bool{}
	ordered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			return ""
		}
		seen[part] = true
		ordered = append(ordered, part)
	}
	if len(ordered) == 2 && ordered[0] > ordered[1] {
		ordered[0], ordered[1] = ordered[1], ordered[0]
	}
	return strings.Join(ordered, ",")
}

type Journal struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

func OpenJournal(root string) (*Journal, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("front-door coordinator state root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	return &Journal{path: filepath.Join(root, JournalFilename), now: time.Now}, nil
}

func (j *Journal) Read() (Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.readLocked()
}

func (j *Journal) readLocked() (Status, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{SchemaVersion: 1, Target: TargetIdle, State: StateIdle}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	if err := validateStatus(status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (j *Journal) Advance(next Status) (Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	current, err := j.readLocked()
	if err != nil {
		return Status{}, err
	}
	next.SchemaVersion = 1
	next.Revision = current.Revision + 1
	next.UpdatedAt = j.now().UTC()
	if err := validateStatus(next); err != nil {
		return Status{}, err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return Status{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".front-door-transition-*.tmp")
	if err != nil {
		return Status{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return Status{}, err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return Status{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Status{}, err
	}
	if err := tmp.Close(); err != nil {
		return Status{}, err
	}
	if err := os.Rename(tmpName, j.path); err != nil {
		return Status{}, err
	}
	directory, err := os.Open(filepath.Dir(j.path))
	if err != nil {
		return Status{}, err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return Status{}, err
	}
	if err := directory.Close(); err != nil {
		return Status{}, err
	}
	return next, nil
}
