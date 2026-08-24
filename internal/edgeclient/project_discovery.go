package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultProjectDiscoveryEntries = 128
	maxProjectDiscoveryEntries     = 512
	defaultProjectDiscoveryTimeout = 30 * time.Second
	maxProjectDiscoveryTimeout     = 2 * time.Minute
)

type ProjectRecoveryState string

const (
	ProjectRecoveryReuseExisting     ProjectRecoveryState = "reuse_existing"
	ProjectRecoveryAssociateExisting ProjectRecoveryState = "associate_existing"
	ProjectRecoveryCloneRequired     ProjectRecoveryState = "clone_required"
	ProjectRecoveryBlocked           ProjectRecoveryState = "blocked"
)

type ProjectDiscoveryConfig struct {
	Roots      WorkspaceRoots
	Inspector  ProjectCheckoutInspector
	MaxEntries int
	Timeout    time.Duration
}

type ProjectDiscoveryRequest struct {
	Alias      string
	Owner      string
	Repository string
}

type ProjectRecoveryDecision struct {
	Alias          string
	Owner          string
	Repository     string
	CanonicalPath  string
	CandidatePath  string
	CandidateCount int
	State          ProjectRecoveryState
	Reason         ProjectErrorCode
}

type ProjectRecoveryStatus struct {
	Alias          string               `json:"alias"`
	Repository     string               `json:"repository"`
	State          ProjectRecoveryState `json:"state"`
	CandidateCount int                  `json:"candidate_count"`
	Reason         ProjectErrorCode     `json:"reason,omitempty"`
}

type projectDiscoveryCandidate struct {
	path  string
	state ProjectCheckoutState
}

func DiscoverProjectCheckout(ctx context.Context, config ProjectDiscoveryConfig, request ProjectDiscoveryRequest) (ProjectRecoveryDecision, error) {
	alias, err := NormalizeProjectAlias(request.Alias)
	if err != nil {
		return ProjectRecoveryDecision{}, err
	}
	owner, repository, err := NormalizeProjectRepository(request.Owner, request.Repository)
	if err != nil {
		return ProjectRecoveryDecision{}, err
	}
	roots, err := normalizeWorkspaceRoots(config.Roots)
	if err != nil {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorCheckoutUnsafe, err)
	}
	root, err := projectDevelopmentRoot(roots)
	if err != nil {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorCheckoutUnsafe, err)
	}
	inspector := config.Inspector
	if inspector == nil {
		inspector = newProjectCheckoutInspector()
	}
	maxEntries := config.MaxEntries
	if maxEntries == 0 {
		maxEntries = defaultProjectDiscoveryEntries
	}
	if maxEntries < 1 || maxEntries > maxProjectDiscoveryEntries {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorInvalidInput, errors.New("project discovery entry limit is invalid"))
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultProjectDiscoveryTimeout
	}
	if timeout < time.Second || timeout > maxProjectDiscoveryTimeout {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorInvalidInput, errors.New("project discovery timeout is invalid"))
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	canonical, err := CanonicalProjectPath(roots, repository)
	if err != nil {
		return ProjectRecoveryDecision{}, err
	}
	decision := ProjectRecoveryDecision{
		Alias: alias, Owner: owner, Repository: repository, CanonicalPath: canonical,
	}
	if exists, unsafe, err := projectDiscoveryPathState(canonical); err != nil {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorCheckoutUnsafe, err)
	} else if exists {
		decision.CandidateCount = 1
		if unsafe {
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorCheckoutUnsafe
			return decision, nil
		}
		state, inspectErr := inspector.Inspect(discoveryCtx, canonical, owner, repository)
		if inspectErr != nil {
			if discoveryCtx.Err() != nil {
				return ProjectRecoveryDecision{}, projectErr(ProjectErrorDiscoveryTimeout, errors.New("project discovery timed out"))
			}
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorCheckoutUnsafe
			return decision, nil
		}
		switch state {
		case ProjectCheckoutReady:
			decision.State = ProjectRecoveryReuseExisting
			decision.CandidatePath = canonical
		case ProjectCheckoutDirty:
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorCheckoutDirty
		case ProjectCheckoutRemoteMismatch:
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorRepositoryMismatch
		default:
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorCheckoutUnsafe
		}
		return decision, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorCheckoutUnsafe, errors.New("project development root is unavailable"))
	}
	if len(entries) > maxEntries {
		return ProjectRecoveryDecision{}, projectErr(ProjectErrorDiscoveryLimit, errors.New("project discovery entry limit exceeded"))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	candidates := make([]projectDiscoveryCandidate, 0, 2)
	for _, entry := range entries {
		if entry.Name() == repository || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if filepath.Dir(path) != filepath.Clean(root) || !pathInside(root, path) {
			continue
		}
		if discoveryCtx.Err() != nil {
			return ProjectRecoveryDecision{}, projectErr(ProjectErrorDiscoveryTimeout, errors.New("project discovery timed out"))
		}
		state, inspectErr := inspector.Inspect(discoveryCtx, path, owner, repository)
		if inspectErr != nil {
			if discoveryCtx.Err() != nil {
				return ProjectRecoveryDecision{}, projectErr(ProjectErrorDiscoveryTimeout, errors.New("project discovery timed out"))
			}
			continue
		}
		if state == ProjectCheckoutReady || state == ProjectCheckoutDirty {
			candidates = append(candidates, projectDiscoveryCandidate{path: path, state: state})
		}
	}
	decision.CandidateCount = len(candidates)
	switch len(candidates) {
	case 0:
		decision.State = ProjectRecoveryCloneRequired
	case 1:
		if candidates[0].state == ProjectCheckoutDirty {
			decision.State = ProjectRecoveryBlocked
			decision.Reason = ProjectErrorCheckoutDirty
		} else {
			decision.State = ProjectRecoveryAssociateExisting
			decision.CandidatePath = candidates[0].path
		}
	default:
		decision.State = ProjectRecoveryBlocked
		decision.Reason = ProjectErrorAmbiguousCheckout
	}
	return decision, nil
}

func (decision ProjectRecoveryDecision) SafeStatus() ProjectRecoveryStatus {
	return ProjectRecoveryStatus{
		Alias:          decision.Alias,
		Repository:     decision.Owner + "/" + decision.Repository,
		State:          decision.State,
		CandidateCount: decision.CandidateCount,
		Reason:         decision.Reason,
	}
}

func projectDiscoveryPathState(path string) (exists, unsafe bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || strings.TrimSpace(filepath.Base(path)) == "", nil
}
