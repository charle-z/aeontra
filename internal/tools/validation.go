package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const validationPlanTTL = 2 * time.Minute

// ValidationRunner is a small private service. It is deliberately separate from
// the public MCP daemon because only the runner may reach Docker. Its request
// model accepts a repo name and a fixed profile, never an argv or shell script.
type ValidationRunner interface {
	Available() bool
	Run(ctx context.Context, repo, profile string) (ValidationResult, error)
}

type ValidationResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

type validationHTTPRunner struct {
	baseURL, token string
	client         *http.Client
}

func NewValidationRunner(rawURL, token string) ValidationRunner {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "http" || u.Host == "" || len(strings.TrimSpace(token)) < 32 {
		return disabledValidationRunner{}
	}
	return validationHTTPRunner{baseURL: strings.TrimRight(u.String(), "/"), token: token, client: &http.Client{Timeout: 9 * time.Minute}}
}

func (r validationHTTPRunner) Available() bool { return true }
func (r validationHTTPRunner) Run(ctx context.Context, repo, profile string) (ValidationResult, error) {
	b, _ := json.Marshal(map[string]string{"repo": repo, "profile": profile})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/run", bytes.NewReader(b))
	if err != nil {
		return ValidationResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return ValidationResult{}, fmt.Errorf("calling private validation runner: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ValidationResult{}, fmt.Errorf("private validation runner returned %s", resp.Status)
	}
	var result ValidationResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return ValidationResult{}, fmt.Errorf("decoding private validation runner response: %w", err)
	}
	return result, nil
}

type disabledValidationRunner struct{}

func (disabledValidationRunner) Available() bool { return false }
func (disabledValidationRunner) Run(context.Context, string, string) (ValidationResult, error) {
	return ValidationResult{}, fmt.Errorf("private validation runner is not configured")
}

func (s *Service) ValidationPreview(repo, profile string) (string, error) {
	sp := s.log.Start("project_validation_preview")
	if !s.validation.Available() {
		err := fmt.Errorf("private validation runner is not configured; set MCP_DEVBOX_VALIDATION_RUNNER_URL and MCP_DEVBOX_VALIDATION_RUNNER_TOKEN in the MCP service after deploying the private runner")
		sp.Finish(audit.Deny, profile, nil, err)
		return "", err
	}
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, profile, nil, err)
		return "", err
	}
	if filepath.Dir(dir) != s.root {
		err := fmt.Errorf("validation accepts one direct repository under the workspace root")
		sp.Finish(audit.Deny, profile, []string{dir}, err)
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		err = fmt.Errorf("repository has no package.json")
		sp.Finish(audit.Deny, profile, []string{dir}, err)
		return "", err
	}
	profile = strings.TrimSpace(profile)
	var effect, network string
	switch profile {
	case "pnpm-lockfile":
		effect, network = "Generate pnpm-lock.yaml and prefetch the declared dependency graph without lifecycle scripts.", "enabled only for npm registry resolution/fetch; no project scripts run"
	case "pnpm-validate":
		effect, network = "Install from the existing frozen lockfile offline, then run fixed check, test and build scripts in an ephemeral container.", "disabled"
	default:
		err := fmt.Errorf("unsupported validation profile %q", profile)
		sp.Finish(audit.Deny, profile, []string{dir}, err)
		return "", err
	}
	plan, err := s.plans.CreateTTL("project-validation", map[string]string{"repo": filepath.Base(dir), "profile": profile}, validationPlanTTL)
	if err != nil {
		sp.Finish(audit.Error, profile, []string{dir}, err)
		return "", err
	}
	sp.Finish(audit.Allow, profile+" "+plan.ID, []string{dir}, nil)
	return fmt.Sprintf("Repository: %s\nProfile: %s\n\nEffect: %s\nNetwork: %s\nContainer posture: private runner only; no Docker socket or host terminal is exposed to the public MCP.\n\nPlan ID: %s\nExpiry: %s\n", filepath.Base(dir), profile, effect, network, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) ValidationExecute(planID string, approve bool) (string, error) {
	sp := s.log.Start("project_validation_execute")
	if !s.validation.Available() {
		err := fmt.Errorf("private validation runner is not configured")
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
		return "APPROVAL REQUIRED: project_validation_execute will run the reviewed fixed profile in the private validation runner. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "project-validation")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	result, err := s.validation.Run(ctx, plan.Args["repo"], plan.Args["profile"])
	out := s.redact(result.Output)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return out, err
	}
	if result.ExitCode != 0 {
		err := fmt.Errorf("validation profile %s failed with exit code %d", plan.Args["profile"], result.ExitCode)
		sp.Finish(audit.Allow, planID, []string{filepath.Join(s.root, plan.Args["repo"])}, err)
		return out, err
	}
	sp.Finish(audit.Allow, planID, []string{filepath.Join(s.root, plan.Args["repo"])}, nil)
	return out, nil
}
