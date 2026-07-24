package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

const (
	projectAssociationPlanVersion = 1
	projectAssociationPlanTTL     = 5 * time.Minute
)

type ProjectAssociationAction string

const (
	ProjectAssociationReuseExisting     ProjectAssociationAction = "reuse_existing"
	ProjectAssociationAssociateExisting ProjectAssociationAction = "associate_existing"
)

type ProjectAssociationConfig struct {
	Projects   *ProjectRegistry
	Workspaces *WorkspaceRegistry
	Roots      WorkspaceRoots
	Inspector  ProjectCheckoutInspector
	Now        func() time.Time
}

type ProjectAssociationRequest struct {
	Alias       string
	Owner       string
	Repository  string
	TargetAlias string
	Profile     WorkspaceProfile
}

type ProjectAssociationPlan struct {
	Version       int
	Alias         string
	Owner         string
	Repository    string
	TargetAlias   string
	Profile       WorkspaceProfile
	Action        ProjectAssociationAction
	CandidatePath string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

type ProjectAssociationPreview struct {
	Alias      string                   `json:"alias"`
	Repository string                   `json:"repository"`
	Target     string                   `json:"target"`
	Profile    WorkspaceProfile         `json:"profile"`
	Action     ProjectAssociationAction `json:"action"`
	State      string                   `json:"state"`
}

func PlanProjectAssociation(ctx context.Context, config ProjectAssociationConfig, request ProjectAssociationRequest) (ProjectAssociationPlan, error) {
	config, err := normalizeProjectAssociationConfig(config)
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	alias, err := NormalizeProjectAlias(request.Alias)
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	owner, repository, err := NormalizeProjectRepository(request.Owner, request.Repository)
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	if owner != config.Projects.allowedOwner {
		return ProjectAssociationPlan{}, projectErr(ProjectErrorOwnerDenied, errors.New("project owner is not allowed"))
	}
	target, err := normalizeProjectTarget(request.TargetAlias)
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	profiles, err := normalizeProjectProfiles([]WorkspaceProfile{request.Profile})
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	decision, err := DiscoverProjectCheckout(ctx, ProjectDiscoveryConfig{
		Roots: config.Roots, Inspector: config.Inspector,
	}, ProjectDiscoveryRequest{Alias: alias, Owner: owner, Repository: repository})
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	action, err := associationActionForDecision(decision)
	if err != nil {
		return ProjectAssociationPlan{}, err
	}
	now := config.Now().UTC()
	return ProjectAssociationPlan{
		Version: projectAssociationPlanVersion,
		Alias:   alias, Owner: owner, Repository: repository, TargetAlias: target,
		Profile: profiles[0], Action: action, CandidatePath: decision.CandidatePath,
		CreatedAt: now, ExpiresAt: now.Add(projectAssociationPlanTTL),
	}, nil
}

func ApplyProjectAssociation(ctx context.Context, config ProjectAssociationConfig, plan ProjectAssociationPlan) (ProjectStatus, error) {
	config, err := normalizeProjectAssociationConfig(config)
	if err != nil {
		return ProjectStatus{}, err
	}
	if err := validateProjectAssociationPlan(config, plan); err != nil {
		return ProjectStatus{}, err
	}
	decision, err := DiscoverProjectCheckout(ctx, ProjectDiscoveryConfig{
		Roots: config.Roots, Inspector: config.Inspector,
	}, ProjectDiscoveryRequest{Alias: plan.Alias, Owner: plan.Owner, Repository: plan.Repository})
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, err)
	}
	action, actionErr := associationActionForDecision(decision)
	if actionErr != nil || action != plan.Action || filepath.Clean(decision.CandidatePath) != filepath.Clean(plan.CandidatePath) {
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, errors.New("project association decision changed"))
	}
	workspace, created, err := config.Workspaces.AddProfile(decision.CandidatePath, plan.Profile)
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorPlanChanged, err)
	}
	project, _, err := config.Projects.Register(ProjectRegistration{
		Alias: plan.Alias, Owner: plan.Owner, Repository: plan.Repository,
		PreferredTarget: plan.TargetAlias, TargetAlias: plan.TargetAlias,
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{plan.Profile},
	})
	if err != nil {
		if created {
			if rollbackErr := config.Workspaces.Remove(workspace.ID); rollbackErr != nil {
				return ProjectStatus{}, projectErr(ProjectErrorRegistryUnavailable, errors.Join(err, rollbackErr))
			}
		}
		return ProjectStatus{}, err
	}
	return (ProjectResolution{
		Project: project, TargetAlias: plan.TargetAlias, Workspace: workspace,
	}).SafeStatus(), nil
}

func (plan ProjectAssociationPlan) SafePreview() ProjectAssociationPreview {
	return ProjectAssociationPreview{
		Alias:      plan.Alias,
		Repository: plan.Owner + "/" + plan.Repository,
		Target:     plan.TargetAlias,
		Profile:    plan.Profile,
		Action:     plan.Action,
		State:      "approval_required",
	}
}

func normalizeProjectAssociationConfig(config ProjectAssociationConfig) (ProjectAssociationConfig, error) {
	if config.Projects == nil || config.Projects.db == nil || config.Workspaces == nil || config.Workspaces.db == nil ||
		config.Projects.workspaces != config.Workspaces {
		return ProjectAssociationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project association configuration is invalid"))
	}
	roots, err := normalizeWorkspaceRoots(config.Roots)
	if err != nil || roots != config.Workspaces.roots {
		return ProjectAssociationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project association roots do not match the workspace registry"))
	}
	config.Roots = roots
	config.Inspector = config.Projects.inspector
	if config.Inspector == nil {
		return ProjectAssociationConfig{}, projectErr(ProjectErrorInvalidInput, errors.New("project checkout inspector is unavailable"))
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config, nil
}

func validateProjectAssociationPlan(config ProjectAssociationConfig, plan ProjectAssociationPlan) error {
	alias, err := NormalizeProjectAlias(plan.Alias)
	if err != nil || alias != plan.Alias {
		return projectErr(ProjectErrorPlanChanged, errors.New("project association alias changed"))
	}
	owner, repository, err := NormalizeProjectRepository(plan.Owner, plan.Repository)
	if err != nil || owner != plan.Owner || repository != plan.Repository || owner != config.Projects.allowedOwner {
		return projectErr(ProjectErrorPlanChanged, errors.New("project association repository changed"))
	}
	target, err := normalizeProjectTarget(plan.TargetAlias)
	if err != nil || target != plan.TargetAlias {
		return projectErr(ProjectErrorPlanChanged, errors.New("project association target changed"))
	}
	profiles, err := normalizeProjectProfiles([]WorkspaceProfile{plan.Profile})
	now := config.Now().UTC()
	if !now.Before(plan.ExpiresAt) {
		return projectErr(ProjectErrorPlanExpired, errors.New("project association plan expired"))
	}
	if err != nil || profiles[0] != plan.Profile || plan.Version != projectAssociationPlanVersion ||
		(plan.Action != ProjectAssociationReuseExisting && plan.Action != ProjectAssociationAssociateExisting) ||
		plan.CreatedAt.IsZero() || plan.ExpiresAt.IsZero() || now.Before(plan.CreatedAt) ||
		!plan.ExpiresAt.After(plan.CreatedAt) || plan.ExpiresAt.Sub(plan.CreatedAt) != projectAssociationPlanTTL {
		return projectErr(ProjectErrorPlanChanged, errors.New("project association plan is invalid"))
	}
	if !filepath.IsAbs(plan.CandidatePath) || filepath.Clean(plan.CandidatePath) == filepath.Clean(config.Roots.Dev) ||
		!pathInside(config.Roots.Dev, filepath.Clean(plan.CandidatePath)) {
		return projectErr(ProjectErrorPlanChanged, errors.New("project association candidate escaped the development root"))
	}
	return nil
}

func associationActionForDecision(decision ProjectRecoveryDecision) (ProjectAssociationAction, error) {
	switch decision.State {
	case ProjectRecoveryReuseExisting:
		return ProjectAssociationReuseExisting, nil
	case ProjectRecoveryAssociateExisting:
		return ProjectAssociationAssociateExisting, nil
	case ProjectRecoveryCloneRequired:
		return "", projectErr(ProjectErrorCheckoutMissing, errors.New("project checkout is missing"))
	case ProjectRecoveryBlocked:
		return "", projectErr(decision.Reason, errors.New("project checkout cannot be associated safely"))
	default:
		return "", projectErr(ProjectErrorCheckoutUnsafe, errors.New("project recovery decision is invalid"))
	}
}
