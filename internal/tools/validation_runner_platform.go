package tools

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	managedValidationRunnerName       = "mcp-devbox-validation-runner-managed"
	managedValidationRunnerPort       = "8787"
	managedValidationRunnerDockerfile = "/Dockerfile.validation-runner"
)

// PlatformValidationRunnerCreatePreview plans one narrowly defined private Coolify
// application. The agent supplies only the source branch; destination and mount
// authority remain administrator-owned configuration.
func (s *PlatformCapability) PlatformValidationRunnerCreatePreview(branch string) (string, error) {
	sp := s.log.Start("platform_validation_runner_create_preview")
	if err := s.coolify.builderConfigError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	if s.coolify.destinationUUID == "" {
		err := fmt.Errorf("COOLIFY_DESTINATION_UUID is required")
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	mounts, repoHostRoot, store, err := s.validationRunnerRuntimeConfig()
	if err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	branch = defaultGitName(branch, "main")
	if !safeGitName(branch) {
		err := fmt.Errorf("invalid git branch %q", branch)
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	exists, err := s.platformAppNameExists(managedValidationRunnerName)
	if err != nil {
		sp.Finish(audit.Error, "preview", nil, err)
		return "", err
	}
	if exists {
		err := fmt.Errorf("coolify application %q already exists", managedValidationRunnerName)
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	repo := "https://github.com/" + s.github.owner + "/mcp-devbox.git"
	plan, err := s.plans.Create("platform-validation-runner-create", map[string]string{
		"name": managedValidationRunnerName, "repository": repo, "branch": branch,
		"server_uuid": s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"environment_uuid": s.coolify.environmentUUID, "environment_name": s.coolify.environmentName,
		"destination_uuid": s.coolify.destinationUUID, "mounts": strings.Join(mounts, "\n"),
		"repo_host_root": repoHostRoot, "store": store,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("name: %s\nrepository: %s\nbranch: %s\ndestination_uuid: %s\ndockerfile_location: %s\ninternal_port: %s\ndomain: none\nports_mappings: none\nauto_deploy: disabled\ninstant_deploy: disabled\nmounts: %s\nnon_secret_environment_variables: 7\nsecret_environment_variables_pending_manual_entry: MCP_DEVBOX_VALIDATION_RUNNER_TOKEN\neffect: create exactly one private validation runner application without deploying it\nplan_id: %s\nexpiry: %s\n",
		managedValidationRunnerName, repo, branch, s.coolify.destinationUUID,
		managedValidationRunnerDockerfile, managedValidationRunnerPort,
		strings.Join(mounts, " | "), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) PlatformValidationRunnerCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_validation_runner_create")
	if err := s.coolify.builderConfigError(); err != nil {
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
		return "APPROVAL REQUIRED: platform_validation_runner_create would create the reviewed private Coolify application. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-validation-runner-create")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	mounts, repoHostRoot, store, err := s.validationRunnerRuntimeConfig()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["server_uuid"] != s.coolify.serverUUID || plan.Args["project_uuid"] != s.coolify.projectUUID ||
		plan.Args["environment_uuid"] != s.coolify.environmentUUID || plan.Args["environment_name"] != s.coolify.environmentName ||
		plan.Args["destination_uuid"] != s.coolify.destinationUUID || plan.Args["mounts"] != strings.Join(mounts, "\n") ||
		plan.Args["repo_host_root"] != repoHostRoot || plan.Args["store"] != store {
		err := fmt.Errorf("validation runner builder configuration changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	exists, err := s.platformAppNameExists(managedValidationRunnerName)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if exists {
		err := fmt.Errorf("coolify application %q already exists", managedValidationRunnerName)
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	var options []string
	for _, mount := range mounts {
		options = append(options, "--mount "+mount)
	}
	payload := map[string]any{
		"name":        managedValidationRunnerName,
		"server_uuid": plan.Args["server_uuid"], "project_uuid": plan.Args["project_uuid"],
		"destination_uuid": plan.Args["destination_uuid"],
		"git_repository":   plan.Args["repository"], "git_branch": plan.Args["branch"],
		"build_pack": "dockerfile", "dockerfile_location": managedValidationRunnerDockerfile,
		"ports_exposes": managedValidationRunnerPort, "ports_mappings": "",
		"fqdn": "", "autogenerate_domain": false, "is_auto_deploy_enabled": false, "instant_deploy": false,
		"connect_to_docker_network": true,
		"custom_docker_run_options": strings.Join(options, " "),
		"health_check_enabled":      true, "health_check_type": "http", "health_check_scheme": "http",
		"health_check_method": "GET", "health_check_path": "/healthz", "health_check_port": 8787,
		"health_check_return_code": 200, "health_check_interval": 10, "health_check_timeout": 3,
		"health_check_retries": 12, "health_check_start_period": 20,
	}
	if plan.Args["environment_uuid"] != "" {
		payload["environment_uuid"] = plan.Args["environment_uuid"]
	} else {
		payload["environment_name"] = plan.Args["environment_name"]
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/public", payload)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("coolify create validation runner request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("coolify create validation runner -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return s.coolifySafe(body), err
	}
	apps, err := decodePlatformApplications("[" + body + "]")
	if err != nil || len(apps) != 1 || apps[0].UUID == "" {
		sp.Finish(audit.Error, planID, nil, err)
		return s.coolifySafe(body), fmt.Errorf("coolify created the application but its UUID could not be decoded")
	}
	app := apps[0]
	env := map[string]string{
		"MCP_DEVBOX_VALIDATION_RUNNER_ADDR":      ":8787",
		"MCP_DEVBOX_VALIDATION_RUNNER_ROOT":      "/repos",
		"MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT": repoHostRoot,
		"MCP_DEVBOX_VALIDATION_RUNNER_IMAGE":     "node:22-alpine",
		"MCP_DEVBOX_VALIDATION_RUNNER_STORE":     store,
		"MCP_DEVBOX_VALIDATION_RUNNER_USER":      "10001:10001",
		"MCP_DEVBOX_VALIDATION_RUNNER_TIMEOUT":   "8m",
	}
	if err := s.setPlatformEnvironment(app.UUID, env); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return fmt.Sprintf("application_uuid: %s\napplication_created: true\ndeployed: false\n", app.UUID), err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("application_uuid: %s\nname: %s\nbranch: %s\ndomain: none\nports_mappings: none\ndeployed: false\nnon_secret_environment_variables_configured: 7\nnext_manual_step: set MCP_DEVBOX_VALIDATION_RUNNER_TOKEN in Coolify, then request deployment\n",
		app.UUID, managedValidationRunnerName, plan.Args["branch"]), nil
}

func (s *PlatformCapability) validationRunnerRuntimeConfig() ([]string, string, string, error) {
	if s.coolify == nil || len(s.coolify.allowedMounts) != 3 {
		return nil, "", "", fmt.Errorf("COOLIFY_ALLOWED_MOUNTS must contain exactly the three validation-runner mounts")
	}
	mounts := make([]string, 0, len(s.coolify.allowedMounts))
	for mount := range s.coolify.allowedMounts {
		mounts = append(mounts, mount)
	}
	sort.Strings(mounts)
	var repoHostRoot, store string
	var socket bool
	for _, mount := range mounts {
		parts := map[string]string{}
		for _, item := range strings.Split(mount, ",") {
			kv := strings.SplitN(item, "=", 2)
			if len(kv) == 2 {
				parts[kv[0]] = kv[1]
			}
		}
		switch parts["target"] {
		case "/var/run/docker.sock":
			socket = parts["type"] == "bind" && parts["source"] == "/var/run/docker.sock"
		case "/repos":
			if parts["type"] == "bind" {
				repoHostRoot = parts["source"]
			}
		case "/pnpm-store":
			if parts["type"] == "volume" {
				store = parts["source"]
			}
		}
	}
	if !socket || repoHostRoot == "" || store == "" {
		return nil, "", "", fmt.Errorf("validation-runner mount allowlist is incomplete or malformed")
	}
	return mounts, repoHostRoot, store, nil
}

func (s *PlatformCapability) platformAppNameExists(name string) (bool, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		return false, fmt.Errorf("listing Coolify applications: %w", err)
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("listing Coolify applications -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	apps, err := decodePlatformApplications(body)
	if err != nil {
		return false, err
	}
	for _, app := range apps {
		if app.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (s *PlatformCapability) setPlatformEnvironment(app string, vars map[string]string) error {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		payload := map[string]any{
			"key": key, "value": vars[key], "is_preview": false, "is_literal": true,
			"is_runtime": true, "is_buildtime": false,
		}
		status, body, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/"+app+"/envs", payload)
		if err != nil {
			return fmt.Errorf("setting %s: %w", key, err)
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("setting %s -> HTTP %d: %s", key, status, s.coolifySafe(body))
		}
	}
	return nil
}
