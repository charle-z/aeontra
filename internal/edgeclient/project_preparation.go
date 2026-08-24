package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	projectPreparationPlanVersion = 1
	projectPreparationPlanTTL     = 5 * time.Minute
)

type ProjectPreparationAction string

const (
	ProjectPreparationReuseExisting     ProjectPreparationAction = "reuse_existing"
	ProjectPreparationAssociateExisting ProjectPreparationAction = "associate_existing"
	ProjectPreparationClone             ProjectPreparationAction = "clone"
)

type ProjectPreparationConfig struct {
	StateRoot  string
	Projects   *ProjectRegistry
	Workspaces *WorkspaceRegistry
	Roots      WorkspaceRoots
	Credential GitHubCredential
	Runner     DevGitCommandRunner
	ToolPath   string
	Now        func() time.Time
}

type ProjectPreparationRequest struct {
	Alias       string
	Repository  string
	TargetAlias string
	Profile     WorkspaceProfile
}

type ProjectPreparationPlan struct {
	Version       int
	Alias         string
	Owner         string
	Repository    string
	TargetAlias   string
	Profile       WorkspaceProfile
	Action        ProjectPreparationAction
	CandidatePath string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

type ProjectPreparationPreview struct {
	Alias      string                   `json:"alias"`
	Repository string                   `json:"repository"`
	Target     string                   `json:"target"`
	Profile    WorkspaceProfile         `json:"profile"`
	Action     ProjectPreparationAction `json:"action"`
	State      string                   `json:"state"`
}

func PlanProjectPreparation(ctx context.Context, config ProjectPreparationConfig, request ProjectPreparationRequest) (ProjectPreparationPlan, error) {
	config, err := normalizeProjectPreparationConfig(config)
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	alias, err := NormalizeProjectAlias(request.Alias)
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	owner, repository, err := NormalizeProjectRepository(config.Credential.Owner, request.Repository)
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	target, err := normalizeProjectTarget(request.TargetAlias)
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	profiles, err := normalizeProjectProfiles([]WorkspaceProfile{request.Profile})
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	decision, err := DiscoverProjectCheckout(ctx, ProjectDiscoveryConfig{Roots: config.Roots, Inspector: config.Projects.inspector}, ProjectDiscoveryRequest{
		Alias: alias, Owner: owner, Repository: repository,
	})
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	action, candidate, err := projectPreparationAction(decision)
	if err != nil {
		return ProjectPreparationPlan{}, err
	}
	now := config.Now().UTC()
	return ProjectPreparationPlan{
		Version: projectPreparationPlanVersion, Alias: alias, Owner: owner, Repository: repository,
		TargetAlias: target, Profile: profiles[0], Action: action, CandidatePath: candidate,
		CreatedAt: now, ExpiresAt: now.Add(projectPreparationPlanTTL),
	}, nil
}

func ApplyProjectPreparation(ctx context.Context, config ProjectPreparationConfig, plan ProjectPreparationPlan) (ProjectStatus, error) {
	config, err := normalizeProjectPreparationConfig(config)
	if err != nil {
		return ProjectStatus{}, err
	}
	if err := validateProjectPreparationPlan(config, plan); err != nil {
		return ProjectStatus{}, err
	}
	decision, err := DiscoverProjectCheckout(ctx, ProjectDiscoveryConfig{Roots: config.Roots, Inspector: config.Projects.inspector}, ProjectDiscoveryRequest{
		Alias: plan.Alias, Owner: plan.Owner, Repository: plan.Repository,
	})
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, err)
	}
	action, candidate, actionErr := projectPreparationAction(decision)
	if actionErr != nil || action != plan.Action || filepath.Clean(candidate) != filepath.Clean(plan.CandidatePath) {
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, errors.New("project preparation decision changed"))
	}
	switch plan.Action {
	case ProjectPreparationReuseExisting, ProjectPreparationAssociateExisting:
		association, err := PlanProjectAssociation(ctx, ProjectAssociationConfig{
			Projects: config.Projects, Workspaces: config.Workspaces, Roots: config.Roots,
			Now: config.Now,
		}, ProjectAssociationRequest{
			Alias: plan.Alias, Owner: plan.Owner, Repository: plan.Repository,
			TargetAlias: plan.TargetAlias, Profile: plan.Profile,
		})
		if err != nil {
			return ProjectStatus{}, err
		}
		return ApplyProjectAssociation(ctx, ProjectAssociationConfig{
			Projects: config.Projects, Workspaces: config.Workspaces, Roots: config.Roots,
			Now: config.Now,
		}, association)
	case ProjectPreparationClone:
		return applyProjectClone(ctx, config, plan)
	default:
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, errors.New("project preparation action is invalid"))
	}
}

func (plan ProjectPreparationPlan) SafePreview() ProjectPreparationPreview {
	return ProjectPreparationPreview{
		Alias: plan.Alias, Repository: plan.Owner + "/" + plan.Repository,
		Target: plan.TargetAlias, Profile: plan.Profile, Action: plan.Action, State: "approval_required",
	}
}

func normalizeProjectPreparationConfig(config ProjectPreparationConfig) (ProjectPreparationConfig, error) {
	if config.Projects == nil || config.Projects.db == nil || config.Workspaces == nil || config.Workspaces.db == nil ||
		config.Projects.workspaces != config.Workspaces || config.Projects.inspector == nil {
		return ProjectPreparationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project preparation configuration is invalid"))
	}
	roots, err := normalizeWorkspaceRoots(config.Roots)
	if err != nil || roots != config.Workspaces.roots {
		return ProjectPreparationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project preparation roots do not match the workspace registry"))
	}
	config.Roots = roots
	config.StateRoot = filepath.Clean(strings.TrimSpace(config.StateRoot))
	if !filepath.IsAbs(config.StateRoot) || config.StateRoot == string(os.PathSeparator) {
		return ProjectPreparationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project preparation state root is invalid"))
	}
	owner, _, ownerErr := NormalizeProjectRepository(config.Credential.Owner, "project")
	if ownerErr != nil || owner != config.Projects.allowedOwner || config.Credential.SchemaVersion != 1 || !validGitHubToken(config.Credential.Token) {
		return ProjectPreparationConfig{}, projectErr(ProjectErrorCredentialUnavailable, errors.New("local GitHub authority is unavailable"))
	}
	config.Credential.Owner = owner
	if config.Runner == nil {
		config.Runner = NewDevGitCommandRunner(config.StateRoot, config.ToolPath)
	}
	if config.Runner == nil {
		return ProjectPreparationConfig{}, projectErr(ProjectErrorCredentialUnavailable, errors.New("development Git runner is unavailable"))
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config, nil
}

func validateProjectPreparationPlan(config ProjectPreparationConfig, plan ProjectPreparationPlan) error {
	alias, aliasErr := NormalizeProjectAlias(plan.Alias)
	owner, repository, repositoryErr := NormalizeProjectRepository(plan.Owner, plan.Repository)
	target, targetErr := normalizeProjectTarget(plan.TargetAlias)
	profiles, profileErr := normalizeProjectProfiles([]WorkspaceProfile{plan.Profile})
	now := config.Now().UTC()
	if aliasErr != nil || alias != plan.Alias || repositoryErr != nil || owner != plan.Owner || repository != plan.Repository ||
		owner != config.Credential.Owner || targetErr != nil || target != plan.TargetAlias || profileErr != nil || profiles[0] != plan.Profile ||
		plan.Version != projectPreparationPlanVersion || plan.CreatedAt.IsZero() || plan.ExpiresAt.IsZero() || now.Before(plan.CreatedAt) ||
		!plan.ExpiresAt.After(plan.CreatedAt) || plan.ExpiresAt.Sub(plan.CreatedAt) != projectPreparationPlanTTL {
		return projectErr(ProjectErrorPlanChanged, errors.New("project preparation plan is invalid"))
	}
	if !now.Before(plan.ExpiresAt) {
		return projectErr(ProjectErrorPlanExpired, errors.New("project preparation plan expired"))
	}
	canonical, err := CanonicalProjectPath(config.Roots, plan.Repository)
	if err != nil {
		return projectErr(ProjectErrorPlanChanged, err)
	}
	switch plan.Action {
	case ProjectPreparationClone:
		if filepath.Clean(plan.CandidatePath) != canonical {
			return projectErr(ProjectErrorPlanChanged, errors.New("project clone target changed"))
		}
	case ProjectPreparationReuseExisting, ProjectPreparationAssociateExisting:
		root, rootErr := projectDevelopmentRoot(config.Roots)
		if rootErr != nil || !filepath.IsAbs(plan.CandidatePath) || filepath.Clean(plan.CandidatePath) == filepath.Clean(root) || !pathInside(root, filepath.Clean(plan.CandidatePath)) {
			return projectErr(ProjectErrorPlanChanged, errors.New("project association target changed"))
		}
	default:
		return projectErr(ProjectErrorPlanChanged, errors.New("project preparation action changed"))
	}
	return nil
}

func projectPreparationAction(decision ProjectRecoveryDecision) (ProjectPreparationAction, string, error) {
	switch decision.State {
	case ProjectRecoveryReuseExisting:
		return ProjectPreparationReuseExisting, decision.CandidatePath, nil
	case ProjectRecoveryAssociateExisting:
		return ProjectPreparationAssociateExisting, decision.CandidatePath, nil
	case ProjectRecoveryCloneRequired:
		return ProjectPreparationClone, decision.CanonicalPath, nil
	case ProjectRecoveryBlocked:
		return "", "", projectErr(decision.Reason, errors.New("project checkout cannot be prepared safely"))
	default:
		return "", "", projectErr(ProjectErrorCheckoutUnsafe, errors.New("project recovery decision is invalid"))
	}
}

func applyProjectClone(ctx context.Context, config ProjectPreparationConfig, plan ProjectPreparationPlan) (ProjectStatus, error) {
	root, err := projectDevelopmentRoot(config.Roots)
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorCheckoutUnsafe, err)
	}
	if err := os.Mkdir(plan.CandidatePath, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, errors.New("project clone target appeared"))
		}
		return ProjectStatus{}, projectErr(ProjectErrorCloneFailed, errors.New("project clone target could not be reserved"))
	}
	reservation, err := os.Open(plan.CandidatePath)
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorCleanupRequired, errors.New("project clone reservation is unavailable"))
	}
	defer reservation.Close()
	reserved, err := reservation.Stat()
	if err != nil || !reserved.IsDir() || reserved.Mode()&os.ModeSymlink != 0 {
		return ProjectStatus{}, projectErr(ProjectErrorCleanupRequired, errors.New("project clone reservation is unsafe"))
	}
	cleanup := func(code ProjectErrorCode, cause error) (ProjectStatus, error) {
		if cleanupErr := removeReservedProjectClone(root, plan.CandidatePath, reserved); cleanupErr != nil {
			return ProjectStatus{}, projectErr(ProjectErrorCleanupRequired, errors.Join(cause, cleanupErr))
		}
		return ProjectStatus{}, projectErr(code, cause)
	}
	remoteURL := "https://github.com/" + plan.Owner + "/" + plan.Repository + ".git"
	if _, err := config.Runner.Run(ctx, plan.CandidatePath, []string{"clone", "--single-branch", "--", remoteURL, "."}, config.Credential); err != nil {
		return cleanup(ProjectErrorCloneFailed, errors.New("project clone failed"))
	}
	state, inspectErr := config.Projects.inspector.Inspect(ctx, plan.CandidatePath, plan.Owner, plan.Repository)
	if inspectErr != nil || state != ProjectCheckoutReady {
		return cleanup(ProjectErrorCloneFailed, errors.New("cloned project verification failed"))
	}
	workspace, created, err := config.Workspaces.AddProfile(plan.CandidatePath, plan.Profile)
	if err != nil {
		return cleanup(ProjectErrorRegistryUnavailable, err)
	}
	project, _, err := config.Projects.Register(ProjectRegistration{
		Alias: plan.Alias, Owner: plan.Owner, Repository: plan.Repository,
		PreferredTarget: plan.TargetAlias, TargetAlias: plan.TargetAlias,
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{plan.Profile},
	})
	if err != nil {
		if created {
			if rollbackErr := config.Workspaces.Remove(workspace.ID); rollbackErr != nil {
				return ProjectStatus{}, projectErr(ProjectErrorCleanupRequired, errors.Join(err, rollbackErr))
			}
		}
		return cleanup(ProjectErrorRegistryUnavailable, err)
	}
	return (ProjectResolution{Project: project, TargetAlias: plan.TargetAlias, Workspace: workspace}).SafeStatus(), nil
}

func removeReservedProjectClone(root, path string, reserved os.FileInfo) error {
	if reserved == nil || filepath.Dir(filepath.Clean(path)) != filepath.Clean(root) || !pathInside(root, filepath.Clean(path)) {
		return errors.New("project clone cleanup target is invalid")
	}
	current, err := os.Lstat(path)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(reserved, current) {
		return errors.New("project clone cleanup requires review")
	}
	if err := os.RemoveAll(path); err != nil {
		return errors.New("project clone cleanup failed")
	}
	return nil
}
