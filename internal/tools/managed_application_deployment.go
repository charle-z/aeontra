package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	managedDeploymentListLimit   = 20
	managedDeploymentDecodeLimit = 100
)

type managedApplicationDeployment struct {
	DeploymentUUID string
	Status         string
	Commit         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *PlatformCapability) latestManagedApplicationDeployment(appID string) (managedApplicationDeployment, error) {
	appID = strings.TrimSpace(appID)
	if !coolifyUUIDRe.MatchString(appID) {
		return managedApplicationDeployment{}, errors.New("managed application id is invalid")
	}
	query := url.Values{
		"skip": {"0"},
		"take": {fmt.Sprint(managedDeploymentListLimit)},
	}
	basePath := "/api/v1/deployments/applications/" + url.PathEscape(appID)
	paths := []string{basePath + "?" + query.Encode(), basePath}
	body := ""
	for index, path := range paths {
		status, candidate, err := s.coolify.request(context.Background(), http.MethodGet, path, nil)
		if err != nil {
			return managedApplicationDeployment{}, fmt.Errorf("reading managed application deployments: %w", err)
		}
		if status < 200 || status >= 300 {
			return managedApplicationDeployment{}, fmt.Errorf("reading managed application deployments -> HTTP %d: %s", status, s.coolifySafe(candidate))
		}
		if strings.TrimSpace(candidate) != "" {
			body = candidate
			break
		}
		if index == len(paths)-1 {
			return managedApplicationDeployment{}, errors.New("managed application deployments response is empty")
		}
	}
	deployments, err := decodeManagedApplicationDeployments(body)
	if err != nil {
		return managedApplicationDeployment{}, fmt.Errorf("decoding managed application deployments: %w", err)
	}
	if len(deployments) == 0 {
		return managedApplicationDeployment{}, errors.New("managed application has no deployments")
	}
	sort.Slice(deployments, func(i, j int) bool {
		if deployments[i].CreatedAt.Equal(deployments[j].CreatedAt) {
			return deployments[i].UpdatedAt.After(deployments[j].UpdatedAt)
		}
		return deployments[i].CreatedAt.After(deployments[j].CreatedAt)
	})
	latest := deployments[0]
	for _, candidate := range deployments[1:] {
		if !candidate.CreatedAt.Equal(latest.CreatedAt) {
			break
		}
		if candidate.DeploymentUUID != latest.DeploymentUUID {
			return managedApplicationDeployment{}, errors.New("managed application latest deployment is ambiguous")
		}
	}
	return latest, nil
}

type managedApplicationDeploymentRecord struct {
	DeploymentUUID string `json:"deployment_uuid"`
	UUID           string `json:"uuid"`
	Status         string `json:"status"`
	Commit         string `json:"commit"`
	GitCommitSHA   string `json:"git_commit_sha"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func decodeManagedApplicationDeployments(body string) ([]managedApplicationDeployment, error) {
	var raw []managedApplicationDeploymentRecord
	trimmed := strings.TrimSpace(body)
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		var wrapped struct {
			Deployments []managedApplicationDeploymentRecord `json:"deployments"`
			Data        []managedApplicationDeploymentRecord `json:"data"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapped); err != nil {
			return nil, err
		}
		if wrapped.Deployments != nil {
			raw = wrapped.Deployments
		} else {
			raw = wrapped.Data
		}
	}
	if len(raw) > managedDeploymentDecodeLimit {
		return nil, fmt.Errorf("managed application deployment response has %d records, max %d", len(raw), managedDeploymentDecodeLimit)
	}
	seen := map[string]bool{}
	deployments := make([]managedApplicationDeployment, 0, len(raw))
	for _, item := range raw {
		id := strings.TrimSpace(item.DeploymentUUID)
		if id == "" {
			id = strings.TrimSpace(item.UUID)
		}
		if !coolifyUUIDRe.MatchString(id) || seen[id] {
			return nil, errors.New("managed application deployment identity is invalid or duplicated")
		}
		seen[id] = true
		created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.CreatedAt))
		if err != nil || created.IsZero() {
			return nil, errors.New("managed application deployment created_at is invalid")
		}
		updated := created
		if strings.TrimSpace(item.UpdatedAt) != "" {
			updated, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(item.UpdatedAt))
			if err != nil || updated.Before(created) {
				return nil, errors.New("managed application deployment updated_at is invalid")
			}
		}
		commit := strings.TrimSpace(item.Commit)
		if commit == "" {
			commit = strings.TrimSpace(item.GitCommitSHA)
		}
		deployments = append(deployments, managedApplicationDeployment{
			DeploymentUUID: id,
			Status:         strings.TrimSpace(item.Status),
			Commit:         commit,
			CreatedAt:      created.UTC(),
			UpdatedAt:      updated.UTC(),
		})
	}
	return deployments, nil
}

func requireManagedDeployment(name string, deployment managedApplicationDeployment, expectedCommit string) error {
	if deployment.Status != "finished" {
		return fmt.Errorf("managed %s latest deployment is not finished: %s", name, deployment.Status)
	}
	if !frontDoorCommitPattern.MatchString(deployment.Commit) {
		return fmt.Errorf("managed %s latest deployment commit is invalid", name)
	}
	if expectedCommit != "" && deployment.Commit != expectedCommit {
		return fmt.Errorf("managed %s latest deployment commit does not match the approved branch", name)
	}
	return nil
}
