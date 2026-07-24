package edgeclient

import (
	"context"
	"errors"
	"strings"
)

func ProjectErrorCodeOf(err error) ProjectErrorCode {
	var projectFailure *ProjectError
	if errors.As(err, &projectFailure) {
		return projectFailure.Code
	}
	return ProjectErrorRegistryUnavailable
}

func (r *ProjectRegistry) Status(ctx context.Context, rawAlias, rawTarget string) (ProjectStatus, error) {
	if r == nil || r.db == nil || r.workspaces == nil || r.inspector == nil {
		return ProjectStatus{}, projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	alias, err := NormalizeProjectAlias(rawAlias)
	if err != nil {
		return ProjectStatus{}, err
	}
	project, found, err := loadProject(r.db, alias)
	if err != nil {
		return ProjectStatus{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	target := strings.TrimSpace(rawTarget)
	if found && target == "" {
		target = project.PreferredTarget
	} else if target != "" {
		target, err = normalizeProjectTarget(target)
		if err != nil {
			return ProjectStatus{}, err
		}
	}
	status := ProjectStatus{Alias: alias, Target: target, State: "blocked"}
	if !found {
		status.Reason = ProjectErrorProjectNotFound
		return status, nil
	}
	status.Repository = project.Owner + "/" + project.Repository
	resolution, err := r.Resolve(ctx, alias, target)
	if err == nil {
		return resolution.SafeStatus(), nil
	}
	status.Reason = ProjectErrorCodeOf(err)
	return status, nil
}
