package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

func (c *GitHubClient) configError() error {
	var missing []string
	if c == nil || strings.TrimSpace(c.token) == "" {
		missing = append(missing, "GITHUB_TOKEN")
	}
	if c == nil || strings.TrimSpace(c.owner) == "" {
		missing = append(missing, "GITHUB_OWNER")
	}
	if c == nil || (c.ownerType != "user" && c.ownerType != "org") {
		missing = append(missing, "GITHUB_OWNER_TYPE (user or org)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("GitHub configuration missing or invalid: %s", strings.Join(missing, ", "))
	}
	return nil
}

// SourceRepoInfo reads one repository under the fixed configured owner. A 404 is
// represented as exists:false rather than being confused with a transport failure.
func (s *SourceCapability) SourceRepoInfo(name string) (string, error) {
	sp := s.log.Start("source_repo_info")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "source_repo_info", nil, err)
		return "", err
	}
	name = strings.TrimSpace(name)
	if !safeCloneDir(name) {
		err := fmt.Errorf("invalid GitHub repository name %q", name)
		sp.Finish(audit.Deny, "source_repo_info", nil, err)
		return "", err
	}
	status, body, err := s.github.repoInfo(context.Background(), name)
	if err != nil {
		sp.Finish(audit.Error, "source_repo_info "+name, nil, err)
		return "", fmt.Errorf("GitHub repository info request failed: %w", err)
	}
	if status == http.StatusNotFound {
		sp.Finish(audit.Allow, "source_repo_info "+name, nil, nil)
		return fmt.Sprintf("owner: %s\nfull_name: %s/%s\nexists: false\n", s.github.owner, s.github.owner, name), nil
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("GitHub repository info -> HTTP %d: %s", status, s.redact(body))
		sp.Finish(audit.Error, "source_repo_info "+name, nil, err)
		return s.redact(body), err
	}
	var repo githubRepoResponse
	if err := json.Unmarshal([]byte(body), &repo); err != nil {
		sp.Finish(audit.Error, "source_repo_info "+name, nil, err)
		return "", fmt.Errorf("decoding GitHub repository info: %w", err)
	}
	sp.Finish(audit.Allow, "source_repo_info "+name, nil, nil)
	return formatSourceRepo(s.github.owner, repo, true), nil
}

func (s *SourceCapability) SourceRepoCreatePreview(name, visibility, description string) (string, error) {
	sp := s.log.Start("source_repo_create_preview")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	name = strings.TrimSpace(name)
	if !safeCloneDir(name) {
		err := fmt.Errorf("invalid GitHub repository name %q", name)
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	visibility = normalizeRepoVisibilityWithDefault(visibility, s.github.defaultVisibility)
	if visibility == "" {
		err := fmt.Errorf("invalid visibility (use private or public)")
		sp.Finish(audit.Deny, "preview "+name, nil, err)
		return "", err
	}
	status, body, err := s.github.repoInfo(context.Background(), name)
	if err != nil {
		sp.Finish(audit.Error, "preview "+name, nil, err)
		return "", fmt.Errorf("checking GitHub repository existence: %w", err)
	}
	if status >= 200 && status < 300 {
		err := fmt.Errorf("repository %s/%s already exists", s.github.owner, name)
		sp.Finish(audit.Deny, "preview "+name, nil, err)
		return s.redact(body), err
	}
	if status != http.StatusNotFound {
		err := fmt.Errorf("checking GitHub repository existence -> HTTP %d: %s", status, s.redact(body))
		sp.Finish(audit.Error, "preview "+name, nil, err)
		return s.redact(body), err
	}
	safeDescription := s.redact(strings.TrimSpace(description))
	plan, err := s.plans.Create("source-repo-create", map[string]string{
		"owner": s.github.owner, "owner_type": s.github.ownerType, "name": name,
		"visibility": visibility, "description": safeDescription, "exists": "false",
	})
	if err != nil {
		sp.Finish(audit.Error, "preview "+name, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+name+" "+plan.ID, nil, nil)
	return fmt.Sprintf("owner: %s\nfull_name: %s/%s\nvisibility: %s\ndescription: %s\nexists: false\neffect: create one GitHub repository under the configured owner\nplan_id: %s\nexpiry: %s\n",
		s.github.owner, s.github.owner, name, visibility, safeDescription, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourceRepoCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_repo_create")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_repo_create would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-repo-create")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["owner"] != s.github.owner || plan.Args["owner_type"] != s.github.ownerType {
		err := fmt.Errorf("configured GitHub owner changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	status, body, err := s.github.repoInfo(context.Background(), plan.Args["name"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if status != http.StatusNotFound {
		err := fmt.Errorf("repository state changed after preview: expected absent, got HTTP %d", status)
		sp.Finish(audit.Deny, planID, nil, err)
		return s.redact(body), err
	}
	status, body, err = s.github.createRepo(context.Background(), plan.Args["name"], plan.Args["description"], plan.Args["visibility"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("GitHub create repository request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("GitHub create repository -> HTTP %d: %s", status, s.redact(body))
		sp.Finish(audit.Error, planID, nil, err)
		return s.redact(body), err
	}
	var repo githubRepoResponse
	if err := json.Unmarshal([]byte(body), &repo); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("decoding created GitHub repository: %w", err)
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return formatSourceRepo(s.github.owner, repo, true), nil
}

func formatSourceRepo(owner string, repo githubRepoResponse, exists bool) string {
	visibility := strings.TrimSpace(repo.Visibility)
	if visibility == "" {
		if repo.Private {
			visibility = "private"
		} else {
			visibility = "public"
		}
	}
	permission := strings.TrimSpace(repo.RoleName)
	if permission == "" {
		switch {
		case repo.Permissions.Admin:
			permission = "admin"
		case repo.Permissions.Push:
			permission = "write"
		case repo.Permissions.Pull:
			permission = "read"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "owner: %s\nfull_name: %s\nvisibility: %s\ndefault_branch: %s\n", owner, repo.FullName, visibility, repo.DefaultBranch)
	if clone, err := sanitizeCredentialFreeURL(repo.CloneURL); err == nil && clone != "" {
		fmt.Fprintf(&b, "clone_url: %s\n", clone)
	}
	fmt.Fprintf(&b, "exists: %t\n", exists)
	if permission != "" {
		fmt.Fprintf(&b, "viewer_permission: %s\n", permission)
	}
	return b.String()
}
